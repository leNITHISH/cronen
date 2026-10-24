# cronen

A Bubble Tea TUI that tells you what you're about to get into **before** you run `pacman -Syu`.

It shows pending repo and AUR updates, pulls the Arch news feed, flags anything that needs manual intervention, highlights risky packages (kernel, GPU drivers, glibc, systemd…), and links to changelogs. When you're happy, it hands off to `sudo pacman -Syu` (or your AUR helper).

---

## Goals

- One screen that answers: *Is it safe to update right now, and what do I need to read first?*
- Never cause a partial upgrade. cronen never runs `pacman -Sy`; it only reads.
- Fast. Startup to first render should be under 1s, and network data loads in the background.
- Vim-style keys by default.
- Also usable non-interactively (`--check`, `--json`) for status bars like waybar.

## Non-goals (for v1)

- Being an AUR helper. cronen reads AUR data but delegates installs to `paru`/`yay`.
- Rolling back packages, managing mirrors, or editing pacman.conf.
- Supporting non-Arch distros. Arch derivatives (EndeavourOS, CachyOS) should work but aren't a target.

---

## Tech stack

| Purpose | Library |
|---|---|
| TUI framework | Bubble Tea **v2**, `charm.land/bubbletea/v2` |
| Components (list, viewport, spinner, help, textinput) | `charm.land/bubbles/v2/...` |
| Styling and layout | `charm.land/lipgloss/v2` |
| Rendering news posts | `charm.land/glamour/v2` plus an HTML→Markdown converter (for example `github.com/JohannesKaufmann/html-to-markdown/v2`) |
| RSS parsing | `github.com/mmcdole/gofeed` |
| Config | TOML (`github.com/BurntSushi/toml`) |
| CLI flags | stdlib `flag` (keep it small) |

Use the latest v2 releases via `go get charm.land/bubbletea/v2@latest` (and the same for the others). **Do not mix v1 `github.com/charmbracelet/*` imports with v2 `charm.land/*` imports.**

Bubble Tea v2 API reminders (it differs from v1, so don't write v1 code from memory):
- `View()` returns `tea.View` (built with `tea.NewView(...)`), not `string`.
- Key handling uses `tea.KeyPressMsg`, not `tea.KeyMsg` with `msg.Type`.
- Check `UPGRADE_GUIDE_V2.md` in the bubbletea repo when in doubt.

Go version: whatever current Arch ships (`go` package). Set it in `go.mod`.

---

## Data sources

All system access goes through interfaces (see Architecture) so it can be faked in tests.

### 1. Pending repo updates
- Command: `checkupdates` (from `pacman-contrib`). It syncs a **temporary** copy of the database, so it's safe and won't cause partial upgrades.
- Output lines: `name oldver -> newver`
- Exit codes: `0` means updates are available, `2` means no updates, `1` means an error. Treat 2 as "empty list", not an error.
- If `checkupdates` isn't installed, the error is the single line `checkupdates not found — install pacman-contrib`. The list pane widens to fit a section error on one line as long as the detail pane keeps 40 columns (from 91 columns for this one); narrower terminals wrap it rather than cut it.

### 2. Pending AUR updates
- Installed foreign packages: `pacman -Qm` (prints `name version`).
- Latest versions: AUR RPC v5, batched:
  `https://aur.archlinux.org/rpc/v5/info?arg[]=pkg1&arg[]=pkg2...`
  Batch at most ~150 names per request to keep URLs sane.
- Compare versions with `vercmp old new` (ships with pacman). Do **not** hand-roll Arch version comparison, because epochs and pkgrel make it hard to get right.
- Packages that are installed locally but missing from the AUR get flagged as "not in AUR (orphaned/removed?)".
- From the RPC response, also surface `OutOfDate` (flagged out of date) and `Maintainer == null` (orphaned).
- **Debug splits.** `<name>-debug` is a makepkg debug split when `<name>` is installed (check against `pacman -Qq`, so splits of locally rebuilt repo packages count too). Debug splits are never on the AUR: don't look them up, never flag them "not in AUR", and hide them from the list unless `show_debug = true`. A `-debug` package whose base isn't installed is treated like any other package.
- **VCS packages.** Names ending in `-git`, `-svn`, `-hg`, `-bzr`, `-fossil`, `-darcs` or `-cvs` are built from upstream's latest source, so the RPC version is only a stale snapshot. Look them up (for links, orphaned and out-of-date status) but never compare versions, so they are never pending. List them with a `VCS` tag and leave them out of every update count.

### 3. Arch news
- RSS: `https://archlinux.org/feeds/news/`
- Each item has a title, link, pubDate, and HTML description. Convert the HTML to Markdown, then render it with Glamour in the detail pane.

### 4. Last full upgrade time
- Parse `/var/log/pacman.log` for the **last** line containing `[PACMAN] starting full system upgrade`.
- Timestamps look like `[2026-10-20T19:02:11+0530]`. Older logs used `[2019-01-01 12:00]`, so handle both.
- If the log is unreadable, fall back to "unknown" and treat all news from the last 30 days as unread.

### 5. Package metadata and changelogs
- Repo packages: `https://archlinux.org/packages/search/json/?name=<pkg>` gives `pkgbase`, `repo`, and `url` (upstream).
  - Packaging changelog link: `https://gitlab.archlinux.org/archlinux/packaging/packages/<pkgbase>/-/commits/main`
- AUR packages: changelog link is `https://aur.archlinux.org/cgit/aur.git/log/?h=<pkgbase>`, and `URL` from the RPC is the upstream link.
- v1 only shows and opens these links (`xdg-open`). It doesn't scrape changelog contents.

### 6. .pacnew files (post-upgrade hint)
- `pacdiff -o` (pacman-contrib) lists `.pacnew`/`.pacsave` files. Show the count in the status bar so it isn't forgotten.

---

## "Needs attention" logic

This is the core value of the tool, so it gets its own package (`internal/risk`) with thorough table-driven tests.

A pending update is **flagged** if any of these hold:

1. **News match.** An unread news item (published after the last full upgrade, and not marked read in state) mentions the package name as a whole word in its title or body. Match on `pkgbase` too (for example, news about `nvidia` should match `nvidia-utils`).
2. **Manual intervention.** An unread news item whose title or body contains "manual intervention", "intervention required", or "requires manual". These raise a **global banner** even if no pending package matches.
3. **Watchlist.** The package matches the watchlist (configurable, with glob support). Default:
   `linux*`, `nvidia*`, `mesa`, `vulkan-*`, `glibc`, `systemd`, `grub`, `pacman`, `openssl`, `python`, `gcc*`
4. **Major version bump.** The leading version component changes (`1.x → 2.x`), ignoring epoch-only changes. Skip it when the installed version's leading component is ≥ 50 (chromium 141, polkit 126, NVIDIA 580 and date versions bump it every release), and for packages matching `ignore_major` (globs, matched against name and pkgbase).
5. **Python minor bump.** `python 3.X → 3.Y`. Warn that AUR packages depending on Python likely need rebuilding, and list installed foreign packages that depend on `python` (`pacman -Qi` on each `pacman -Qm` package, then read "Depends On"). Mark this part as a stretch goal if it gets slow.
6. **Kernel updated.** If `linux*` is in the update list, show a "reboot after upgrade" note.

Each flag carries a short human-readable reason shown in the detail pane, for example: "Mentioned in news: 'NVIDIA 590 driver drops Pascal support' (2 days ago)".

Severity levels: `info` (watchlist, kernel), `warn` (major bump, news mention, Python bump), and `critical` (manual intervention).

---

## UI

```
┌ cronen ─────────────────────────────── last upgrade: 3 days ago ┐
│ ⚠ 1 news item requires manual intervention — press n to read      │
├──────────────────────────────┬────────────────────────────────────┤
│ NEEDS ATTENTION (3)          │ nvidia-utils                       │
│ ▸ nvidia-utils  580→590  ⚠  │ 580.95.05-1 → 590.44.01-1   extra  │
│   linux       6.17→6.18  ℹ  │                                    │
│   python      3.13→3.14  ⚠  │ Why flagged:                       │
│ REPO (41)                    │  • Mentioned in news (2 days ago)  │
│   ...                        │  • Major version bump              │
│ AUR (5)                      │                                    │
│   ...                        │ [o] upstream  [c] changelog        │
├──────────────────────────────┴────────────────────────────────────┤
│ 49 updates · 3 flagged · 2 unread news · 1 .pacnew   ? help  q quit│
└────────────────────────────────────────────────────────────────────┘
```

Screens:
- **Updates** (default): grouped list on the left, details on the right.
- **News** (`n`): list of recent news, with a Glamour-rendered post in the viewport. Unread items are bold.
- **Help** (`?`): from `bubbles/help`, generated from the keymap.
- **Confirm upgrade** (`U`): a modal that lists any unread critical news first and requires typing `y` to proceed.

Keybindings:

| Key | Action |
|---|---|
| `j`/`k`, `↓`/`↑` | move |
| `g`/`G` | top / bottom |
| `ctrl+d`/`ctrl+u` | half-page in the detail or news viewport |
| `tab` | switch focus between panes |
| `/` | filter package list |
| `n` | news screen |
| `m` | mark news item read |
| `o` | open upstream URL |
| `c` | open packaging changelog |
| `r` | refresh everything |
| `U` | upgrade (confirm modal, then hand off) |
| `?` | help |
| `q`, `ctrl+c` | quit |

Loading behaviour: render immediately with spinners per section. Repo updates, AUR, news, and the log run as separate concurrent `tea.Cmd`s, and each section fills in when its message arrives. A failed source shows an inline error in its section; it never crashes the app.

Theme: respect terminal background (light/dark). Keep colors subtle; use them for severity only.

Layout rules:
- **Version column.** Versions are right-aligned against the severity icon. When a row is too narrow, truncate the old version first, then the package name; never truncate the new version. If the new version can't fit at all, show the name alone. To keep the column compact, the list shows versions without epoch, pkgrel or git commit hash (`+g6f0cf3d`), and only their first two components (`590.44.01-1` → `590.44`) unless those are equal on both sides; the detail pane shows full versions.
- **URLs.** The upstream and changelog links in the detail pane, and the link under a news post's title, are never wrapped. A URL that doesn't fit is truncated with `…`, and it is an OSC 8 hyperlink to the full address, so it stays clickable. Strip control characters from URLs first, because they come from the network. (Links inside a news post's body are laid out by Glamour, which may still break a long URL.)

---

## Upgrade hand-off

- Command is configurable. Default: `sudo pacman -Syu`. If `aur_helper = "paru"` or `"yay"` is set, use `paru -Syu` / `yay -Syu`.
- Run via `tea.ExecProcess` so the TUI suspends and the real command gets the terminal. Don't capture its output.
- After it exits, record the time and refresh. Then show the `.pacnew` count and, if the kernel was updated, the reboot note.
- Mark all news up to now as read only after a **successful** upgrade (exit code 0).

---

## Non-interactive modes

- `cronen --check` prints a short plain-text summary and exits.
  Exit codes: `0` nothing to do, `10` updates available, `20` attention needed, `30` manual intervention, `1` error.
- `cronen --json` prints a machine-readable summary, for example `{"updates":49,"aur":5,"flagged":3,"critical":1,"unread_news":2}`. Make sure it works as a waybar `custom` module (`text`, `tooltip`, `class` fields behind a `--waybar` flag).
- `cronen --demo` launches the TUI using bundled fixture data and touches nothing on the system. **This is required**, both for screenshots and because development may happen on a non-Arch machine.

---

## Config and state

- Config: `$XDG_CONFIG_HOME/cronen/config.toml` (default `~/.config/cronen/config.toml`)
  ```toml
  aur = true
  aur_helper = ""            # "", "paru", "yay"
  upgrade_command = ""       # overrides everything if set
  watchlist = ["linux*", "nvidia*", "mesa", "glibc", "systemd"]
  watchlist_mode = "extend"  # "extend" defaults or "replace"
  news_days = 30
  ignore_major = []          # globs whose major version bumps aren't flagged
  show_debug = false         # list makepkg <name>-debug splits
  ```
- State: `$XDG_STATE_HOME/cronen/state.json`, which holds read news GUIDs and the last time cronen ran an upgrade.
- Cache: `$XDG_CACHE_HOME/cronen/`, which holds the news feed and AUR responses with a 15 minute TTL. `r` bypasses the cache.
- A missing config file is fine; use defaults. Write nothing to config automatically.

---

## Architecture

```
cmd/cronen/main.go          flags, wiring, choose TUI vs --check/--json/--demo
internal/system/              interfaces + real implementations
    runner.go                 type Runner interface { Run(ctx, name, args...) ([]byte, int, error) }
    fs.go                     file reads (pacman.log) behind an interface
    http.go                   HTTP client with timeout + cache
internal/pacman/              checkupdates, -Qm, -Qi parsing, vercmp wrapper, pacman.log parser
internal/aur/                 RPC v5 client, batching, version comparison via vercmp
internal/news/                RSS fetch, HTML→Markdown, unread logic
internal/archweb/             archlinux.org package JSON lookups (pkgbase, upstream URL)
internal/risk/                flagging rules, severity, reasons (pure functions, no I/O)
internal/config/              load config, XDG paths, state file
internal/ui/                  Bubble Tea models
    app.go                    root model, screen routing, messages
    updates.go                updates list + detail pane
    news.go                   news list + glamour viewport
    confirm.go                upgrade confirm modal
    keys.go                   keymap (bubbles/key)
    styles.go                 lipgloss styles
internal/fixtures/            embedded sample data for tests and --demo (go:embed)
```

Rules:
- Only `internal/system` touches `os/exec`, the filesystem, or the network. Everything else takes interfaces.
- `internal/risk` is pure: given updates, news, state, and config, it returns flags. This is where most tests live.
- UI models never block. All I/O happens in `tea.Cmd`s that return messages.

---

## Testing

- **The dev environment may not be Arch.** Nothing in the test suite may require `pacman`, `checkupdates`, `vercmp`, or network access.
- Fake `Runner` returns canned output from `internal/fixtures/`. Include fixtures for:
  - `checkupdates` with updates, with none (exit 2), and with an error
  - `pacman -Qm` output, including debug splits (of a foreign and of a repo package) and a VCS package, plus `pacman -Qq` listing every installed package
  - AUR RPC responses, including an orphaned package, an out-of-date package, and a package missing from the AUR
  - news RSS containing a manual-intervention post and a post that mentions a package
  - `pacman.log` with both timestamp formats
- `vercmp` fake: use a small table of known comparisons in tests. In the real implementation, always shell out.
- Table-driven tests for every rule in `internal/risk`.
- UI: snapshot the output of `View()` for key states (loading, loaded, error, news open, confirm modal) using fixture data at a fixed terminal size. Use Charm's `teatest` if a v2-compatible version is available; otherwise drive the model by calling `Update` with messages directly.
- CI: GitHub Actions on `ubuntu-latest` running `go vet`, `go test ./...`, and `golangci-lint`.

---

## Milestones

Each milestone is sized to be **one cloud session**. M1, M2, and M3 can run **in parallel** after M0 is merged, because they touch separate packages.

### M0: Skeleton (run alone, first)
- `go mod init`, add Charm v2 deps, directory layout as above.
- `internal/system` interfaces plus real and fake implementations.
- `internal/fixtures` with all fixture files listed under Testing.
- `cmd/cronen` with flags parsed and a placeholder Bubble Tea v2 program that opens and quits on `q`.
- GitHub Actions CI, `.golangci.yml`, `Makefile` (`build`, `test`, `lint`, `demo`).
- **Done when:** `make test` and `make lint` pass, and `go run ./cmd/cronen --demo` opens and quits cleanly.

### M1: Repo and AUR updates (parallel)
- `internal/pacman`: parse `checkupdates`, `pacman -Qm`; `vercmp` wrapper; handle exit codes.
- `internal/aur`: batched RPC v5 client, outdated/orphaned/missing detection.
- **Done when:** both packages are tested against fixtures, covering every edge case above.

### M2: News and attention rules (parallel)
- `internal/news`: fetch, parse, HTML→Markdown, cache.
- `internal/risk`: every rule in "Needs attention", with severity and reasons.
- **Done when:** table-driven tests cover each rule, including false positives (for example `linux-firmware` news shouldn't flag `linux-api-headers` unless the watchlist matches).

### M3: Log, config and state (parallel)
- `pacman.log` parser (both timestamp formats, last full upgrade).
- `internal/config`: XDG paths, TOML config with defaults, state file read/write.
- `internal/archweb`: package JSON lookup for pkgbase and upstream URL.
- **Done when:** everything is tested, and a missing or corrupt config/state never panics.

### M4: The TUI
- Root model, Updates screen, News screen, help, concurrent loading with per-section spinners and errors.
- Wire `--demo` to fixtures.
- **Done when:** `--demo` shows the full UI with flagged items, a critical banner, and readable news, with snapshot tests for main states, and it resizes correctly.

### M5: Actions and non-interactive modes
- `o`/`c` open links via `xdg-open`; `m` marks read; `/` filter.
- Upgrade confirm modal and `tea.ExecProcess` hand-off; post-upgrade refresh, reboot note, `.pacnew` count.
- `--check`, `--json`, `--waybar` with the exit codes above.
- **Done when:** all are tested with fakes, and the README documents the waybar snippet.

### M6: Polish and release
- README with a demo GIF (recorded with `vhs` from a `.tape` file using `--demo`).
- `PKGBUILD` (for an eventual `cronen` / `cronen-git` AUR package) and a `goreleaser` config.
  - `depends=(pacman-contrib)`, which provides `checkupdates` and `pacdiff`.
  - `optdepends=('xdg-utils: open upstream and changelog links' 'paru: AUR upgrades' 'yay: AUR upgrades')`
  - `makedepends=(go)`
  - The README must list these dependencies.
- Man page or `--help` that's actually helpful.
- **Done when:** a fresh clone builds with `makepkg -si` on Arch (verified manually by the maintainer).

---

## Later (not v1)

- Show actual upstream release notes (GitHub Releases API) in the detail pane.
- Download size per package and total.
- Desktop notification via a systemd user timer running `cronen --check`.
- Flag packages whose dependencies changed in a way that pulls in big new stuff.
- Archive lookup: "this version was released N days ago".
