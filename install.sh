#!/bin/sh
# Installs the fakerforge CLI from GitHub Releases.
#
#   curl -sSL https://raw.githubusercontent.com/kwasii1/fakerforge-db-agent/main/install.sh | sh
#   curl -sSL .../install.sh | sh -s -- v0.2.0        # pin a version
#   curl -sSL .../install.sh | INSTALL_DIR=$HOME/bin sh
#
# Needs: curl, tar, and sha256sum (or shasum). Windows users: download the
# windows zip from the Releases page instead.
set -eu

REPO="kwasii1/fakerforge-db-agent"
BIN="fakerforge"

VERSION="${1:-${FAKERFORGE_VERSION:-latest}}"
INSTALL_DIR="${INSTALL_DIR:-}"

fail() { echo "install.sh: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "needs '$1' (not found in PATH)"; }

need curl
need tar
if command -v sha256sum >/dev/null 2>&1; then
  SHA256="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  SHA256="shasum -a 256"
else
  fail "needs 'sha256sum' or 'shasum' (not found in PATH)"
fi

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux|darwin) ;;
  *) fail "unsupported OS '$OS' — grab a build from https://github.com/${REPO}/releases" ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) fail "unsupported architecture '$ARCH'" ;;
esac

if [ "$VERSION" = "latest" ]; then
  TAG="$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')"
  [ -n "$TAG" ] || fail "could not resolve the latest release"
else
  TAG="$VERSION"
fi

ARCHIVE="${BIN}_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/${REPO}/releases/download/${TAG}"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT INT TERM
cd "$TMPDIR"

echo "Downloading ${BIN} ${TAG} for ${OS}/${ARCH}..."
curl -sSL -o "$ARCHIVE" "${BASE}/${ARCHIVE}"
curl -sSL -o checksums.txt "${BASE}/checksums.txt"

echo "Verifying checksum..."
grep "  ${ARCHIVE}\$" checksums.txt | $SHA256 -c - >/dev/null \
  || fail "checksum mismatch for ${ARCHIVE}"

tar -xzf "$ARCHIVE"

if [ -z "$INSTALL_DIR" ]; then
  if [ -w /usr/local/bin ]; then
    INSTALL_DIR=/usr/local/bin
  else
    INSTALL_DIR="$HOME/.local/bin"
  fi
fi
mkdir -p "$INSTALL_DIR"

if [ -w "$INSTALL_DIR" ]; then
  mv "$BIN" "$INSTALL_DIR/$BIN"
else
  echo "Need sudo to write to $INSTALL_DIR"
  sudo mv "$BIN" "$INSTALL_DIR/$BIN"
fi
chmod +x "$INSTALL_DIR/$BIN"

echo "Installed ${BIN} ${TAG} to ${INSTALL_DIR}/${BIN}"
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "NOTE: ${INSTALL_DIR} is not on your PATH — add it to your shell profile." ;;
esac
"$INSTALL_DIR/$BIN" version
