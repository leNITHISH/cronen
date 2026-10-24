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

## Usage

```sh
cronen            # the TUI
cronen --demo     # the TUI on bundled sample data; touches nothing
cronen --check    # one-shot summary for scripts
cronen --json     # machine-readable summary
cronen --json --waybar
```

### Keys

| Key | Action |
|---|---|
| `j`/`k`, `↓`/`↑` | move |
| `g`/`G` | top / bottom |
| `ctrl+d`/`ctrl+u` | half-page in the detail or news pane |
| `tab` | switch focus between panes |
| `/` | filter the package list (`enter` keeps the filter, `esc` clears it) |
| `n` | news screen |
| `m` | mark the selected news item read (on the updates screen: the news behind the selected package's flags) |
| `o` | open the upstream URL (on the news screen: the post) with `xdg-open` |
| `c` | open the packaging changelog with `xdg-open` |
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
| `20` | attention needed: at least one update is flagged |
| `30` | an unread news item requires manual intervention |
| `1` | error: a source (checkupdates, the AUR, the news feed) failed |

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
reboot after upgrading: the kernel is updated
```

### `--json`

```console
$ cronen --json
{"updates":18,"aur":2,"flagged":11,"critical":1,"unread_news":2,"pacnew":1,"reboot":true}
```

`updates` counts repo and AUR updates together and `aur` the AUR ones.
`critical` counts unread news items requiring manual intervention. If a
source failed, an `errors` array lists what went wrong. `--json` exits 0
whenever it printed its output.

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
```

cronen remembers read news and the last upgrade it ran in
`$XDG_STATE_HOME/cronen/state.json`, and caches the news feed and AUR
responses in `$XDG_CACHE_HOME/cronen/`.
