#!/bin/sh
set -e

REPO="angalato08/mcp-language-server"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin) OS="darwin" ;;
  linux)  OS="linux" ;;
  mingw*|msys*|cygwin*) OS="windows" ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64)  ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest version
VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
if [ -z "$VERSION" ]; then
  echo "Failed to fetch latest version"
  exit 1
fi

BINARY="mcp-language-server-${OS}-${ARCH}"
if [ "$OS" = "windows" ]; then
  BINARY="${BINARY}.exe"
fi

URL="https://github.com/$REPO/releases/download/$VERSION/$BINARY"

echo "Downloading mcp-language-server $VERSION ($OS/$ARCH)..."
TMPFILE=$(mktemp)
curl -fsSL "$URL" -o "$TMPFILE"

mkdir -p "$INSTALL_DIR"

TARGET="$INSTALL_DIR/mcp-language-server"
if [ "$OS" = "windows" ]; then
  TARGET="${TARGET}.exe"
fi

mv "$TMPFILE" "$TARGET"
chmod +x "$TARGET"

echo "Installed mcp-language-server $VERSION to $TARGET"

# Check if INSTALL_DIR is in PATH
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "Add $INSTALL_DIR to your PATH if it's not already there." ;;
esac
