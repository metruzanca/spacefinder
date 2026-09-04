#!/bin/sh
# spacefinder installer for macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/metruzanca/spacefinder/main/.github/install.sh | sh
set -eu

REPO="metruzanca/spacefinder"
BIN_NAME="spacefinder"
BASE="https://github.com/${REPO}/releases"

: "${BINDIR:="${PREFIX:-$HOME/.local}/bin"}"

log() { printf '%s\n' "$*"; }
die() { log "error: $*" >&2; exit 1; }

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/spacefinder.XXXXXX")"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

log "Determining latest release..."
version="$(
  curl -fsSIL "${BASE}/latest" \
    | tr -d '\r' \
    | sed -n 's#^[Ll]ocation: .*/tag/\([^ ]*\).*#\1#p' \
    | head -n 1
)"
[ -n "$version" ] || die "could not determine the latest version (network issue?)"
ver="${version#v}"

os_name="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os_name" in
  linux|darwin) ;;
  *) die "unsupported OS: $os_name" ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64)   arch_name="amd64" ;;
  aarch64|arm64)  arch_name="arm64" ;;
  *) die "unsupported architecture: $arch" ;;
esac

archive="${BIN_NAME}_${ver}_${os_name}_${arch_name}.tar.gz"
url="${BASE}/download/${version}/${archive}"

log "Downloading ${archive}..."
cd "$tmpdir"
curl -fsSL "$url" -o "$archive"
curl -fsSL "${BASE}/download/${version}/${BIN_NAME}_${ver}_checksums.txt" -o checksums.txt

log "Verifying checksum..."
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c --ignore-missing checksums.txt >/dev/null
else
  shasum -a 256 -c --ignore-missing checksums.txt >/dev/null
fi

tar -xzf "$archive" "$BIN_NAME"

mkdir -p "$BINDIR"
install -m 0755 "$BIN_NAME" "$BINDIR/$BIN_NAME"

log "Installed ${BIN_NAME} ${version} to ${BINDIR}/${BIN_NAME}"

if ! printf '%s' "$PATH" | tr ':' '\n' | grep -qFx "$BINDIR"; then
  log "Note: ${BINDIR} is not on your PATH; add it, e.g.:"
  log "  echo 'export PATH=\"${BINDIR}:\$PATH\"' >> ~/.profile"
fi