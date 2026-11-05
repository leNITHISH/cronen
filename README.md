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
| `f` / `s` | Packages: cycle what is listed (all, explicit, dependencies, foreign, orphans) / the sort (name, size, install date). Updates: `s` sorts by download size |
| `space` | Updates, Packages, Housekeeping: mark / unmark the selected package or orphan |
| `X` | Updates, Packages, Housekeeping: remove the marked packages or orphans (dry run and confirmation first) |
| `F` | Updates: prefetch, download the pending packages now and install them later (confirmation first) |
| `I` | Updates: install helper, "I need X now" (also `cronen install <package>...`) |
| `D` | Packages, History: downgrade the selected package to an older version from the package cache (dry run and confirmation first) |
| `P` | Housekeeping: merge `.pacnew` files with `pacdiff` (confirmation first) |
| `t` | upgrade modal, install helper option A: tick or untick "take a snapshot first" (only with Timeshift or snapper installed) |
| `S` / `R` | Housekeeping, with Timeshift or snapper installed: take a snapshot now / restore the snapshot under the cursor (confirmation first; `space` marks and `X` deletes snapshots when the cursor is on the Snapshots list) |
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
{"updates":18,"aur":2,"flagged":11,"acknowledged":0,"critical":1,"unread_news":2,"pacnew":1,"reboot":true,"download_bytes":929069795,"size_delta_bytes":27053259,"new":2,"replaced":1,"held":1,"security_fixes":2,"vulnerable":1,"preflight":[{"severity":"critical","message":"/boot is in fstab but not mounted while linux updates: …"}],"reboot_pending":false,"days_behind":2,"eta_seconds":679,"prefetched_bytes":0,"metered":false}
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

`last_snapshot` (RFC 3339) is the time of the newest known snapshot; it is absent when
no snapshot tool is installed or none is known.

The bandwidth helper adds `days_behind` (days since the last full upgrade, 0 when
unknown), `eta_seconds` (the time estimate; absent until cronen has timing samples),
`prefetched_bytes` (what is already in the package cache) and `metered`.

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
metered_warn_mib = 100     # warn on a metered connection above this download; 0 turns the check off
prefetch_command = ""      # replaces "checkupdates -d" for F; run as written, add sudo yourself
archive_install = false    # install helper option B (experimental); see "Install helper"
partial_check = true       # pacman -Qu on the real database, History marks and the ~ badge
linkage_skip = []          # more directory globs the broken-linkage scan leaves out
snapshots = true           # the snapshot helper: look for Timeshift or snapper, show the Snapshots section
snapshot_tool = ""         # "", "timeshift" or "snapper"; "" uses whichever is installed, Timeshift first
snapshot_before_upgrade = "ask"  # "ask": tick the box when a tool is found and no pacman hook snapshots; "always"; "never"
snapshot_max_age_days = 7  # preflight warns when the newest known snapshot is older than this
snapshot_list_sudo = false # allow `sudo -n timeshift --list` (it only reads); otherwise only snapshots cronen took are listed
snapper_config = "root"    # the snapper config cronen takes snapshots of
```

### Partial-upgrade detector

A partial upgrade is `pacman -Sy X` (or `-Syy`, `-S -y`, `--sync --refresh`, from
pacman, paru or yay) without `-u`: the databases move on, the system doesn't.
cronen looks for three things, all read-only:

- **The database was synced without upgrading.** `pacman -Qu` against the real
  database (no sync) lists packages with newer versions waiting. That raises a
  warning banner, counts as exit code `20` for `--check`, and is `--json`'s
  `partial_upgrade`. It becomes critical (exit `30`) when the last linkage scan
  found damage.
- **History.** The History screen marks transactions that synced without
  upgrading and installed something (`~`); the header says "2 partial upgrades
  since your last full upgrade (Oct 3: pacman -Sy discord)", and the Packages
  screen gets a `~` column for the packages they installed or upgraded, until the
  next full upgrade.
- **Broken linkage.** `B` on the Housekeeping screen (Esc cancels), or
  `cronen --scan-linkage`, reads the ELF files of every installed package and
  reports each library that is installed in a different version than the one a
  program needs, which is what a partial upgrade or a soname bump leaves
  ("/usr/bin/foo (foo 1.2-1) needs libbar.so.3, which no installed package
  provides" while libbar.so.2 is there). Only these count as broken.
  A library installed in no version is most likely an optional feature (a
  plugin whose optional dependency was never installed): it is counted as
  "unresolved, probably an optional feature", listed with `--verbose`, and never
  changes the exit code or the severity. A library next to the file that needs
  it, or in a RUNPATH directory of another file of the same package, is found.
  `--scan-linkage` exits `20` if anything is broken, `0` if not, `1` on an error. Libraries are looked up the way the
  loader does: RUNPATH/RPATH (`$ORIGIN` expanded), `/etc/ld.so.conf` and
  `/etc/ld.so.conf.d/*`, then `/usr/lib` (`/usr/lib32` for 32-bit files). ELF
  data is cached per package version in `$XDG_CACHE_HOME/cronen/linkage.json`;
  plugin directories that are only `dlopen`ed are skipped (a table in
  `internal/linkage/skip.go`, extended by `linkage_skip`). `--json` reports
  `broken_linkage` only while a cached scan matches the installed packages.

Suggested fixes: for a broken repo package, the full upgrade (or a reinstall from
the cache at the installed version, shown as a command); for a broken foreign
package, a rebuild with your AUR helper (`b`, after a confirmation), offered only
when no repo updates are pending. Otherwise cronen says "upgrade first, then
rebuild".

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

### Bandwidth helper

Under the tab bar, the updates line says what the upgrade costs: `47 days behind ·
312 packages · 1.8 GiB · +~250 MiB/week · ~14 min (from your last 6 upgrades)`.

- **Growth.** cronen keeps the size of the pending download once a day (90 days,
  in the state file; `--check`/`--json` runs also add the day's sample). Once the latest
  growing stretch spans a week, the weekly growth shows. An upgrade resets it.
- **Time estimate.** Every successful upgrade hand-off is timed (bytes to download,
  packages, duration; the last 10 are kept), and so is every prefetch. The estimate is
  the median download speed of the prefetches plus the median install time per package
  from the upgrades; with no prefetch it uses the median speed of whole upgrades. With no
  samples it says "no estimate yet". The time you spend at pacman's `[Y/n]` prompt is in
  the upgrade's duration, so keep answering quickly or prefetch first.
- **Big downloads.** `s` on the updates screen sorts by download size and shows the
  size in the list. The detail pane shows "last used ~4 months ago": the newest access time
  among the package's files in `bin` directories and its `.desktop` files. It is only
  shown when the filesystem holding them is not mounted `noatime`; under `relatime` it is
  accurate to about a day. `space` marks an installed package and `X` removes the marked ones; the
  preview reruns afterwards and the notice says how much the download shrank.
- **Prefetch (`F`).** Runs `checkupdates -d`, which downloads the pending packages into the
  package cache from checkupdates' own temporary database and installs nothing, so it
  can't cause a partial upgrade. It needs the transaction preview (its dry run) and
  something left to download. The privilege command is added only when you can't write to
  any package cache directory; `prefetch_command` replaces the command and is run as
  written. Afterwards the header says "1.7 of 1.8 GiB already downloaded".
- **Metered connection.** The default route comes from `/proc/net/route`, and
  `nmcli -t -g GENERAL.METERED device show <dev>` says whether NetworkManager calls it
  metered (`yes` or `yes (guessed)`). Above `metered_warn_mib` this is a `warn`
  preflight problem, repeated in the upgrade and prefetch modals. Without `nmcli`,
  NetworkManager or a default route, the check is skipped silently.
- **ParallelDownloads.** When `ParallelDownloads` isn't set (or is 1) in
  `/etc/pacman.conf`, pacman downloads one file at a time; the Housekeeping screen and
  the upgrade modal suggest `ParallelDownloads = 5`. cronen doesn't edit the file.

### Install helper

`cronen install <package>...` (or `I` on the updates screen) is for "I need X now" on a
system that is behind. It never runs `pacman -Sy X`, or `pacman -S X` against the real
database: that is a partial upgrade. Names are validated like every other name, and
looked up in the database checkupdates synced (and in the AUR when `aur_helper` is
set). Two options are shown side by side, and nothing runs until a dry run as you
succeeded and you press `y`:

- **A. Full upgrade, then X** (recommended, selected by default).
  Dry run: `pacman -Sup --dbpath <checkupdates db> … -- X`. Hand-off:
  `<privilege_command> pacman -Syu -- X`, or `paru -Syu -- X` / `yay -Syu -- X` with
  `aur_helper` set (which also covers AUR packages). `upgrade_command` is not used.
- **B. X as of your last full upgrade** (experimental, repository packages only, off
  unless `archive_install = true`). It needs `fakeroot` (`base-devel`) and a last full
  upgrade time in `pacman.log`. Pressing `b` makes a fresh `0700` directory under
  `$XDG_CACHE_HOME/cronen/archive/` for each of the two candidate days (the upgrade day
  and the next), each with a `local` symlink to your real local database and a
  generated `pacman.conf`, syncs them from the Arch Linux Archive as you with
  `fakeroot -- pacman -Sy --dbpath <dir> --config <dir>/pacman.conf`, and keeps the day
  where `pacman -Qu` lists the fewest differing packages (the modal lists them).
  The generated config keeps your `[options]` and enables only `core`, `extra` and
  `multilib` (if enabled), each with just the archive `Server`; custom repositories,
  every `Include` line, `DBPath` and `XferCommand` are left out, and the modal says so.
  Hand-off: `<privilege_command> pacman -S --dbpath <dir> --config <dir>/pacman.conf -- X`.
  Because that lock lives in the temporary directory, cronen checks that
  `/var/lib/pacman/db.lck` doesn't exist, and that the directory and config are
  unchanged, right before the hand-off. Directories are not deleted afterwards.

State records "X installed from the archive (day)"; `pacman.log` (History) shows the
transaction.

### Snapshot helper

With Timeshift or snapper installed (`snapshot_tool = ""` uses whichever is found,
Timeshift first), the upgrade modal and the install helper's option A get a line
`☑ take a snapshot first (Timeshift)` that `t` toggles. With
`snapshot_before_upgrade = "ask"` (the default) it starts ticked when a tool is found and no
pacman hook already snapshots; `"always"` ticks it whenever a tool is found, `"never"`
hides it. Ticked, cronen chains two hand-offs:

1. `<privilege_command> timeshift --create --comments "cronen: before -Syu (312 packages)"`
   (snapper: `<privilege_command> snapper -c <snapper_config> create --description "…" --cleanup-algorithm number`)
2. then, **only if that exited 0**, the upgrade.

If the snapshot fails the upgrade does not start. The modal comes back with the error and
a separate choice, "upgrade without a snapshot", which needs `y` again (`t` ticks the
snapshot to retry). cronen records each snapshot it took in the state file (time, tool,
comment).

If `timeshift-autosnap` or `snap-pac` is installed, pacman already snapshots on every
transaction: the modal says so and, under `"ask"`, adds none.

The Housekeeping screen has a Snapshots section: `S` takes one now, `space` marks
snapshots and `X` deletes the marked ones (`timeshift --delete --snapshot <name>`,
`snapper -c <config> delete <number>`, one command each, stopping at the first
failure), and `R` restores the one under the cursor. A Timeshift restore
(`timeshift --restore --snapshot <name>`; Timeshift then asks its own questions) is the
heaviest confirmation in cronen: it says the system needs a reboot and everything
changed since the snapshot is lost, and you must type `restore`. A snapper rollback
depends on how the system boots, so it is only shown as a command to copy and is never
run. Snapshot names are validated (Timeshift `YYYY-MM-DD_HH-MM-SS`, snapper a number) and
passed as separate arguments; every command is checked against the exact allowed forms
right before it runs.

Listing: `snapper -c <config> list` runs as you and works when the config's `ALLOW_USERS`
or `ALLOW_GROUPS` allows it; permission-denied output is shown as such, never as an
empty list. Timeshift can only be listed as root: with `snapshot_list_sudo = true` cronen
runs `sudo -n timeshift --list` (the only command it runs with a privilege command that is
not a hand-off; it only reads, and `-n` never asks for a password), otherwise only the
snapshots cronen took are shown. A list cronen cannot make sense of is reported as "could
not read the snapshot list", never guessed. Timeshift's mode (rsync or btrfs) and, for
rsync with the backup device mounted, its free space come from
`/etc/timeshift/timeshift.json`.

Preflight warns when the newest known snapshot is older than `snapshot_max_age_days`, and
notes when none is known. With neither tool installed, none of this shows anywhere.

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
