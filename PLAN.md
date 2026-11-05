# cronen

A Bubble Tea TUI that tells you what you're about to get into **before** you run `pacman -Syu`.

> v1 (M0–M6) is shipped. **v2** (M7–M19, at the end of this file) turns cronen into a maintenance console and amends the Goals, Non-goals, config and exit codes below.

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

# v2: from pre-upgrade check to maintenance console

v1 (M0–M6) is shipped. v2 keeps everything above and adds two things. First, it tells you much more about the upgrade before you run it: exactly what the transaction will do, what it will break, and whether your system is ready for it. Second, it adds a few **previewed, confirmed** write actions (removing packages, cleaning orphans, downgrading from the cache), handed off to pacman the same way the upgrade already is. Then the helpers: bandwidth, urgent installs, partial-upgrade detection and snapshots.

Where this section contradicts v1 (Goals, Non-goals, config, exit codes), this section wins.

## v2 goals

- Answer *what exactly will `-Syu` do?*: new packages pulled in, packages replaced, download size, disk usage change, AUR packages that will need a rebuild.
- Catch the upgrades that fail halfway: full `/boot`, unmounted `/boot`, stale mirror, stale lock, old keyring.
- Point out reasons **to** update as well as reasons to wait: security fixes.
- Make reviewing AUR updates realistic: maintainer changes, the commit log, and the PKGBUILD diff with suspicious changes highlighted.
- Let the user understand and clean up what's installed: browse packages, see why each is installed, remove what they pick, clear out orphans, and go back to a cached version when an upgrade breaks something.
- Keep the flagged list meaningful: flags can be acknowledged, so the list doesn't turn into noise.
- Respect a limited connection. Say what an upgrade will cost in bytes and time before it starts, let the download happen early, and point out the big downloads worth dropping.
- Make the urgent case safe. "I need X now, and I haven't upgraded in weeks" gets real options with real costs, never `-Sy X`.
- Detect partial upgrades that already happened and the damage they left.
- Take a snapshot before upgrading: a Timeshift (or snapper) helper built into the upgrade flow.

## v2 non-goals

- Searching for packages, or installing them in any way other than the two the install helper offers (`-Syu X`, or a date-matched archive install). cronen is still not an AUR helper or a pacman frontend.
- Upgrading only some packages. Partial upgrades are unsupported on Arch, so the only upgrade is the full one from v1.
- Downgrading to versions that aren't already on disk. The Arch Linux Archive is used only for the install helper's date-matched installs.
- Managing mirrors or editing `pacman.conf`. cronen *reads* both and only *reports* problems with them.

## Safety invariants

These hold for every v2 feature. Reviews should check them explicitly.

1. **cronen never changes the system itself.** Every action that changes something goes through `tea.ExecProcess` with the terminal handed over, after a confirm modal that shows the **exact command line**. Nothing runs as root without a hand-off. The one exception is opt-in: with `snapshot_list_sudo = true`, cronen runs `sudo -n timeshift --list`, which only reads.
2. **The preview comes before the hand-off.** Each write action first runs pacman's own `--print` dry run as the user. If that fails (unsatisfiable dependencies, breaking a dependency), the modal shows pacman's error and offers no way to proceed.
3. **No shell, ever.** Commands are argv slices, with `--` before package names. Package names are validated against `^[a-z0-9@._+][a-z0-9@._+-]*$` and file paths for `-U` must be regular files inside a configured cache directory. Anything that fails validation is refused rather than escaped.
4. **No `-Sy` without `-u`.** v1's partial-upgrade guarantee still holds. The only installs are the install helper's two forms: `pacman -Syu -- X` against the real database, and `pacman -S -- X` against a temporary database synced from the Arch Linux Archive for the date of the last full upgrade. A plain `pacman -S X` against the real database is never offered.
5. **The real sync database is never written to.** cronen reads the database `checkupdates` already synced, always with `--dbpath` pointing at it, plus `--logfile /dev/null`.
6. **Every pacman call runs with `LC_ALL=C`.** v1 parses `pacman -Qi` without this, which breaks on a localised system. M7 fixes this in `internal/system`, for all commands cronen runs and not only the new ones.
7. **Protected packages** (see config) can't be removed or downgraded with a single `y`: the modal requires typing the package name.
8. **Everything new can fail on its own.** Each new data source has a spinner, an inline error, and a config switch. A failed source never blocks the rest of the UI. As in v1, it does make `--check` exit 1.

## New data sources

### 7. The checkupdates sync database
- `checkupdates` syncs into `$CHECKUPDATES_DB`, or `${TMPDIR:-/tmp}/checkup-db-${UID}/` if that's unset, and leaves the database there. Resolve the path the same way. If the directory or its `sync/` subdirectory is missing after `checkupdates` ran, every feature in this section reports "transaction preview unavailable" instead of guessing.
- **Transaction preview:** `pacman -Sup --dbpath <db> --logfile /dev/null --noconfirm --print-format '%n %v %r %s'` lists every package `-Syu` would install, with its repo and download size (0 when it's already in the cache).
  - Packages in this list that aren't installed are **new**. A new package is either a **replacement**, when its `Replaces` (from `-Si`) names an installed package (which then shows as **removed**), or a **new dependency**. For a new dependency, show which pending package depends on it.
- **Metadata:** `pacman -Si --dbpath <db> <pkg>...` (batched) gives the new `Installed Size`, `Depends On`, `Provides`, `Replaces` and `Build Date`. The old values come from `pacman -Qi`. Parse sizes (`12.34 MiB`) and `Build Date` in the C locale (`Tue Oct 21 10:00:00 2025`, local time).
- **Held packages:** `pacman -Qu --dbpath <db>` prints `IgnorePkg`/`IgnoreGroup` updates with an `[ignored]` suffix, which `checkupdates` filters out. List them as **held**.

### 8. Local package database
- `pacman -Qi` with no arguments dumps every installed package in one call. Parse name, version, description, repo origin (via the `-Qm` set from v1), install reason (`Explicitly installed` or `Installed as a dependency…`), install date, installed size, `Depends On`, `Required By`, `Optional For`, `Provides`, `Groups` and `Backup Files`. Do it once per load, in the background, and share the result between screens.
- Orphans: `pacman -Qdtq`. It exits 1 when there are none, so treat that as an empty list, like `checkupdates` exit 2.
- File lists for foreign packages (only for the rebuild scan): `pacman -Qlq <pkg>...`.

### 9. Security tracker
- `https://security.archlinux.org/all.json`: a list of advisory groups (`name` (AVG-…), `packages`, `status`, `severity`, `affected`, `fixed`, `issues` (CVE ids)). Check the field names against the live feed when writing the fixture.
- An update **fixes** a group when `installed < fixed ≤ new` (vercmp). An installed package is **vulnerable with no fix** when the group's status isn't `Fixed`, `installed ≥ affected`, and there's no `fixed` version newer than the installed one.
- Match on both `pkgname` and `pkgbase`. Cache for 6 hours. The tracker sometimes lags, so this is a hint, not a guarantee, and the UI says so once in help.

### 10. Known issues (GitLab)
- For pending repo updates: `https://gitlab.archlinux.org/api/v4/projects/archlinux%2Fpackaging%2Fpackages%2F<project>/issues?state=opened&created_after=<new build date, RFC 3339>&per_page=20`.
- `<project>` is the pkgbase as it appears in Arch's GitLab project paths. Those aren't always the pkgbase itself (`+` is spelled out, e.g. `libsigc++` → `libsigcplusplus`). Put the mapping in `internal/archweb` and have the v1 changelog link use it too; it currently escapes the pkgbase, which is wrong for those packages.
- Only look up pending repo updates, at most 4 requests at a time, cached for 1 hour. A 404 means "no project", not an error.

### 11. Mirror freshness
- The first uncommented `Server =` in `/etc/pacman.d/mirrorlist` is where pacman downloads from (ignore `file://` mirrors). Strip the `$repo/os/$arch` suffix to get the mirror root.
- Fetch `<root>/lastupdate` (a Unix timestamp of the last repo change the mirror has) and the same file from the reference mirror, `https://geo.mirror.pkgbuild.com/lastupdate`. If the mirror is more than `mirror_lag_hours` behind, warn that it's N hours behind and that `checkupdates` may be missing updates. Cache for 1 hour.

### 12. AUR history
- Commit log: `https://aur.archlinux.org/cgit/aur.git/atom/?h=<pkgbase>` (title, author, date, commit id).
- Diff: `https://aur.archlinux.org/cgit/aur.git/rawdiff/?h=<pkgbase>&id=<newest>&id2=<base>`. `<base>` is the newest commit dated at or before the installed package's `Build Date`. If none qualifies, show the full log with no diff and say why.
- Fetch the diff only when the user opens it. The log is fetched with the rest of the AUR data and uses the same cache.

### 13. Package cache
- Cache directories: `CacheDir` from `[options]` in `/etc/pacman.conf` (default `/var/cache/pacman/pkg/`), plus `extra_cache_dirs` from config (for example `~/.cache/paru/clone/*/`, with globs allowed).
- File names are `<name>-<pkgver>-<pkgrel>-<arch>.pkg.tar.{zst,xz}`. Split on the **last three** `-`, because names contain hyphens. Ignore `.sig` files and anything that doesn't parse.

### 14. System facts
- Free space via `statfs` on `/`, every cache directory, and `/boot` (or wherever the kernels' `vmlinuz-*` live).
- Mounts from `/proc/self/mounts` and declared mounts from `/etc/fstab`.
- Running kernel from `/proc/sys/kernel/osrelease`. Installed module directories from `/usr/lib/modules/`.
- `/var/lib/pacman/db.lck` exists or doesn't.
- Failed units: `systemctl --failed --plain --no-legend` and the same with `--user`.
- `.pacnew` contents: diff each file returned by `pacdiff -o` against its `.pacnew` with `diff -u` (diffutils is in `base`). A file the user can't read shows "needs root to view", not an error.

## New attention rules

Like v1's rules, these live in `internal/risk` (or a pure package it calls), get table-driven tests, and attach a reason to a package or to the global banner. Severities use v1's scale.

7. **Rebuild needed** (`warn`). After the upgrade, an installed foreign package will link against a library version that no longer exists. Two signals, both shown:
   - *Metadata:* a soname in an updated package's old `Provides` (`libicuuc.so=75-64`) is missing from its new `Provides`, and a foreign package's `Depends On` names it. Handle both soname spellings pacman uses (`libfoo.so=N-64` and the `lib:`-prefixed autodeps form); check both against real packages before relying on the format.
   - *ELF scan* (`rebuild_scan_elf`, on by default): the `DT_NEEDED` entries of ELF files owned by foreign packages, matched against the old sonames. Use `debug/elf` on a `ReaderAt` from `internal/system`. Cache results by package name and version, because this is the slow part.
   - *Versioned module directories:* a foreign package owns files under a directory tied to the old version of an updated interpreter (`/usr/lib/python3.13/`, `/usr/lib/perl5/5.40/`). Keep the table of interpreters data-driven. This replaces v1 rule 5's "Depends On python" heuristic, with the warning kept.
   - The reason names the culprit: "Needs rebuild: links against libicuuc.so.75 (icu 75.1 → 76.1)".
8. **Security fix** (`info`, marked with its own `🛡` badge and listed first in the detail pane). "Fixes AVG-2843 (High): CVE-2025-1234". It's a reason to upgrade, so it **doesn't** count toward `flagged` and doesn't change exit codes. Its own JSON field counts it.
9. **Installed and vulnerable, no fix yet** (`warn`, global). Shown in the housekeeping screen and in `--check`, never as a package flag.
10. **Known issues** (`info`, or `warn` when an issue's labels include a critical or high severity label; confirm the label names Arch actually uses). "2 open issues since this build: 'Crash on startup with Wayland'".
11. **Fresh build** (`info`, off by default: `flag_fresh_hours = 0`). The list always shows build age in the detail pane ("built 5 hours ago"). With the option set, builds younger than that are flagged.
12. **AUR maintainer changed** (`warn`). The RPC `Maintainer` (or `CoMaintainers`) differs from the one recorded in state. That includes an orphan being adopted (null → someone), which is a common hijacking pattern. With nothing recorded, the current maintainer is the baseline and nothing is flagged. The record is updated only by a successful upgrade or by acknowledging the flag, so the flag doesn't vanish on the next run without the user seeing it.
13. **Suspicious PKGBUILD change** (`warn`, only once the diff is loaded, which happens when it's opened or when `aur_review = "eager"`). Lines added in the diff that:
    - change the host of a `source=` URL,
    - add or change an `install=` script,
    - pipe a download into a shell (`curl … | sh`, `wget -O- … | bash`),
    - decode and run data (`base64 -d`, `eval`, `xxd -r`),
    - or set `sha256sums`/`b2sums` to `SKIP` for a non-VCS source.
    Each is its own reason, and the matching diff lines are highlighted.
14. **Preflight problems** (global banner, not attached to a package):
    - `critical`: `/boot` is declared in fstab but isn't mounted while a kernel is updating, or free space on `/`, the cache or `/boot` is below what the transaction needs. For the cache that's the download size; for `/` it's the installed-size delta; for `/boot` it's the current size of the updated kernels' `vmlinuz-*` plus their `initramfs-*` images.
    - `warn`: the db lock exists (another pacman is running, or it's stale; say how to tell which), the mirror is behind, or a reboot is pending (the running kernel's module directory is gone: an earlier upgrade replaced the kernel and the system hasn't rebooted).
    - `info`: `archlinux-keyring` is pending and the last full upgrade was more than 60 days ago. Suggest `pacman -Sy archlinux-keyring && pacman -Su` if signature errors appear, as the wiki does. cronen never runs it.
15. **Held package** (`info`). "Held by IgnorePkg: 1.2-1 → 1.3-1. Holding packages is a partial upgrade."

### Acknowledging flags
- `a` acknowledges every non-critical flag on the selected package **for this exact new version**. State records the package, the version, and the set of rule IDs acknowledged.
- An acknowledged package moves out of NEEDS ATTENTION into its normal group with a `✓`. Its flags stay in the detail pane, dimmed. A new version, or a rule that wasn't acknowledged, brings it back.
- Manual-intervention news (critical) and preflight problems can't be acknowledged; read the news (`m`) or fix the problem.
- `--check`, `--json` and the exit codes ignore acknowledged flags, and the summary shows "2 acknowledged".

## Write actions

They all share one flow in `internal/ops` (pure) and `internal/ui/confirm.go` (generalised from v1's upgrade modal):

1. Build the action: what was selected and with which options.
2. Run the dry run as the user and parse its output. Parse pacman's error lines too: "removing X breaks dependency Y required by Z" and "could not satisfy dependencies".
3. The modal shows:
   - the exact command line,
   - every package affected, split into *selected* and *pulled in by pacman*, with versions,
   - total size freed or changed,
   - any `.pacsave` that will be created (a modified file listed in the package's `Backup Files`),
   - warnings: protected packages, packages other installed packages need, a downgrade's reverse dependencies.
4. `y` (or typing the package name for protected packages) hands off. `esc` cancels.
5. Afterwards, reload the local database and the updates, and show what changed.

The privilege command is `privilege_command` (default `sudo`; `doas` and `run0` also work). v1's default upgrade command becomes `<privilege_command> pacman -Syu`. `upgrade_command` still overrides everything.

### Remove packages
- Mark packages with `space` in the package browser (or in the housekeeping orphan list), then press `X`.
- Dry run: `pacman -R<flags>p --print-format '%n %v' -- <names>`. The flags default to `-Rns`; in the modal, `s` toggles recursive and `n` toggles nosave, and each toggle reruns the dry run.
- Hand-off: `<privilege> pacman -R<flags> -- <names>`.
- No cascade (`-c`), ever. If removal would break a dependency, the modal lists who needs what and stops there.

### Clean up orphans
- The housekeeping screen lists orphans with their sizes and the total, all marked by default. `X` runs the removal flow on the marked ones.
- Note in the modal that removing orphans can create new orphans; `-s` already takes care of that.

### Downgrade from cache
- From the history screen or the package browser, `D` on a package lists the versions in the cache that are older than the installed one, newest first, with build date and whether a `.sig` exists.
- Dry run: `pacman -Up --print-format '%n %v' -- <file>`. Hand-off: `<privilege> pacman -U -- <file>`.
- The modal warns that the next `-Syu` will upgrade the package again unless it's added to `IgnorePkg` (cronen won't do that), and lists the installed packages that require it.
- Stretch: **revert a transaction.** On a history entry, offer to downgrade every package that transaction upgraded, in one `pacman -U`. Only offer it if every old version is in the cache, and never remove what the transaction installed.

### Merge .pacnew files
- From housekeeping, `P` hands off to `<privilege> pacdiff` (or `pacdiff_command` if set). It's interactive, so there's no dry run, but the modal still shows the command line and the list of files.

## New screens

The screen switcher becomes a tab bar under the header. `1`–`5` jump to a screen, and v1's `n` still opens news.

1. **Updates** (v1), extended:
   - New groups after AUR: `NEW (n)` (pulled in, with "needed by …" in the detail pane), `REMOVED (n)` (replaced, "replaced by …"), `HELD (n)`.
   - The detail pane adds:
     - download size and installed-size change,
     - build age,
     - security fixes,
     - known issues (`i` opens the GitLab issue list),
     - rebuild warnings,
     - for AUR packages: maintainer and co-maintainers, the latest commits, and `d` to open the PKGBUILD diff.
   - The status bar adds totals: `49 updates · 412 MiB ↓ · +38 MiB`.
   - Preflight problems join the v1 banner, most severe first. Several problems collapse into one line ("2 preflight problems: press 5"), with the details in housekeeping.
2. **News** (v1).
3. **Packages**: every installed package.
   - Columns: name, version, size, install date. Badges: `E` explicit, `D` dependency, `F` foreign, `O` orphan.
   - `f` cycles the filter (all / explicit / dependencies / foreign / orphans). `s` cycles the sort (name / size / install date). `/` filters by name, as in v1.
   - The detail pane shows the description, size, dependencies, required by, optional for, and **why it's installed**: the shortest chain of `Required By` links up to an explicitly installed package (`libxyz ← gtk4 ← nautilus (explicit)`), worked out in memory from the parsed database.
   - `space` marks, `X` removes the marked packages, `D` downgrades.
4. **History**: `pacman.log` parsed into transactions.
   - A transaction starts at `[ALPM] transaction started` and holds the `installed`/`upgraded`/`downgraded`/`reinstalled`/`removed` lines up to `transaction completed` or the next `transaction started`. It's labelled with the preceding `[PACMAN] Running '…'` command line.
   - The list shows transactions newest first (date, command, counts). The detail pane shows every change and marks each old version that's still in the cache.
   - `/` filters by package, which turns the view into that package's own history ("every nvidia-utils you've had"). `D` downgrades.
   - Handle both v1 timestamp formats, and transactions cut off by a crash (no `completed` line; mark them "interrupted").
5. **Housekeeping**:
   - preflight results,
   - reboot pending,
   - orphans (with `X`),
   - `.pacnew`/`.pacsave` files with an inline diff and `P` for pacdiff,
   - pkg cache size and file count, with a `paccache -r` hint (display only),
   - failed units,
   - installed packages that are vulnerable with no fix.

New keys:

| Key | Where | Action |
|---|---|---|
| `1`–`5` | everywhere | switch screen |
| `a` | updates | acknowledge flags on the selected package |
| `d` | updates (AUR) | open PKGBUILD diff |
| `i` | updates (repo) | open known issues in the browser |
| `space` | packages, housekeeping | mark / unmark |
| `X` | packages, housekeeping | remove marked (preview first) |
| `D` | packages, history | downgrade from cache (preview first) |
| `f` / `s` | packages | cycle filter / sort |
| `P` | housekeeping | merge `.pacnew` files with pacdiff |
| `F` | updates | prefetch: download pending packages now, install later |
| `I` | updates | install helper ("I need X now") |
| `t` | upgrade modal | toggle "take a snapshot first" |
| `S` | housekeeping | take a snapshot now |
| `R` | housekeeping (snapshot row) | restore a snapshot |
| `B` | housekeeping | run the broken-linkage scan |

## Helpers

These four features come from how cronen is actually used. The upgrade usually gets checked when bandwidth is limited, or when a package is needed urgently on a machine that hasn't been upgraded in weeks. In this household, a feature that wraps another tool is called a *helper*.

### Bandwidth helper
- **What the upgrade costs.** M7 already gives the total download (packages already in the cache count as 0). The header adds:
  - **how far behind you are:** "47 days behind · 312 packages · 1.8 GiB",
  - **growth:** state records one `(date, pending bytes)` sample per day, keeping 90, and once two samples are at least a week apart it shows the trend: "+~250 MiB/week",
  - **a time estimate.**
- **Time estimate.**
  - For every upgrade hand-off, state records the bytes it had to download, how many packages it upgraded, and how long the hand-off took. It keeps the last 10.
  - Prefetches record download-only samples, which are better data.
  - The estimate is "~14 min (from your last 6 upgrades)". With no samples it says "no estimate yet" rather than guessing. Keep the model simple, with the logic in a pure package: median throughput from the prefetch samples, plus a per-package install time from the upgrade samples.
- **Big downloads.**
  - In the updates screen, `s` sorts by download size.
  - The detail pane shows "last used ~4 months ago": the newest atime among the package's files in `bin` directories and its `.desktop` files. Show it only when the filesystem is mounted `relatime` or `strictatime` (read from `/proc/self/mounts`), never under `noatime`.
  - `space` and `X` work here too. Removing a big package you don't use before upgrading is the honest way to cut the download. Afterwards the transaction preview reruns and shows what was saved.
- **Prefetch (`F`).**
  - Hands off `checkupdates -d`, which downloads the pending packages into the cache without touching the real sync database. Check on Arch whether it needs root to write the cache, and hand off with or without `privilege_command` to match. `prefetch_command` overrides it.
  - Afterwards the preview's download size drops, and the header shows "1.7 of 1.8 GiB already downloaded".
- **Metered connection.** Read NetworkManager's `GENERAL.METERED` (`nmcli -t -g GENERAL.METERED device show <dev>`) for the device carrying the default route (from `/proc/net/route`).
  - `yes` or `yes (guessed)`, with a download bigger than `metered_warn_mib`, raises a `warn` preflight problem. The upgrade and prefetch modals repeat it.
  - Without `nmcli`, skip the check silently.
- **ParallelDownloads hint.** If `ParallelDownloads` in `[options]` means pacman downloads one file at a time (check what current pacman does when the key is missing), show an `info` hint in housekeeping and the upgrade modal: "set `ParallelDownloads = 5` in /etc/pacman.conf". cronen doesn't edit the file.

### Install helper ("I need X now")
The urgent case is the one that tempts people into `pacman -Sy X`: syncing the database and installing one package on a system weeks behind. That's a partial upgrade. X gets built against libraries newer than the ones installed, and things break later. The helper offers the two safe alternatives with their costs side by side, and explains the partial-upgrade problem once, with a link to the wiki's "Partial upgrades" section.

- **Entry points:** `cronen install <pkg>...` (opens the TUI straight into the helper), or `I` on the updates screen (a text input for package names).
- **Names** are validated like every other name and looked up in the checkupdates database (`pacman -Si --dbpath`). AUR names are looked up with the v1 RPC client.
- **Option A: full upgrade plus X.**
  - Dry run: `pacman -Sup --dbpath <checkupdates db> --print-format … -- X`. Hand-off: `<privilege> pacman -Syu -- X`. With `aur_helper` set, use `paru -Syu -- X` / `yay -Syu -- X`, which also covers AUR packages.
  - The modal shows the whole transaction's download size and time estimate, and offers a snapshot first (see the snapshot helper).
  - This is the recommended option and is selected by default.
- **Option B: X as of your last full upgrade** (experimental, `archive_install = true`, repo packages only).
  - The Arch Linux Archive keeps daily snapshots of the repos: `https://archive.archlinux.org/repos/YYYY/MM/DD/$repo/os/$arch`. Installing X from the snapshot taken when you last fully upgraded gets you a build that matches what you actually have, so you download only X and its new dependencies.
  - Only offered when the last full upgrade time is known (from `pacman.log`).
  - Steps:
    1. In a fresh `0700` directory under `$XDG_CACHE_HOME/cronen/archive/`, create `sync/`, a `local` symlink to the real local database (the same trick `checkupdates` uses), and a `pacman.conf`. The `pacman.conf` copies the real `[options]` and enables the official repos that are enabled in the real config (`core`, `extra`, `multilib`), each with the archive `Server`. Custom repos are dropped, and the modal says so.
    2. Sync it as the user, like `checkupdates` does: `fakeroot pacman -Sy --dbpath <dir> --config <dir>/pacman.conf --logfile /dev/null`. `fakeroot` is in `base-devel`. Without it, option B is shown as unavailable and says why.
    3. **Pick the day.** The snapshot for the upgrade day may be from before the upgrade, so try that day and the next. Keep the one where `pacman -Qu --dbpath <dir>` lists the fewest installed packages that differ from the snapshot. If it isn't 0, list the packages that differ in the modal and lower the confidence: "your system doesn't exactly match 2026-09-18: 3 packages differ".
    4. Dry run: `pacman -Sp --dbpath <dir> --config <dir>/pacman.conf --print-format … -- X`.
    5. Hand-off: `<privilege> pacman -S --dbpath <dir> --config <dir>/pacman.conf -- X`.
  - **The lock caveat:** this takes its lock in the temporary directory, not the real one, so right before the hand-off cronen checks that `/var/lib/pacman/db.lck` doesn't exist and refuses if it does.
  - The modal also says that the archive is slow, that the next `-Syu` upgrades X normally, and that root reads a config from a directory cronen just wrote (fresh, `0700`, regenerated for every install).
  - Afterwards, state records "X installed from the archive (2026-09-18)", and History shows it.
- **Never offered:** `-Sy X`, or `-S X` against the real database. If neither option is possible (option B disabled or unavailable and the user declines A), the helper says so and does nothing.

### Partial-upgrade detector
- **The database was synced without upgrading.** Run `pacman -Qu` against the **real** database (no sync, so it's read-only), dropping `[ignored]` lines. If it lists anything, someone ran `-Sy` without `-u`. That raises a global `warn`: "Your package database was synced without upgrading. Installing anything now is a partial upgrade. Run the full upgrade first." It becomes `critical` when the linkage scan below finds damage. It counts toward exit code 20 (30 when critical).
- **History.**
  - The History screen marks transactions whose command synced without upgrading and installed something: `-Sy X`, `-Syy X`, `-S -y X`, `--sync --refresh X`, from pacman, paru or yay.
  - Parse the options properly (short-option clusters and long options), not with a regex on the whole line.
  - Since the last full upgrade, the updates header says "2 partial upgrades since your last full upgrade (Oct 3: pacman -Sy discord)". Packages those transactions installed or upgraded get a `~` badge in the Packages screen until the next full upgrade.
- **Broken-linkage scan.**
  - On demand: `B` in housekeeping, or `cronen --scan-linkage`, which prints the result and exits `20` if anything is broken and `0` if not. It runs in the background with progress and can be cancelled.
  - Reuses M8's ELF reader across **all** installed packages' ELF files.
  - Resolution, in this order:
    1. `RUNPATH`/`RPATH` (with `$ORIGIN` expanded),
    2. the directories from `/etc/ld.so.conf` and `/etc/ld.so.conf.d/*`,
    3. `/usr/lib`, and `/usr/lib32` for 32-bit ELF.
  - Also found: a library in the requesting file's own directory, or in a RUNPATH directory of another ELF file of the same package.
  - Unresolved and a different version of the same library (same base name up to `.so`) is installed: broken, "/usr/bin/foo (foo 1.2-1) needs libbar.so.3, which no installed package provides". Only this counts for exit code 20, the critical escalation, fixes and the problem count.
  - Unresolved with no version installed: probably an optional feature. Counted separately ("unresolved, probably an optional feature: N"), listed with `--verbose`, never counted as damage.
  - Cache the results per package version.
  - Skip what's known to be dlopen-only (plugin directories); keep that list data-driven.
- **Fixes it suggests:**
  - a broken repo package → the full upgrade (or a reinstall from the cache at the installed version, if the cached file exists),
  - a broken foreign package → a rebuild, by handing off `<aur_helper> -S -- X`. Only offer the rebuild when there are no pending repo updates; otherwise say "upgrade first, then rebuild", because a rebuild against the old libraries would just break again.

### Snapshot helper (Timeshift, snapper)
- **Backends** in `internal/snapshot`: Timeshift and snapper. With `snapshot_tool = ""`, use whichever one is installed (Timeshift first). Each backend gives: detect, list (if possible), create, delete and restore argv, and how it names snapshots. Snapshot names are validated (Timeshift: `^\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}$`; snapper: a number).
- **Detecting an existing hook.**
  - If `timeshift-autosnap` or `snap-pac` is installed, pacman already takes a snapshot on every transaction. Say so in the modal and don't add a second snapshot.
  - The preflight still reports the last snapshot's age.
- **In the upgrade flow.**
  - The upgrade modal (and the install helper's option A) gets a line `☑ take a snapshot first (Timeshift)`, which `t` toggles. With `snapshot_before_upgrade = "ask"` (the default), it starts ticked when a backend is detected and no hook is. `"always"` and `"never"` do what they say.
  - When ticked, cronen chains two hand-offs:
    1. `<privilege> timeshift --create --comments "cronen: before -Syu (312 packages)"` (snapper: `<privilege> snapper -c <config> create --description "…" --cleanup-algorithm number`).
    2. Then, **only if that exited 0**, the upgrade.
  - If the snapshot fails, the upgrade doesn't start. The modal comes back with the error and a separate "upgrade without a snapshot" choice, which needs `y` again.
  - State records each snapshot cronen created (time, tool, comment).
- **Listing.**
  - snapper: `snapper -c <config> list` as the user works when the config allows it (`ALLOW_USERS`/`ALLOW_GROUPS`).
  - Timeshift: listing needs root. Use `sudo -n timeshift --list` only with `snapshot_list_sudo = true` (the one read-only exception to invariant 1); otherwise show only the snapshots cronen itself recorded, and say so.
  - Read Timeshift's mode (rsync or btrfs) and backup device from `/etc/timeshift/timeshift.json` when it's readable. Check the field names on a real install.
- **Preflight.** `warn` when the newest known snapshot is older than `snapshot_max_age_days` (default 7), and `info` when none is known. For rsync mode with the backup device mounted, also show its free space.
- **Housekeeping.** A Snapshots section with the list, `S` to take one now, `X` to delete the marked ones (`timeshift --delete --snapshot <name>`), and `R` to restore one (`timeshift --restore --snapshot <name>`; Timeshift asks its own questions after that).
  - Restore gets the heaviest confirmation in the app: the modal says the system will need a reboot and that changes since the snapshot will be lost, and it requires typing `restore`.
  - snapper restores (`snapper rollback`) are shown as a command to copy and are not handed off, because the rollback model depends on how the system boots.

## Non-interactive changes

- `--json` adds:
  - `download_bytes`, `size_delta_bytes`
  - `new`, `replaced`, `held`
  - `security_fixes`, `rebuilds`, `acknowledged`
  - `preflight`: an array of `{"severity","message"}`
  - `reboot_pending`
  - `days_behind`, `eta_seconds` (omitted with no samples), `prefetched_bytes`, `metered`
  - `partial_upgrade` (the real database was synced without upgrading), `broken_linkage` (only when a cached scan result exists)
  - `last_snapshot` (RFC 3339, omitted when none is known)

  Existing fields keep their meaning.
- New: `cronen install <pkg>...` (the install helper) and `cronen --scan-linkage` (prints broken files; exit `20` if any, `0` if none, `1` on error).
- `--check` adds lines for preflight problems, rebuilds and security fixes, ordered like the rest by severity.
- Exit code `30` now means **critical**: manual-intervention news *or* a critical preflight problem. `20` and `10` are unchanged, but ignore acknowledged flags. Document this in the README and man page as a behaviour change.
- `--waybar` `class`/`alt` use the new definitions, so a full `/boot` turns the module red.

## Config additions

```toml
privilege_command = "sudo"   # "sudo", "doas", "run0"
security = true              # security.archlinux.org advisories
known_issues = true          # GitLab issues for pending repo packages
mirror_check = true
mirror_lag_hours = 6
flag_fresh_hours = 0         # flag builds younger than this; 0 = never
rebuild_scan_elf = true
aur_review = "lazy"          # "lazy": fetch diffs when opened; "eager": fetch for every pending AUR update
protected = ["base", "linux*", "glibc", "systemd", "pacman", "sudo", "grub", "systemd-boot*"]
protected_mode = "extend"    # like watchlist_mode
extra_cache_dirs = []        # globs, e.g. ["~/.cache/paru/clone/*"]
pacdiff_command = ""         # overrides "<privilege_command> pacdiff"

# helpers
metered_warn_mib = 100       # warn on a metered connection above this download size
prefetch_command = ""        # overrides "checkupdates -d"
archive_install = true       # install helper option B (experimental)
snapshot_tool = ""           # "", "timeshift", "snapper"; "" = detect
snapshot_before_upgrade = "ask"  # "ask" (tick it when a tool is detected), "always", "never"
snapshot_max_age_days = 7
snapshot_list_sudo = false   # allow "sudo -n timeshift --list" (read-only)
snapper_config = "root"
```

State moves to `version: 2`. It adds acknowledgements (`{name, version, rules}`) and recorded AUR maintainers (`{pkgbase, maintainer, comaintainers}`). It also adds the helpers' records:
- upgrade and prefetch timing samples (last 10 of each),
- daily pending-size samples (last 90),
- snapshots cronen created,
- archive installs. A version-1 file loads as-is, and an unknown future version is treated like a corrupt file (v1's rules: never panic, start from empty).

Cache TTLs: security 6 h, GitLab 1 h, mirror 1 h, AUR atom/diff 15 min (like v1's AUR), ELF scan results until the package version changes. `r` still bypasses all of them.

## Architecture additions

```
internal/system/       + Statfs, ReadDir/Stat, Open (io.ReaderAt for ELF),
                         LC_ALL=C for every command; fakes for all of it
internal/pacman/       + syncdb.go (checkupdates db path, -Sup print, -Si, held via -Qu)
                         localdb.go (full -Qi dump), history.go (log → transactions),
                         cache.go (cache dirs, file name parsing), conf.go ([options] only)
internal/rebuild/      soname / module-dir / ELF matching (pure; ELF reading via system)
internal/preflight/    pure checks over a Facts struct gathered by system
internal/security/     tracker client + version matching
internal/gitlab/       known-issues client
internal/mirror/       mirrorlist parsing + lag
internal/aur/          + review.go (atom log, rawdiff, base-commit choice, diff heuristics)
internal/ops/          pure: build argv for remove/downgrade/pacdiff, validate names and
                         paths, parse --print output and pacman errors
internal/risk/         + rules 7–15, acknowledgements
internal/ui/           + tabs.go, packages.go, history.go, housekeeping.go, diff.go,
                         install.go; confirm.go generalised for every write action and
                         for chained hand-offs (snapshot, then upgrade)
internal/bandwidth/    pure: time estimates, growth trend, "last used" from atimes
internal/archive/      archive-install planning: day choice, pacman.conf generation (pure);
                         directory setup and sync go through system
internal/linkage/      pure resolution of DT_NEEDED against search paths; ELF via system
internal/partial/      pure: detect sync-without-upgrade commands in log transactions
internal/snapshot/     Timeshift and snapper backends: detect, list parsing, argv
```

The v1 rules still apply. On top of them: `internal/ops`, `internal/rebuild`, `internal/bandwidth`, `internal/partial` and `internal/linkage` (apart from the ELF reader) take no interfaces at all, only parsed data, so they're tested like `internal/risk`. `internal/system` gains `/proc/net/route` and `/proc/self/mounts` reads, `nmcli`, and access times through `Stat`.

## v2 testing

- Same rules as v1: no pacman, no network, no root, no Arch in tests.
- New fixtures:
  - `pacman -Sup --print-format` output with a new dependency and a replacement
  - `pacman -Si` for the pending packages, including a soname change
  - `pacman -Qu` with `[ignored]` lines
  - a full `pacman -Qi` dump (a few dozen packages covering explicit, dependency, foreign, orphan, and `Backup Files`)
  - `-Qdtq` with and without orphans
  - `-Rp` output and its "breaks dependency" error
  - `-Up` output and its "could not satisfy dependencies" error
  - a `pacman.log` with several transactions including an interrupted one
  - a cache directory listing with awkward names
  - `pacman.conf` with `CacheDir`/`IgnorePkg`
  - `mirrorlist`, `lastupdate` pairs
  - security `all.json`
  - GitLab issues JSON (plus a 404)
  - an AUR atom feed and rawdiffs: one clean, and one for each suspicious-change heuristic
  - small ELF fixtures checked in (a few KB each, with known `DT_NEEDED` entries)
  - `/proc/self/mounts` and `fstab` with and without a separate `/boot`
  - `statfs` results via the fake
  - `nmcli` metered output (`yes`, `yes (guessed)`, `no`, `unknown`) and `/proc/net/route`
  - `checkupdates -d` success and failure
  - archive install: a real `pacman.conf` with custom repos and `Include` lines, plus `-Qu --dbpath` drift for two candidate days
  - real-database `pacman -Qu` with and without pending entries
  - `pacman.log` with `-Sy X`, `-Syy X`, `-S -y X`, `--sync --refresh X`, paru and yay variants, and full upgrades that must **not** match (`-Syu`, `-Syyu`, `-Suy`)
  - ELF fixtures with `RUNPATH`/`$ORIGIN`, a missing soname, and a 32-bit binary; `ld.so.conf` with an include
  - `timeshift --list` output, `timeshift.json`, `snapper list` output, and a permission-denied `snapper list`
- Table-driven tests for rules 7–15, acknowledgement logic, `internal/ops` argv building and validation (including names and paths that must be refused), and "why installed" chains (including cycles and packages with no explicit root).
- Table-driven tests for the time estimate (no samples, one, many, outliers), the growth trend, option parsing in `internal/partial`, the archive day choice, linkage resolution, and snapshot name validation.
- Snapshot tests for each new screen and for the confirm modal with each action: plain remove, remove with a protected package, remove that breaks a dependency, downgrade, orphan cleanup, the install helper with both options (and with B unavailable), and upgrade with a snapshot. Also test the chained hand-off with the fake runner: the snapshot fails → the upgrade never runs.

### Needs checking on Arch (by the maintainer, before the milestone merges)
The PR for each milestone lists which of these it relied on. Fixtures for them should be captured from a real system, not written by hand.
- `pacman -Sup --print-format …` with `--dbpath` at the checkupdates database works as a normal user, without root and without taking the real lock (M7)
- the checkupdates database path on the current pacman-contrib (M7)
- the soname `Provides` spellings on current repo packages, including makepkg autodeps (M8)
- `pacman -Rp` and `pacman -Up` work as a normal user (M14, M15)
- the security tracker's JSON field names (M10), GitLab project-path mangling (M10), and severity label names (M10)
- whether `checkupdates -d` needs root to write the cache, and how `ParallelDownloads` behaves when it's not set (M16)
- the whole archive-install flow: the `fakeroot` sync as a normal user, `pacman -S` with `--dbpath` and `--config` writing the real local database through the symlink, and which archive day matches best (M17)
- `pacman -Qu` against the real database works without root and without a lock (M18)
- Timeshift's CLI flags and `timeshift.json` fields, and snapper's `list` permissions (M19)

## v2 milestones

M7 goes first. M8–M11 then only touch their own packages plus a short UI wiring step each, so they **can run in parallel** like M1–M3. Expect small conflicts in `internal/ui` and `internal/risk`; rebase onto `main` and don't merge it in. M12–M15 run in order. After M15, the helpers (M16–M19) run in order too. M19 only needs M14's confirm modal, so it can move up to right after M14 if a snapshot before upgrading is wanted sooner.

### M7: Transaction preview (run alone, first)
- `LC_ALL=C` for every command in `internal/system`, with a test that the real runner sets it.
- Sync database location, `-Sup` print, `-Si` batch, held packages, sizes, build dates.
- Updates screen: NEW / REMOVED / HELD groups, sizes in the detail pane and status bar, build age, rule 15, and rule 11 (off by default).
- `--json` / `--check` fields for the above.
- **Done when:** the sample output in `--demo` shows a new dependency, a replacement and a held package, all from fixtures, and the totals add up in tests.

### M8: Rebuild detection (parallel after M7)
- `internal/rebuild`: all three signals from rule 7, the ELF result cache, and v1 rule 5 folded in.
- **Done when:** the fixtures produce one rebuild warning from each signal, the false-positive cases (an unchanged soname, a debug split, a VCS package that doesn't link the library) don't, and `--demo` shows one.

### M9: Preflight (parallel after M7)
- `internal/preflight` plus the system facts it needs: disk, mounts, lock, keyring age, reboot pending, mirror lag (`internal/mirror`).
- The banner integration and rule 14. The exit-code change for critical preflight problems.
- **Done when:** every check has passing and failing table cases, and `--demo` shows a `/boot` warning.

### M10: Security and known issues (parallel after M7)
- `internal/security`, `internal/gitlab`, the GitLab project-path mapping (and the changelog link fixed to use it), rules 8–10.
- **Done when:** a pending update shows "fixes AVG-…", a repo update shows an open issue, and turning `security`/`known_issues` off makes no requests at all (asserted with the fake HTTP client).

### M11: AUR review (parallel after M7)
- Atom log, choosing the base commit, rawdiff, a diff viewer (`d`, a viewport with `+`/`-` colouring and the suspicious lines highlighted), rules 12–13, maintainer records in state v2.
- **Done when:** each heuristic has a fixture that triggers it and a clean diff triggers nothing, the maintainer-change flag survives a restart until acknowledged or upgraded, and a v1 state file still loads.

### M12: Acknowledgements
- `a`, the state records, the regrouping, `--check`/`--json`/exit code handling, and a "show acknowledged" toggle in help.
- **Done when:** the acknowledgement tests cover a new version, a new rule, and the critical flags that can't be acknowledged.

### M13: Package browser (read-only)
- Local database parsing, the Packages screen, filters, sorting, "why installed", marking (with no actions yet), and the tab bar with `1`–`5`.
- **Done when:** a full `pacman -Qi` fixture of ~1,500 packages (generated in the test) parses and renders the first frame in under 100 ms on CI, and the "why installed" tests pass.

### M14: Removal, orphans and housekeeping
- `internal/ops` for removal, the generalised confirm modal, protected packages, the `privilege_command` setting, and the Housekeeping screen (orphans, `.pacnew` diffs and `P`, cache size, failed units, vulnerable packages).
- **Done when:** the snapshot tests cover every modal case listed under v2 testing, the `ops` validation tests refuse bad names, and the hand-off is tested with the fake runner (argv exactly as expected, nothing run when the dry run fails).

### M15: History and downgrade
- Log transactions, the History screen, cache lookup, the downgrade flow from History and Packages, and (stretch) reverting a transaction.
- **Done when:** the history parser handles both timestamp formats and an interrupted transaction, the downgrade modal's tests cover a missing `.sig` and a reverse-dependency warning, and `-U` paths outside the cache directories are refused.

### M16: Bandwidth helper
- How far behind, the growth trend, the time estimate and its samples in state, sorting by download size, "last used", prefetch (`F`), the metered check, the `ParallelDownloads` hint, and the JSON fields.
- **Done when:** the estimate tests pass, prefetch is tested with the fake runner, "last used" is hidden under `noatime`, and `--demo` shows "N days behind" and an estimate from fixture samples.

### M17: Install helper
- `cronen install` and `I`, option A, and option B (`internal/archive`, behind `archive_install`), with the lock check and the "never offered" guarantees.
- **Done when:** the argv tests show that `-Sy` without `-u` and `-S` against the real database can't be produced, option B is unavailable without `fakeroot` or a known last upgrade, the day-choice tests pass, and the generated `pacman.conf` drops custom repos and `Include` lines.

### M18: Partial-upgrade detector
- The real-database `-Qu` check, History marking and the `~` badge, the linkage scan (`internal/linkage`, `B`, `--scan-linkage`), and the suggested fixes.
- **Done when:** the option-parsing table covers every variant listed under v2 testing, the linkage fixtures find exactly the broken file, and a rebuild is never offered while repo updates are pending.

### M19: Snapshot helper
- `internal/snapshot` with both backends, detecting an existing hook, the chained hand-off in the upgrade modal and in install option A, the preflight age check, and the Snapshots section in housekeeping (create, delete, restore).
- **Done when:** the chained hand-off tests pass (a failed snapshot stops the upgrade), the restore modal requires typing `restore`, the hook detection skips the extra snapshot, and listing without `snapshot_list_sudo` never runs `sudo`.

---

## Later (not v2)

> Superseded by the "After v2" section at the end of this file, which says what became a milestone and what was dropped, bullet by bullet. The list below is kept as it was written.

- Show actual upstream release notes (GitHub Releases API) in the detail pane.
- Desktop notifications via a systemd user timer and `cronen --notify` (notify only when the state changes, for example new critical news).
- Custom rules in config (`[[rule]] match = "*-dkms" severity = "info" reason = "…"`).
- `--json --full` with per-package detail, and shell completions.
- Theming: an ASCII / Nerd Font icon toggle and a configurable palette.
- Downgrades from the Arch Linux Archive for versions that aren't cached.
- Services and processes that need a restart after an upgrade (deleted libraries in `/proc/*/maps`).
- Boot sanity: every kernel has its `vmlinuz` and initramfs, bootloader entries point at real kernels, and the CPU microcode package is installed.
- AUR packages that are now in the official repos, with a switch to the repo version.
- Fixing install reasons (`pacman -D --asdeps` / `--asexplicit`) so orphan cleanup catches more.
- Integrity: `pacman -Qkk`, and reinstalling from the cache at the installed version. Also unowned files, config drift, a package list kept in dotfiles, a maintenance checklist, and cache cleanup with `paccache`.

---

# After v2: v2.x stabilisation, then v3

v2 (M7–M19) is merged. Running it on a real Arch machine showed that the weak spot is not the logic but what the logic was told about the outside world. Four v2 PRs turned out wrong only when their own "Needs checking" commands were run on a real system:

- `pacman -Rnsp` is rejected (`--nosave` and `--print` cannot be combined).
- Plain `pacman -Qi` prints no `Backup Files`; only `-Qii` does, as `path [modified|unmodified|unreadable]`.
- The linkage scan reported 66 false positives on a healthy system, because optional-feature libraries are not installed.
- Arch's GitLab uses numbered scoped labels (`severity::2-high`, `priority::1-urgent`), not the spellings the plan assumed.

All four are handled in the code on `main` now. Each had passed `make test` and `make lint` first, because the fixtures had been written to match the code. `internal/fixtures/testdata/HAND_WRITTEN.txt` still lists dozens of files that are the author's idea of the output, not a capture.

So the order is: first make the tests able to catch this class of bug (v2.x), then build new features (v3), each of which brings its own captured fixtures.

## Ground rules for everything below

1. **No new write actions in v3.** Nothing that installs, removes, restores or changes install reasons is added. Any future one must come with the v2 flow: pacman's own `--print` dry run as the user, then the confirm modal with the exact argv, then `tea.ExecProcess`, plus the argv guards and adversarial tests of `internal/ops`. Where a v3 feature could suggest a command, it shows the text and does nothing.
2. **No new fixture without provenance.** From M21 on, a PR that adds a command or a file reader ships a real capture for it, taken with the M20 script. A hand-written fixture is allowed only with an entry in `HAND_WRITTEN.txt` that says why; M22's test fails CI if the entry is missing.
3. **"Needs checking on Arch" stays mandatory,** and from M20 each PR also records the result in the ledger (see M20), so the plan always says which claims were seen on a real system, when, and on which pacman version.
4. **False positives are bugs.** Any scan that flags something must have a fixture of a healthy system that flags nothing. The linkage scan is the model: report "can't tell" rather than guess.
5. Everything in CLAUDE.md and the v1 and v2 rules stays, including `internal/system` as the only place for `os/exec`, files and network.

## What happens to the "Later (not v2)" list

| Item | Decision |
|---|---|
| Upstream release notes (GitHub Releases) | M28 |
| systemd user timer and notifications | M27 |
| Custom rules in config | M31 |
| `--json --full`, shell completions | `--json --full`: M31. Completions: M26 |
| Theming | An ASCII icon switch only (M31). **Dropped:** a configurable palette. It adds config surface that M25 would have to validate, for little gain; terminals already carry the user's palette. |
| Downgrades from the Archive for versions not cached | **Dropped.** It installs arbitrary old packages from the network, which v2's non-goals exclude. The date-matched archive install is already experimental and still unverified on a real system. |
| Services and processes needing a restart (`/proc/*/maps`) | **Dropped for now.** `/proc/<pid>/maps` of other users' processes is unreadable without root, so an unprivileged cronen would see only the user's own processes and miss the system services that matter. Running it as root is not allowed by invariant 1, and a half-view invites false confidence. `reboot_pending` (M9) stays. Revisit if a root-free source appears. |
| Boot sanity | M30 (with a post-upgrade check) |
| AUR packages now in the official repos | M29, as information only; the switch is not automated |
| Fixing install reasons (`pacman -D`) | **Dropped.** `pacman -D` has no `--print`, so it cannot meet safety invariant 2. |
| Integrity: `pacman -Qkk`, reinstall from cache, unowned files, config drift, dotfile package list, checklist, `paccache` | **Not scheduled.** `-Qkk` on a real system is noisy (modified backup files, root-only paths), so it is exactly the false-positive trap in rule 4. Reconsider after M23, with captures from several machines. The rest is a different tool. |
| Per-package download size | **Already done** in M7 (detail pane, status bar) and M16 (sorting). |
| Archive lookup ("released N days ago") | **Dropped.** M7 already shows the build date from `pacman -Si`: the same fact without another network source. |

## Tracks and order

```
v2.x stabilisation (patch releases, no new features)
  M20 verification sweep -> M21 real fixtures -> M22 golden tests + provenance ratchet   => maintainer tags v2.1
  M23 container harness      (parallel with M20-M22, its own directory)
  M24 --doctor               (after M21)
  M25 config validation      (parallel, any time after M20)                             => maintainer tags v2.2

v3 (after v2.1; each milestone ships on its own, the maintainer decides when v3.0 is tagged)
  M26 packaging              (after M23)
  M27 notifications          (after M25)
  M28 release notes   \
  M29 dependency hints  > parallel, own packages plus a short UI wiring step each
  M30 boot sanity     /
  M31 customisation and scripting output (after M25)
```

Tags are made by the maintainer, never by a session. As in v2, expect small conflicts in `internal/ui` and `internal/risk` between parallel milestones; rebase onto `main`, don't merge it in.

## v2.x stabilisation milestones

### M20: Verification sweep (run first, alone)
- `tools/capture/capture.sh`, outside `internal/` and `cmd/` and never built into the binary. POSIX shell, read-only, no root. It runs every command cronen issues and writes each one's stdout, stderr and exit code to an output directory, plus `MANIFEST.txt` (command, pacman version, date, locale). The argv table in the script is cross-checked by a Go test against the argv builders, so the two cannot drift. Missing tools are listed as `SKIPPED`. It scrubs the host name, user name, home path and machine-id, and copies nothing outside its list.
- Things that need root (the Timeshift root listing, `pacdiff` over other users' files) are not run: the script prints the command for the maintainer to run by hand and paste in.
- The ledger: a table added to the v2 "Needs checking on Arch" list with one row per item: item, command, pacman version, date, result (`confirmed`, `differs: <fix PR>`, `unverified`).
- Fixes for whatever the sweep shows. Known suspects:
  - `severity::1-*`: match scoped labels by their number (`severity::N-...`) and not by the word, so an unseen spelling still maps.
  - "Last used" (M16): desktop scanners pollute access times (on the maintainer's machine 60 of 69 `.desktop` files shared one timestamp). Detect clustering and show "unknown" instead of a date; prefer the executable's atime to the `.desktop` file's. If neither is trustworthy on the maintainer's machine, remove the column and its sort, and say so in the README.
  - Chained `tea.ExecProcess` (snapshot, then upgrade; `cronen install`): a pty-based test that runs the real binary with fake `pacman`, `sudo` and `timeshift` scripts first on `PATH` (a test-only pty dependency). It checks the order, that the terminal is handed back between the commands, that the second never runs after a non-zero exit, and that the screen redraws. The maintainer also tries it once by hand.
  - Timeshift and snapper (M19): neither is installed on the maintainer's machine, so capture from a disposable Arch VM with both. If that isn't possible, the README and the Snapshots section say "untested against a real tool" and `snapshot_before_upgrade = "ask"` doesn't tick the box by default. Never claim otherwise.
  - Archive install option B (M17): stays `archive_install = false`. Run it once in a VM (root pacman writing the real local database through the symlink, the `fakeroot` sync, the day choice). The default flips to on only if that passes, and the plan says so.
- **Done when:** the script has run on the maintainer's machine and its bundle is attached to the PR; every v2 "Needs checking" item has a ledger row; each `differs` row has a fix with a regression test; and nothing the maintainer could not check is described as verified in the plan, README or man page.
- Needs a maintainer-run step: a cloud session cannot see a real Arch system. The session writes the script, the maintainer runs it, the session continues from the bundle.

### M21: Real fixtures (after M20)
- Replace every file listed in `HAND_WRITTEN.txt` with the captured one, keeping the package set where tests count on it, or updating the tests to the real numbers. The data made up in code (`fixtures.go`, `preflight.go`, `partial.go`, `remove.go`: statfs numbers, cache listing, `ld.so.conf`, the real-database `-Qu`) is captured too; the code keeps only what cannot be captured (a full `/boot`, a failed snapshot).
- Use real `pacman -Qi` and `-Qii` dumps trimmed to a fixed package set by the script (not by hand), real `pacman.log` excerpts (including an interrupted transaction if the maintainer has one), real `-Rp` and `-Up` errors, and real small ELF files beside the in-memory ones.
- Whatever can't be captured stays in `HAND_WRITTEN.txt` with one line saying what and why. The file keeps only those.
- `--demo` data may stay synthetic, but is derived from captures, so a demo can't show a shape the real tool never prints.
- **Done when:** `make test` passes on the captures; `HAND_WRITTEN.txt` lists only files that really can't be captured, each with a reason; and the fake runner's behaviour (for example refusing `-n` with `-p`) is checked against the real stderr in the capture, not against the author's memory.

### M22: Golden tests and the provenance ratchet (after M21)
- Golden tests: each captured raw output is parsed and compared with a checked-in `.golden` file (the parsed structure as indented JSON; `go test -update` rewrites it and the diff is reviewed). A parser change that alters what a real capture produces fails loudly.
- An argv golden: one file listing every argv the app can emit (all of `internal/ops`, `pacman`, `snapshot`, `archive`). Adding a command means adding a line and a capture.
- The ratchet: a test that every file in `testdata` is listed either in `MANIFEST.txt` (captured) or in `HAND_WRITTEN.txt` (with a reason), and that no hand-written file shadows a capture of the same command.
- A healthy-system end-to-end test: the full app loaded from the captured set may produce no flags that the capture doesn't explain. This is what the 66 linkage false positives would have tripped.
- **Done when:** deliberately breaking a parser (a renamed field, a dropped `[modified]` marker) fails a golden test; adding an unlisted fixture fails the ratchet; both are shown in the PR description.

### M23: Container integration harness (parallel with M20–M22)
- `integration/`, behind a build tag so `make test` never runs it: a Dockerfile on `archlinux:base` pinned by digest, with `pacman-contrib`, `fakeroot` and `binutils`. A script builds a tiny offline local repo by hand (`bsdtar` of a `.PKGINFO` and a file or two, then `repo-add`) with sonames, optdepends, backup files, orphans, a dependency chain and a replacement, and installs some of it.
- Tests run the real binary against real `pacman -Qi`/`-Qii`/`-Qu`/`-Sup`/`-Rp`/`-Up`/`-Ql`/`-Qdtq`, `vercmp`, `checkupdates` (from a `file://` repo), the ELF linkage scan on real binaries (including a library whose optional dependency is not installed: the false-positive case) and the removal dry runs, all with `--network none`, and assert on cronen's parsed result.
- `make integration` runs it locally (Docker or Podman). CI gets a separate job; the image pull is the only network use, and only in CI. A scheduled weekly workflow runs it against `archlinux:latest`, so a pacman release that changes output is noticed before users notice.
- It does **not** replace the maintainer's checks: a container has no systemd, no real mirrors, no real boot setup and only synthetic packages. It does catch the `-Rnsp`, `-Qi`/`-Qii` and linkage classes of bug on any pacman version.
- **Done when:** `make integration` passes with no network, and fails if the `-n` with `-p` argv bug or the `-Qi` Backup Files assumption is re-introduced (each re-introduced once and noted in the PR); the CI job is green.

### M24: `cronen --doctor` (after M21)
- A table in `internal/doctor`: each "Needs checking" item as an id, the argv (built by the same functions the app uses, not a copy) and an expectation over the output (parses with cronen's parser, the expected fields are present, the exit code is in the expected set). One line per check: `ok`, `differs: <what>`, `skipped: <tool missing>` or `error`, with the pacman version at the top. `--doctor --json` for scripts. `--doctor --bundle <dir>` writes the sanitised raw outputs so a user on a derivative can attach them to an issue.
- Read-only and unprivileged: checks go through the runner, so `internal/system` stays the only place that executes. A test fails if any argv in the table is a write (`-R`, `-S` or `-U` without `-p`, `-D`, `sudo`, `systemctl restart` and so on). Checks that need root print the command instead of running it.
- Config problems from M25 appear as a section of the same report.
- Worth building because it turns the maintainer's manual sweep into something every user, and every new pacman release, can run against their own pacman instead of the author's.
- **Done when:** on the fixtures the report is all `ok`; with one capture swapped for a wrong shape, the matching line says `differs`; the no-write-argv test exists and passes; `--doctor` exits `0`, or `1` if any line is `differs` or `error`.

### M25: Configuration validation (parallel, after M20)
- Today unknown keys are reported and ignored. Add type and range errors (negative days, `mirror_lag_hours = 0`, `aur_review` not `lazy` or `eager`, a `privilege_command` that is not one of the known three or an absolute path), contradictions (`snapshot_before_upgrade = "always"` with no tool found; `archive_install = true` without `fakeroot`), a "did you mean" for near-miss keys, and the line number when the TOML library provides one.
- A bad value falls back to that key's default only; the UI shows one warning line and `--doctor` the full list. `cronen --check-config` prints the same and exits `0` or `1`.
- Values in `protected`, `extra_cache_dirs` and `pacdiff_command` go through the same validation as `internal/ops` (no shell, names pass the name regex), so a config can't loosen a safety guard. A test pins that.
- **Done when:** table tests cover each error, near-miss and contradiction; a config with every key wrong still starts on defaults and never panics; nothing in the config can disable an argv guard.

## v3 milestones

Start after v2.1 is tagged. None of these adds a write action.

### M26: Packaging and release (after M23)
- Already in place from M6: `PKGBUILD`, `.goreleaser.yaml`, `doc/cronen.1`. This milestone completes them.
- Shell completions for bash, zsh and fish, hand-written under `contrib/completions/` (the flags and the `install` subcommand are few). A test checks that every flag in `--help` appears in each file. The PKGBUILD installs them and the release archive includes them.
- Bring the man page up to v2 (every flag, subcommand, config key, the exit codes, the state version), with a test that fails if a flag or config key exists in the code and not in the page.
- Two AUR PKGBUILDs in the repo: `cronen` (tagged release, with checksums) and `cronen-git`. Both pass `namcap` and a `makepkg` build inside the M23 image in CI.
- A release workflow: on a `v*` tag, `goreleaser release` publishes the archive and `checksums.txt`; the version string comes from the tag.
- Creating the tag and publishing to the AUR are the maintainer's actions.
- **Done when:** CI builds both PKGBUILDs in the container and `namcap` is clean; `make release-check` passes; completions are covered by the flag test; the maintainer installs the built package and checks `man cronen` and tab completion on a real system.

### M27: Notifications (after M25)
- `cronen --notify` runs the same pipeline as `--check` (honouring the cache TTLs, and skipping the network on a metered connection) and compares the result with a digest in state (`notified`: critical items, news GUIDs, security fixes, preflight problems). It sends a desktop notification **only when something new appears** at or above `notify_min_severity`. Silent otherwise.
- Sending goes through `notify-send` via the runner (argv, no shell). The text comes from package names and news, so control characters are stripped and the length is capped. `libnotify` is an optdepend; no D-Bus library.
- `contrib/systemd/cronen-notify.service` and `.timer`, user units (`OnCalendar` a few times a day, `RandomizedDelaySec`, `Persistent=true`). cronen never enables them; the README shows `systemctl --user enable --now`. It never starts an upgrade and the notification has no action.
- Config: `notify = true`, `notify_min_severity = "warn"`.
- **Done when:** with the fake runner the first run notifies, a second run with the same state is silent, a new critical item notifies once, and a metered link makes no network requests; the notification text is sanitised (adversarial test); the unit files pass `systemd-analyze verify` in the M23 image.

### M28: Upstream release notes (parallel after v2.1)
- `internal/releasenotes`: for packages whose upstream URL is a GitHub repository, fetch the releases between the installed and the new version from the GitHub Releases API, shown in the detail pane on `R`. Lazy: never fetched for the whole list.
- Tag-to-version matching is the hard part (`v1.2.3`, `1.2.3`, `release-1.2.3`, pkgrel and epoch, git snapshots). Match only when the tag reduces cleanly to the package's upstream version; otherwise show nothing rather than the wrong release.
- Unauthenticated GitHub calls are limited to 60 an hour, so: cache for 6 hours, one package per request, and a plain "rate limited, try later" message. An optional token is read from the environment variable named in `github_token_env`, and is never stored in config or state.
- The body is untrusted markdown: strip control characters and cap the size before Glamour renders it.
- Config switch `release_notes`, off by default until the matching has been checked on real packages.
- **Done when:** captured fixtures (rule 2) show a clean match, a `v` prefix, a non-match, a rate-limit response and a 404; turning it off makes no request; the adversarial body test passes.

### M29: Dependency and repo-move hints (parallel after v2.1)
- Rule 16, a new dependency that pulls in something large: a package in the NEW group (M7's `-Sup`) whose download or installed size is at least `big_new_mib` (default 100), shown with the pending package that asks for it (from `Depends On` in the `-Si` blocks). Info severity, acknowledgeable like the other flags.
- Rule 17, an installed foreign package that now exists in the official repos (from the archweb lookup that already exists). Info. The detail pane says so and names `cronen install <name>`; cronen does not run it or offer to. The rule stays quiet for `-bin` and VCS packages and when `pkgbase` differs, since the same name does not always mean the same thing.
- **Done when:** table tests cover a big new dependency, a small one, a dependency that is already installed, an AUR package that moved, a `-bin` and a VCS package that must not be flagged, and the same name in a different repo; acknowledgement works.

### M30: Boot sanity and post-upgrade verification (parallel after v2.1)
- New checks in `internal/preflight` for what stops a machine booting: for each installed kernel package, `vmlinuz` and the initramfs in `/boot` (or the UKI in the ESP) exist and are non-empty; a bootloader entry points at an existing kernel; the microcode package for the CPU vendor in `/proc/cpuinfo` is installed (whether it is loaded depends on the bootloader, so that part is information only).
- Bootloaders: systemd-boot (BLS entries) and GRUB (`grub.cfg`) only. Any other, or a layout the check doesn't recognise (dracut, a UKI in a custom path), reports "can't tell" at info severity, never a warning. This is the linkage lesson: unknown is not broken.
- After a successful upgrade hand-off, run these checks again, together with the existing `.pacnew` count, reboot note and failed units, and show a one-screen "after the upgrade" summary. A missing initramfs after the upgrade is critical.
- **Done when:** fixtures from captured `/boot` layouts (systemd-boot and GRUB; the maintainer's machine plus a VM for the one it doesn't run) give no warnings; a deleted initramfs and an entry pointing at a missing kernel each give one; an unrecognised layout gives "can't tell".

### M31: Customisation and scripting output (after M25)
- Custom rules: `[[rule]] match = "*-dkms" severity = "info" reason = "..."`. Name globs only; severity `info` or `warn` only, so a user rule can never produce `critical`, create exit code 30 or bypass an acknowledgement; the reason is sanitised and capped. Validated by M25.
- `--json --full`: per-package detail (versions, flags with reasons, sizes, repo, rebuild and security data). Plain `--json` keeps its fields; both gain `"schema": 1`. The output is covered by a golden file (M22), and the man page says what may change without a schema bump.
- `icons = "unicode" | "ascii"` for the severity marks and the box drawing, with `ascii` chosen automatically when `TERM` is `linux` or `dumb`. No palette option.
- **Done when:** table tests cover a user rule (matching, not matching, asking for `critical`); the golden JSON is stable; snapshot tests of the main screens exist for `ascii`.

## v3 non-goals

- Installing, removing, restoring or re-labelling beyond what v2 offers. Downloading packages that are not already on disk, apart from v2's date-matched archive install.
- A daemon. The timer in M27 runs `cronen --notify` and exits.
- A configurable palette, a plugin system, or a scripting language in config. Custom rules are a glob and a severity.
- Mirror management, or support for non-pacman installs.
- Self-update, telemetry or bug-report uploads (`--doctor --bundle` writes files that the user chooses to share), and non-Linux ports.

## Needs checking on Arch (v2.x and v3)

Each PR records its result in the M20 ledger.

- Every open v2 item (M20). In particular: Timeshift and snapper `--list` output and CLI flags, `timeshift.json` fields and snapper permissions (a VM with both installed); the archive-install flow (`fakeroot -- pacman -Sy --dbpath ... --config ...`, the real database written through the symlink, the day choice); the `severity::1-*` label spelling, and whether the GitLab project-path mangling holds for every pending package; chained `tea.ExecProcess` on a real terminal, in at least one terminal emulator and over SSH; the "last used" heuristic and the 69-file `.desktop` case.
- M20 and M21: the capture script under a non-English locale (everything must still run with `LC_ALL=C`), and on pacman 7.x and 6.x if both are available.
- M23: that the container's `pacman -Qii`, `-Sup` and `-Up` output has the same shape as the maintainer's captures. The container is a guide, not the truth.
- M24: that `--doctor` on a derivative (EndeavourOS, CachyOS) says `ok` or a specific `differs`, and doesn't crash.
- M26: `makepkg -si` on a clean Arch install; completions in bash, zsh and fish; `man cronen`.
- M27: `notify-send` from a user timer (does the unit see `DBUS_SESSION_BUS_ADDRESS`?); behaviour after suspend and resume with `Persistent=true`; display under mako, dunst and a desktop environment's own notifier.
- M28: the GitHub Releases API fields and rate-limit headers; tag naming across the ten or twenty most-installed GitHub-hosted packages.
- M29: `Depends On` on real `-Si` output for a package with a new dependency; whether the archweb package JSON answers for every foreign package.
- M30: `/boot` layouts for systemd-boot, GRUB, a UKI and a dracut setup (`ls -lR /boot`, `bootctl status`, the vendor line of `/proc/cpuinfo`, the microcode package names), and a deliberately broken initramfs in a throwaway VM.
- M31: that `TERM=linux` renders the default icons badly enough to justify the automatic choice (look at it on a real console).
