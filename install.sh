#!/usr/bin/env bash
# fcmd installer — downloads latest release and installs + starts the daemon.
# Usage: curl -fsSL https://raw.githubusercontent.com/<owner>/fcmd/main/install.sh | bash
# Override the repo with FCMD_REPO=<owner>/<repo>.
set -euo pipefail

REPO="${FCMD_REPO:-}"
PREFIX="${FCMD_PREFIX:-/usr/local/bin}"
VERSION="${FCMD_VERSION:-latest}"

if [[ -z "$REPO" ]]; then
  echo "FCMD_REPO is required (e.g. FCMD_REPO=iluxav/fcmd)" >&2
  exit 1
fi

uname_s=$(uname -s | tr '[:upper:]' '[:lower:]')
uname_m=$(uname -m)
case "$uname_s" in
  linux)  os="linux" ;;
  darwin) os="darwin" ;;
  msys*|mingw*|cygwin*) os="windows" ;;
  *) echo "unsupported OS: $uname_s" >&2; exit 1 ;;
esac
case "$uname_m" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "unsupported arch: $uname_m" >&2; exit 1 ;;
esac

ext=""
[[ "$os" == "windows" ]] && ext=".exe"

if [[ "$VERSION" == "latest" ]]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -oE '"tag_name"\s*:\s*"[^"]+"' | head -n1 | cut -d'"' -f4)
fi
if [[ -z "$VERSION" ]]; then
  echo "could not resolve latest release tag" >&2
  exit 1
fi

asset="fcmd_${VERSION}_${os}_${arch}${ext}"
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $url"
curl -fsSL -o "$tmp/fcmd${ext}" "$url"
chmod +x "$tmp/fcmd${ext}"

sudo_cmd=""
if [[ $EUID -ne 0 && -n "${SUDO:-}" ]]; then
  sudo_cmd="$SUDO"
elif [[ $EUID -ne 0 ]] && command -v sudo >/dev/null 2>&1; then
  sudo_cmd="sudo"
fi

echo "Installing to $PREFIX/fcmd${ext}"
$sudo_cmd install -m 0755 "$tmp/fcmd${ext}" "$PREFIX/fcmd${ext}"

if [[ "$os" == "linux" ]] && command -v systemctl >/dev/null 2>&1; then
  unit="/etc/systemd/system/fcmd.service"
  echo "Installing systemd unit to $unit"
  $sudo_cmd tee "$unit" >/dev/null <<EOF
[Unit]
Description=fcmd LAN file commander daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${PREFIX}/fcmd run
Restart=on-failure
RestartSec=2
User=${SUDO_USER:-$USER}

[Install]
WantedBy=multi-user.target
EOF
  $sudo_cmd systemctl daemon-reload
  if $sudo_cmd systemctl is-active --quiet fcmd; then
    echo "Restarting existing fcmd service"
    $sudo_cmd systemctl restart fcmd
  else
    echo "Enabling and starting fcmd service"
    $sudo_cmd systemctl enable --now fcmd
  fi
  $sudo_cmd systemctl status fcmd --no-pager || true
elif [[ "$os" == "darwin" ]]; then
  plist="$HOME/Library/LaunchAgents/dev.fcmd.plist"
  mkdir -p "$(dirname "$plist")"
  cat > "$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple Computer//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>dev.fcmd</string>
  <key>ProgramArguments</key>
  <array>
    <string>${PREFIX}/fcmd</string>
    <string>run</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
EOF
  launchctl unload "$plist" 2>/dev/null || true
  launchctl load "$plist"
  echo "launchd agent loaded: $plist"
else
  echo "No service manager integration for this platform. Run 'fcmd run' manually."
fi

echo "fcmd installed. Run 'fcmd' to launch the TUI."
