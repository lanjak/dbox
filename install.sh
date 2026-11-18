#!/bin/sh
# Copyright (C) 2026 Jake Lancaster. SPDX-License-Identifier: GPL-3.0-or-later
# dbox installer - works on any Linux distribution, macOS and FreeBSD.
#
#   curl -fsSL https://git.lanjak.net/rufi/dbox/raw/branch/main/install.sh | sh
#
# Options (or environment variables):
#   --version vX.Y.Z   DBOX_VERSION      install this release (default: latest)
#   --dir PATH         DBOX_INSTALL_DIR  install into PATH
#                                        (default: /usr/local/bin as root, else ~/.local/bin)
#   --uninstall                          remove dbox (your settings are kept)
#   --help
#
# The binary is static, so it needs no libraries. The installer checks the
# SHA-256 sum of the download before it installs it.
set -eu

REPO_URL=${DBOX_REPO_URL:-https://git.lanjak.net/rufi/dbox}
API_URL=${DBOX_API_URL:-https://git.lanjak.net/api/v1/repos/rufi/dbox}
MODULE=git.lanjak.net/rufi/dbox/cmd/dbox
VERSION=${DBOX_VERSION:-latest}
INSTALL_DIR=${DBOX_INSTALL_DIR:-}
UNINSTALL=0

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	B=$(printf '\033[1m') G=$(printf '\033[32m') Y=$(printf '\033[33m') R=$(printf '\033[31m') N=$(printf '\033[0m')
else
	B='' G='' Y='' R='' N=''
fi
say() { printf '%s==>%s %s\n' "$B" "$N" "$*"; }
ok() { printf '%s ok%s  %s\n' "$G" "$N" "$*"; }
warn() { printf '%snote%s %s\n' "$Y" "$N" "$*" >&2; }
die() {
	printf '%serror%s %s\n' "$R" "$N" "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF_USAGE'
dbox installer

  curl -fsSL https://git.lanjak.net/rufi/dbox/raw/branch/main/install.sh | sh
  curl -fsSL https://git.lanjak.net/rufi/dbox/raw/branch/main/install.sh | sh -s -- --version v0.2.0

Options (or environment variables):
  --version vX.Y.Z   DBOX_VERSION      install this release (default: latest)
  --dir PATH         DBOX_INSTALL_DIR  install into PATH
                                       (default: /usr/local/bin as root, else ~/.local/bin)
  --uninstall                          remove dbox (your settings are kept)
  --help
EOF_USAGE
}

while [ $# -gt 0 ]; do
	case $1 in
	--version) [ $# -ge 2 ] || die "--version needs a value"; VERSION=$2; shift 2 ;;
	--version=*) VERSION=${1#*=}; shift ;;
	--dir) [ $# -ge 2 ] || die "--dir needs a value"; INSTALL_DIR=$2; shift 2 ;;
	--dir=*) INSTALL_DIR=${1#*=}; shift ;;
	--uninstall) UNINSTALL=1; shift ;;
	-h | --help) usage; exit 0 ;;
	*) die "unknown option: $1 (try --help)" ;;
	esac
done

if [ -z "$INSTALL_DIR" ]; then
	if [ "$(id -u)" = 0 ]; then
		INSTALL_DIR=/usr/local/bin
	else
		[ -n "${HOME:-}" ] || die "HOME is not set; use --dir"
		INSTALL_DIR=$HOME/.local/bin
	fi
fi

if [ "$UNINSTALL" = 1 ]; then
	if [ -e "$INSTALL_DIR/dbox" ]; then
		rm -f "$INSTALL_DIR/dbox" || die "cannot remove $INSTALL_DIR/dbox (try with sudo)"
		ok "removed $INSTALL_DIR/dbox"
	else
		warn "no dbox in $INSTALL_DIR (use --dir to point at it)"
	fi
	say "settings and history are kept in your config folder (~/.config/dbox on Linux); delete it by hand if you want"
	exit 0
fi

# --- platform ---------------------------------------------------------------
case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
FreeBSD) os=freebsd ;;
*) die "unsupported system $(uname -s); Windows users: download dbox-windows-amd64.exe from $REPO_URL/releases" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
armv7* | armv8l | armhf) arch=arm ;;
i386 | i486 | i586 | i686) arch=386 ;;
riscv64) arch=riscv64 ;;
*) die "unsupported CPU $(uname -m)" ;;
esac

# --- download helpers ---------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL --retry 2 -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$2" "$1"; }
else
	die "install curl or wget first (for example: sudo apt-get install curl)"
fi

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	elif command -v sha256 >/dev/null 2>&1; then
		sha256 -q "$1"
	else
		die "no SHA-256 tool found (sha256sum, shasum or sha256)"
	fi
}

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t dbox)
trap 'rm -rf "$tmp"' EXIT
trap 'exit 130' INT TERM

# --- resolve the release ------------------------------------------------------
if [ "$VERSION" = latest ]; then
	say "finding the latest release"
	if fetch "$API_URL/releases/latest" "$tmp/latest.json" 2>/dev/null; then
		VERSION=$(sed -n 's/.*"tag_name":"\([^"]*\)".*/\1/p' "$tmp/latest.json")
	fi
fi

asset="dbox-$os-$arch"
installed=0
if [ "$VERSION" != latest ] && [ -n "$VERSION" ]; then
	say "downloading dbox $VERSION for $os/$arch"
	base="$REPO_URL/releases/download/$VERSION"
	if fetch "$base/$asset" "$tmp/dbox" && fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS"; then
		want=$(sed -n "s/^\([0-9a-fA-F]\{64\}\)  *\**$asset\$/\1/p" "$tmp/SHA256SUMS")
		[ -n "$want" ] || die "$asset is not listed in SHA256SUMS"
		got=$(sha256 "$tmp/dbox")
		[ "$want" = "$got" ] || die "checksum mismatch for $asset (expected $want, got $got)"
		ok "checksum verified"
		installed=1
	else
		warn "no prebuilt $asset in release $VERSION"
	fi
fi

if [ "$installed" = 0 ]; then
	command -v go >/dev/null 2>&1 || die "no prebuilt binary for $os/$arch and Go is not installed; install Go from https://go.dev/dl and run this again"
	ref=$VERSION
	[ -n "$ref" ] || ref=latest
	say "building dbox $ref from source with $(go version | cut -d' ' -f3)"
	GOBIN="$tmp" CGO_ENABLED=0 go install -trimpath -ldflags "-s -w -X main.version=$ref" "$MODULE@$ref" ||
		die "go install failed"
fi

# --- install ------------------------------------------------------------------
mkdir -p "$INSTALL_DIR" 2>/dev/null || die "cannot create $INSTALL_DIR (try with sudo, or use --dir)"
[ -w "$INSTALL_DIR" ] || die "cannot write to $INSTALL_DIR (try with sudo, or use --dir)"
chmod 0755 "$tmp/dbox"
mv -f "$tmp/dbox" "$INSTALL_DIR/dbox" || die "cannot install into $INSTALL_DIR"
ok "installed $("$INSTALL_DIR/dbox" version) to $INSTALL_DIR/dbox"

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	warn "$INSTALL_DIR is not on your PATH. Add this line to your shell profile (~/.bashrc, ~/.zshrc, ~/.profile):"
	# shellcheck disable=SC2016 # $PATH is meant literally here
	printf '       export PATH="%s:$PATH"\n' "$INSTALL_DIR" >&2
	;;
esac

# --- optional: chafa shows game header images in the terminal -------------------
if ! command -v chafa >/dev/null 2>&1; then
	hint=''
	for pm in apt-get dnf yum pacman zypper apk xbps-install emerge eopkg brew pkg nix-env; do
		command -v "$pm" >/dev/null 2>&1 || continue
		case $pm in
		apt-get) hint='sudo apt-get install chafa' ;;
		dnf | yum) hint="sudo $pm install chafa" ;;
		pacman) hint='sudo pacman -S chafa' ;;
		zypper) hint='sudo zypper install chafa' ;;
		apk) hint='sudo apk add chafa' ;;
		xbps-install) hint='sudo xbps-install chafa' ;;
		emerge) hint='sudo emerge media-gfx/chafa' ;;
		eopkg) hint='sudo eopkg install chafa' ;;
		brew) hint='brew install chafa' ;;
		pkg) hint='sudo pkg install chafa' ;;
		nix-env) hint='nix-env -iA nixpkgs.chafa' ;;
		esac
		break
	done
	[ -n "$hint" ] && say "optional: install chafa to see game images in the terminal: $hint"
fi

say "run ${B}dbox${N} to start (the first run asks for your DepotBox API key)"
