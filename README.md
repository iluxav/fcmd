# fcmd

A terminal dual-pane file manager for local and LAN-connected machines.
Left pane is your local filesystem; the right pane discovers other
`fcmd` daemons on the LAN via mDNS and lets you browse and transfer
files between them.

- Zero-config LAN discovery (`_fcmd._tcp`)
- Dual-pane TUI with multi-select, copy, move, rename, delete
- Chunked transfers with overall/per-file progress, speed, and ETA
- Single static Go binary for Linux, macOS, and Windows
- Runs as a background service via systemd (Linux) or launchd (macOS)

## Install (one-liner, Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/iluxav/fcmd/main/install.sh | bash
```

That single command:

1. Detects your OS/arch and downloads the matching binary from the
   latest GitHub release.
2. Installs it to `/usr/local/bin/fcmd` (may prompt for `sudo`).
3. Registers and starts the daemon via **systemd** on Linux or
   **launchd** on macOS.
4. On re-run, upgrades the binary and restarts the service.

### Installer options

Optional environment variables (pass them right before `bash` so the
piped shell sees them):

| Variable       | Default          | Purpose                          |
|----------------|------------------|----------------------------------|
| `FCMD_VERSION` | `latest`         | Pin a specific release tag       |
| `FCMD_PREFIX`  | `/usr/local/bin` | Install directory for the binary |
| `FCMD_REPO`    | `iluxav/fcmd`    | Source repo (for forks)          |

Examples:

```bash
# Install a specific version
curl -fsSL https://raw.githubusercontent.com/iluxav/fcmd/main/install.sh \
  | FCMD_VERSION=v0.2.0 bash

# Install into ~/.local/bin without sudo
curl -fsSL https://raw.githubusercontent.com/iluxav/fcmd/main/install.sh \
  | FCMD_PREFIX="$HOME/.local/bin" bash
```

## Requirements

- **Running the TUI**: a terminal (no extra runtime).
- **Building from source**: Go 1.24+.
- **LAN discovery**: mDNS must be reachable between hosts (most home/office networks work out of the box).

## Build from source

```bash
git clone https://github.com/iluxav/fcmd.git
cd fcmd
make build            # produces ./fcmd
sudo make install     # copies to /usr/local/bin/fcmd
```

Other useful targets:

```bash
make run              # build + launch the TUI
make daemon           # build + run daemon in foreground (PORT=N to override)
make cross VERSION=v0.1.0   # cross-compile release artifacts into ./dist
make test vet tidy clean
```

## Windows

Download the `fcmd_<version>_windows_amd64.exe` asset from the
Releases page, rename it to `fcmd.exe`, and place it somewhere on your
`PATH`. The installer script does not manage Windows services — start
the daemon manually (see below) or use Task Scheduler.

## Run

### Launch the TUI

```bash
fcmd
```

- Left pane opens in your home directory.
- Right pane scans the LAN for `fcmd` daemons; press **Enter** on a
  host to connect and browse its files.

### Run the daemon

Each machine you want to reach as a remote needs the daemon running.

```bash
fcmd run                # foreground, default port 7891
fcmd run -port 8080     # custom port
```

With the installer script, systemd/launchd already start it on boot.
Manage the Linux service with:

```bash
sudo systemctl status  fcmd
sudo systemctl restart fcmd
sudo systemctl stop    fcmd
sudo journalctl -u fcmd -f
```

On macOS:

```bash
launchctl list | grep fcmd
launchctl unload ~/Library/LaunchAgents/dev.fcmd.plist
launchctl load   ~/Library/LaunchAgents/dev.fcmd.plist
```

## Keyboard reference

| Key             | Action                                         |
|-----------------|------------------------------------------------|
| `←` / `→`       | Switch focus between panes                     |
| `Enter`         | Enter directory / connect to selected host     |
| `Backspace`     | Go up one directory                            |
| `Esc`           | In a remote pane: back to the LAN host list    |
| `Space`         | Toggle selection on the current item           |
| `C`             | Copy selection to the other pane's current dir |
| `M`             | Move selection to the other pane's current dir |
| `Ctrl+Shift+C`  | Copy (only on terminals that forward Shift)    |
| `Ctrl+Shift+M`  | Move (only on terminals that forward Shift)    |
| `F2`            | Rename the current item                        |
| `F7`            | Create a new directory                         |
| `F8` / `Delete` | Delete the selection (confirmation prompt)     |
| `r`             | Rescan the LAN (right pane, host list)         |
| `q` / `Ctrl+C`  | Quit                                           |

Most terminals cannot distinguish `Ctrl+Shift+C` from plain `Ctrl+C`,
so the uppercase `C` / `M` shortcuts are provided as universal
fallbacks.

## How it works

- **Discovery**: the daemon registers `_fcmd._tcp` on mDNS with a
  `version` TXT record. The TUI browses that service type when the
  right pane is on the host list.
- **Transport**: TCP on port 7891 (override with `-port`). Each RPC
  frame is a length-prefixed JSON header; file bodies follow as
  length-prefixed binary chunks terminated by a zero-length chunk.
- **Streaming**: streaming ops (`read` / `write`) use a dedicated
  connection per transfer, so uploads and downloads against the same
  daemon run concurrently (remote→remote copy works).
- **Atomicity**: writes go to `<path>.fcmd.part` and are renamed into
  place on success.

## Current limits (v1)

- **No authentication or encryption**. Anyone on the LAN who can
  reach the port can use the daemon. Run only on trusted networks.
- **No resume** for interrupted transfers (planned).
- **No delta sync or compression** (planned).

## Uninstall

```bash
sudo systemctl disable --now fcmd          # Linux
launchctl unload ~/Library/LaunchAgents/dev.fcmd.plist   # macOS
sudo rm /usr/local/bin/fcmd
sudo rm /etc/systemd/system/fcmd.service   # Linux
rm ~/Library/LaunchAgents/dev.fcmd.plist   # macOS
```

Or, when building from source: `sudo make uninstall`.
