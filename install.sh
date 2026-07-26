#!/usr/bin/env sh
# kirobuff installer
#
#   curl -fsSL https://raw.githubusercontent.com/AlleyBo55/KiroBuff/master/install.sh | sh
#
# Downloads the release binary for this platform, verifies its checksum, and
# installs it. Set KIROBUFF_VERSION to pin a release, PREFIX to change the
# destination.
#
# POSIX sh on purpose: this has to run on a stock macOS and on a minimal Linux
# container without bash.
set -eu

REPO="AlleyBo55/KiroBuff"
BINARY="kirobuff"
PREFIX="${PREFIX:-$HOME/.local/bin}"
VERSION="${KIROBUFF_VERSION:-latest}"

die() { printf 'error: %s\n' "$1" >&2; exit 1; }
info() { printf '%s\n' "$1"; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }
need uname
need mkdir
need tar

# Prefer curl, fall back to wget: minimal images often have only one.
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1"; }
  fetch_to() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO- "$1"; }
  fetch_to() { wget -qO "$2" "$1"; }
else
  die "need curl or wget"
fi

os=$(uname -s)
case "$os" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) die "unsupported OS: $os. Build from source: go install github.com/$REPO/cmd/$BINARY@latest" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

if [ "$VERSION" = "latest" ]; then
  # Resolve via the redirect rather than the API, which is rate-limited for
  # unauthenticated callers.
  VERSION=$(fetch "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
  [ -n "$VERSION" ] || die "could not resolve the latest release; set KIROBUFF_VERSION"
fi

num=${VERSION#v}
archive="${BINARY}_${num}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$VERSION"

tmp=$(mktemp -d)
# Clean up even when the download or checksum step fails.
trap 'rm -rf "$tmp"' EXIT INT TERM

info "Downloading $BINARY $VERSION for $os/$arch"
fetch_to "$base/$archive" "$tmp/$archive" || die "download failed: $base/$archive"

# Verify the checksum when a digest tool is available. An unverified binary is
# still better than no install, but say so rather than staying quiet.
if fetch_to "$base/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
  expected=$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')
  actual=""
  if command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
  elif command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
  fi
  if [ -n "$expected" ] && [ -n "$actual" ]; then
    [ "$expected" = "$actual" ] || die "checksum mismatch for $archive"
    info "Checksum verified"
  else
    info "WARNING: could not verify the checksum (no sha256 tool found)"
  fi
else
  info "WARNING: checksums.txt unavailable, skipping verification"
fi

tar -xzf "$tmp/$archive" -C "$tmp" || die "extract failed"
[ -f "$tmp/$BINARY" ] || die "archive did not contain $BINARY"

mkdir -p "$PREFIX"
mv "$tmp/$BINARY" "$PREFIX/$BINARY"
chmod +x "$PREFIX/$BINARY"
info "Installed $PREFIX/$BINARY"

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *)
    info ""
    info "WARNING: $PREFIX is not on your PATH."
    info "kirobuff's hooks re-invoke it by name, so they will not run until it is."
    info "Add this to your shell profile:"
    info ""
    info "  export PATH=\"$PREFIX:\$PATH\""
    ;;
esac

info ""
info "Next: kirobuff install"
