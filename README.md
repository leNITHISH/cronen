# cronen

A terminal UI that tells you what you're about to get into **before** you run
`pacman -Syu`: pending repo and AUR updates, the Arch news you haven't read,
anything that needs manual intervention, and risky packages (kernel, GPU
drivers, glibc, systemd…), with links to upstream and the packaging
changelog. When you're happy, it hands off to `sudo pacman -Syu` (or your AUR
helper).

cronen only reads. It never runs `pacman -Sy`, so it can't cause a partial
upgrade: pending updates come from `checkupdates`, which syncs a temporary
copy of the database.

![cronen --demo: flagged updates, the news screen and the upgrade confirmation](doc/demo.gif)

## Install

cronen is for Arch Linux and its derivatives.

From a clone, build and install the package with the bundled `PKGBUILD`
(it builds the latest commit on `main` as `cronen-git`):

```sh
git clone https://github.com/leNITHISH/cronen.git
cd cronen
makepkg -si
```

Or with Go, which installs only the binary:

```sh
go install github.com/leNITHISH/cronen/cmd/cronen@latest
```

### Dependencies

| | Package | For |
|---|---|---|
| depends | `pacman-contrib` | `checkupdates` (pending updates) and `pacdiff` (the `.pacnew` count) |
| optdepends | `xdg-utils` | open upstream and changelog links |
| optdepends | `paru` | AUR upgrades |
| optdepends | `yay` | AUR upgrades |
| makedepends | `go` | building |

`makepkg` also needs `git` to fetch the source. `pacman` and `vercmp` come with
every Arch install.

## Usage

```sh
cronen            # the TUI
cronen --demo     # the TUI on bundled sample data; touches nothing
cronen --check    # one-shot summary for scripts
cronen --json     # machine-readable summary
cronen --json --waybar
cronen --version
```

`cronen --help` summarises the flags, exit codes and files, and `man cronen`
documents everything, including the config keys.

### Keys

| Key | Action |
|---|---|
| `j`/`k`, `↓`/`↑` | move |
| `g`/`G` | top / bottom |
| `ctrl+d`/`ctrl+u` | half-page in the detail or news pane |
| `tab` | switch focus between panes |
| `/` | filter the package list (`enter` keeps the filter, `esc` clears it) |
| `1`–`5` | switch screen: Updates, News, Packages, History, Housekeeping |
| `n` | news screen |
| `f` / `s` | Packages: cycle what is listed (all, explicit, dependencies, foreign, orphans) / the sort (name, size, install date) |
| `space` | Packages, Housekeeping: mark / unmark the selected package or orphan |
| `X` | Packages, Housekeeping: remove the marked packages or orphans (dry run and confirmation first) |
| `D` | Packages, History: downgrade the selected package to an older version from the package cache (dry run and confirmation first) |
| `P` | Housekeeping: merge `.pacnew` files with `pacdiff` (confirmation first) |
| `m` | mark the selected news item read (on the updates screen: the news behind the selected package's flags) |
| `o` | open the upstream URL (on the news screen: the post) with `xdg-open` |
| `c` | open the packaging changelog with `xdg-open` |
| `d` | review the PKGBUILD diff of the selected pending AUR update (`esc` or `d` closes it) |
| `r` | refresh everything, bypassing the cache |
| `U` | upgrade: confirm with `y`, then cronen hands the terminal to the upgrade command |
| `?` | help |
| `q`, `ctrl+c` | quit |

Opening links needs `xdg-open` from `xdg-utils`. After an upgrade cronen
reloads everything and tells you how many `.pacnew` files are waiting for
`pacdiff`, and whether to reboot for a new kernel. Only a successful upgrade
(exit code 0) marks the news up to now as read.

## Non-interactive modes

### `--check`

Prints a short plain-text summary, most important first, and exits with:

| Code | Meaning |
|---|---|
| `0` | nothing to do |
| `10` | updates available |
| `20` | attention needed: at least one update is flagged (acknowledged flags don't count) |
| `30` | critical: an unread news item requires manual intervention, or a preflight check found a critical problem |
| `1` | error: a source (checkupdates, the AUR, the news feed) failed |

Exit code `30` changed meaning in this version: it used to mean only the news
case, and now also covers a critical preflight problem (a full or unmounted
`/boot` while a kernel updates, or too little free space for the upgrade).

Any failed source makes the exit code `1`, even if the rest looked fine: with
a source missing, cronen can't tell "nothing to do" from "didn't look". An
unreadable `pacman.log` is not an error; the last upgrade is then unknown and
news from the last `news_days` days counts as unread.

```console
$ cronen --check
18 updates (2 AUR) · 11 flagged · 2 unread news · 1 .pacnew
‼ manual intervention: linux-firmware >= 20251021-1 upgrade requires manual intervention
‼ linux-firmware: Manual intervention in news: '…' (yesterday)
⚠ nvidia-utils: Mentioned in news: 'NVIDIA 590 driver drops Pascal support' (yesterday)
…
transaction: 886 MiB download, +26 MiB on disk, 2 new, 1 replaced
ℹ bash: Held by IgnorePkg: 5.3.3-1 → 5.3.3-2. Holding packages is a partial upgrade.
reboot after upgrading: the kernel is updated
```

### `--json`

```console
$ cronen --json
{"updates":18,"aur":2,"flagged":11,"acknowledged":0,"critical":1,"unread_news":2,"pacnew":1,"reboot":true,"download_bytes":929069795,"size_delta_bytes":27053259,"new":2,"replaced":1,"held":1,"security_fixes":2,"vulnerable":1,"preflight":[{"severity":"critical","message":"/boot is in fstab but not mounted while linux updates: …"}],"reboot_pending":false}
```

`updates` counts repo and AUR updates together and `aur` the AUR ones.
`critical` counts unread news items requiring manual intervention. If a
source failed, an `errors` array lists what went wrong. `--json` exits 0
whenever it printed its output.

The transaction preview adds `download_bytes` and `size_delta_bytes` (the
change in installed size), `new` (packages the upgrade pulls in, including
replacements), `replaced` (installed packages a replacement removes) and
`held` (updates held back by `IgnorePkg`). Held packages are not updates and
never count as flagged.

`preflight` lists the preflight problems as `{"severity","message"}` objects,
most severe first, and `reboot_pending` says an earlier upgrade replaced the
running kernel and the system hasn't rebooted since.

`security_fixes` counts the advisory groups (AVG-…) the pending updates fix and
`vulnerable` the installed packages affected by a group no version fixes yet.

### Preflight

Before you upgrade, cronen checks for the things that make an upgrade fail
halfway. It shows the worst as a banner (several collapse into one line, and
`U` lists them all) and in `--check`:

- critical: `/boot` is in `/etc/fstab` but not mounted while a kernel is
  updating, or the root filesystem, `/boot` or the pacman cache doesn't have
  the free space the upgrade needs (the download for the cache, the installed
  size change for `/`, the current size of the updating kernels' images for
  `/boot`);
- warning: `/var/lib/pacman/db.lck` exists, the first mirror in the mirrorlist
  is more than `mirror_lag_hours` behind the reference mirror, or a reboot is
  pending (the running kernel's `/usr/lib/modules/<release>` is gone);
- info: `archlinux-keyring` is pending and the last full upgrade was more than
  60 days ago.

cronen only reports these; it never mounts, removes or runs anything. A mirror
that can't be reached counts as a failed source (`--check` exits 1).

### Security and known issues

An update that fixes an advisory from
[security.archlinux.org](https://security.archlinux.org) is marked with a 🛡
and the detail pane says which ("Fixes AVG-2901 (High): CVE-…"). It is a reason
to upgrade, so it never counts as flagged and never changes the exit codes. An
installed package that an unfixed advisory affects is counted in the header
("1 vulnerable, no fix") and listed by `--check`. The tracker sometimes lags: it
is a hint, not a guarantee. Set `security = false` to turn it off.

A pending repo update with open issues on the Arch packaging GitLab since its
new build is flagged ("2 open issues since this build: …"), as a warning when
one has a critical or high severity label. Set `known_issues = false` to turn
it off. With either off, cronen makes no request to that service at all.

### Transaction preview

The Updates screen shows what `pacman -Syu` would do, not only which versions
change: `NEW` lists packages the upgrade pulls in (with what needs them, or
what they replace), `REMOVED` the installed packages a replacement removes and
`HELD` the updates `IgnorePkg` holds back. The detail pane adds the download
size, the installed-size change and how long ago the new build was made, and
the status bar the totals (`18 updates · 886 MiB ↓ · +26 MiB`).

cronen reads the database `checkupdates` already synced (`$CHECKUPDATES_DB`,
or `${TMPDIR:-/tmp}/checkup-db-<uid>`) with `pacman -Sup --dbpath … --print-format`
as your user. It never writes to the real database. If that database isn't
there, the preview says so and the rest of cronen carries on. Set
`preview = false` to turn it off.

### Waybar

`cronen --json --waybar` prints a [custom module][waybar-custom]'s `text`
(the number of updates, empty when there are none so waybar hides the module,
or `!` if a source failed), a `tooltip` with the `--check` summary, and the
same state as `class` and `alt`: `none`, `updates`, `attention`, `critical` or
`error`.

```jsonc
"custom/cronen": {
    "exec": "cronen --json --waybar",
    "return-type": "json",
    // checkupdates syncs a temporary database from your mirror on every
    // run, so don't poll often. News and AUR responses are cached for 15 min.
    "interval": 1800,
    "format": "{icon} {}",
    "format-icons": {
        "updates": "↻",
        "attention": "⚠",
        "critical": "‼",
        "error": "✗"
    },
    "on-click": "foot -e cronen"
}
```

```css
#custom-cronen.attention { color: #e8c26a; }
#custom-cronen.critical,
#custom-cronen.error { color: #f2b8b5; }
```

Replace `foot` with your terminal.

[waybar-custom]: https://github.com/Alexays/Waybar/wiki/Module:-Custom

## Configuration

`$XDG_CONFIG_HOME/cronen/config.toml` (default
`~/.config/cronen/config.toml`). Every key is optional:

```toml
aur = true
aur_helper = ""            # "", "paru", "yay": upgrade with paru -Syu / yay -Syu
upgrade_command = ""       # overrides everything if set
watchlist = ["linux*", "nvidia*", "mesa", "glibc", "systemd"]
watchlist_mode = "extend"  # "extend" the defaults or "replace" them
news_days = 30
ignore_major = []          # globs whose major version bumps aren't flagged
show_debug = false         # list makepkg <name>-debug splits
preview = true             # transaction preview: new, replaced and held packages, sizes
flag_fresh_hours = 0       # flag updates built less than this many hours ago; 0 = never
preflight = true           # check /boot, free space, the db lock, a pending reboot, the keyring
mirror_check = true        # also compare the first mirror with the reference mirror
mirror_lag_hours = 6       # a mirror this far behind (or more) is reported
rebuild_scan_elf = true    # also scan foreign packages' ELF files for libraries an upgrade drops
security = true            # security.archlinux.org advisories
known_issues = true        # open GitLab issues for pending repo updates
package_browser = true    # the Packages screen: one `pacman -Qi` for every installed package
aur_review = "lazy"        # "lazy": fetch PKGBUILD diffs when opened (d); "eager": for every pending AUR update
privilege_command = "sudo" # "sudo", "doas" or "run0": prefixes every write action and the default upgrade
protected = []             # globs; removing one needs its name typed (added to the defaults below)
protected_mode = "extend"  # "extend" the defaults (base, linux*, glibc, systemd, pacman, sudo, grub, systemd-boot*) or "replace"
extra_cache_dirs = []      # globs of more package cache directories, e.g. ["~/.cache/paru/clone/*"]
pacdiff_command = ""       # replaces "<privilege_command> pacdiff"
housekeeping = true        # the Housekeeping screen's .pacnew diffs, cache size and failed units
history = true             # the History screen (pacman.log) and the cache lookup behind D
```

Rebuild detection (needs `preview` and `aur`) flags the update behind each
installed AUR package that will need a rebuild after the upgrade: one that
depends on or links against a shared library version the update stops
providing, or that is installed under the old `/usr/lib/python3.13/`-style
directory of an updated interpreter. The ELF scan is the slow part, so its
results are cached in `$XDG_CACHE_HOME/cronen/elf-needs.json` until a package's
version changes (`r` rescans).

### Packages

`3` lists every installed package with its version, size and install date, and
the badges `E` explicit, `D` dependency, `F` foreign and `O` orphan. The detail
pane shows what a package depends on, what requires it, and **why it's
installed**: the shortest chain of "required by" links up to a package you
installed explicitly (`libx11 ← gtk3 ← firefox (explicit)`). `/` filters by
name, `f` and `s` change the filter and the order, and `space` marks packages.
It is read-only for now. cronen reads the package database once per load with
`pacman -Qi`, `pacman -Qm` and `pacman -Qdtq`; `package_browser = false`
turns that off.

### Removing packages and housekeeping

Mark packages with `space` on the Packages screen and press `X`, or press `X`
on the Housekeeping screen (`5`) to remove the marked orphans (all of them start
marked). cronen first runs `pacman -R[s]p --print-format '%n %v' -- <names>`
as you (never with `-n`: pacman refuses `-n` together with `-p`, and `-n` does
not change what is removed), and the modal shows the exact command, what is selected and what `-s`
pulls in, the size freed, the modified config files that become `.pacsave` (read lazily with
`pacman -Qii -- <those packages>`, only without `-n`), and warnings.
`s` and `n` toggle `-s` and `-n` and rerun the dry run (default `-Rns`; there is
no `-c`). If the dry run fails, the modal shows pacman's error and offers no way
to proceed. Packages matching `protected` need their name typed (then `ctrl+s`
and `ctrl+n` are the toggles). `y` hands the terminal to
`<privilege_command> pacman -R<flags> -- <names>`; cronen never changes the
system itself.

Housekeeping also shows the preflight results, a pending reboot, `.pacnew` and
`.pacsave` files with an inline diff (`P` hands off to `pacdiff`), the package
cache size, failed systemd units and installed packages that are vulnerable with
no fix. Each of these reads can fail on its own without breaking the rest.

### History and downgrade

`4` shows `pacman.log` as transactions, newest first: date, the command that
ran them and counts (`+` installed, `↑` upgraded, `↓` downgraded, `⟳`
reinstalled, `-` removed). The detail pane lists every change and marks with `●`
each old version that is still in the package cache. A transaction with no
`transaction completed` line is marked interrupted. `/` filters by package and
turns the list into that package's own history. Both timestamp formats of
`pacman.log` are read.

`D` (History or Packages) lists the versions of the selected package in the
cache (pacman's `CacheDir` plus `extra_cache_dirs`) that are older than the
installed one, newest first, with build date and whether a `.sig` is beside the
file. After you pick one, cronen runs `pacman -Up --print-format '%n %v' -- <file>`
as you, and the modal shows the exact command, what pacman would install, a
warning for a missing `.sig`, the installed packages that require it, and that the
next `-Syu` upgrades it again unless you add it to `IgnorePkg` (cronen never
edits `pacman.conf`). Protected packages need their name typed. `y` hands the
terminal to `<privilege_command> pacman -U -- <file>`. Only regular files directly
inside a configured cache directory are accepted. `history = false` turns the
History screen and `D` off.

### AUR review

The detail pane of a pending AUR update shows the maintainer and
co-maintainers and the latest commits of its AUR git history. `d` opens the
PKGBUILD diff between the commit your installed build most likely came from
(the newest one dated at or before its build date) and the newest commit, with
`+`/`-` colouring. Two rules raise a warning:

- **Maintainer changed.** The maintainer or co-maintainers differ from the
  ones cronen recorded (an orphan being adopted counts). The first maintainers
  cronen sees are the baseline, so nothing is flagged on a first run. The record
  only changes after a successful upgrade, so the flag stays across restarts.
- **Suspicious PKGBUILD change**, once the diff is loaded (when you open it, or
  for every update with `aur_review = "eager"`, which also makes `--check` and
  `--json` fetch them). Added lines that change the host of a `source=` URL,
  add or change an `install=` script, pipe a download into a shell, decode and
  run data (`base64 -d`, `eval`, `xxd -r`) or set a checksum to `SKIP` for a
  non-VCS source. The matching lines are highlighted.

These are hints for a human review, not a verdict: cronen can't tell a
legitimate mirror change from a malicious one.

### Acknowledging flags

`a` acknowledges every non-critical flag on the selected package, for that
exact new version. The package leaves NEEDS ATTENTION for its normal group, marked
`✓`, and its flags stay in the detail pane, dimmed. A newer version brings the
package back, and so does a rule you hadn't acknowledged. For a maintainer change
(rule 12), acknowledging also records the current maintainers as accepted.
`A` hides or shows acknowledged packages in the list.

Manual-intervention news and preflight problems are critical and can't be
acknowledged: read the news (`m`) or fix the problem. `--check`, `--json` and the
exit codes ignore acknowledged flags; the summary says "2 acknowledged" and
`--json` has an `acknowledged` field. Acknowledgements live in the state file.

cronen remembers read news and the last upgrade it ran in
`$XDG_STATE_HOME/cronen/state.json`, and caches the news feed and AUR
responses in `$XDG_CACHE_HOME/cronen/`.

## Development

`make test` and `make lint` are what CI runs. `make gif` re-records
`doc/demo.gif` from `doc/demo.tape` with [vhs](https://github.com/charmbracelet/vhs)
(it also needs `ttyd`, `ffmpeg` and Chromium). `make release-check` validates
`.goreleaser.yaml`, and `make snapshot` builds release archives into `dist/`
without publishing anything. Tool versions are pinned in the `Makefile`.

## License

MIT, see [LICENSE](LICENSE).
