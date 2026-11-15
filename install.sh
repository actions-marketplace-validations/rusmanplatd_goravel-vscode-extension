#!/bin/sh
set -e

usage() {
  this=$1
  cat <<EOF
$this: download go binaries for rusmanplatd/goravel-vscode-extension (goravel-lsp)

Usage: $this [-b] bindir [-d] [tag]
  -b sets bindir or installation directory, Defaults to ./bin
  -d turns on debug logging
   [tag] is a tag from
   https://github.com/rusmanplatd/goravel-vscode-extension/releases
   If tag is missing, then the latest will be used.

EOF
  exit 2
}

parse_args() {
  BINDIR=${BINDIR:-./bin}
  while getopts "b:dh?x" arg; do
    case "$arg" in
      b) BINDIR="$OPTARG" ;;
      d) log_set_priority 10 ;;
      h | \?) usage "$0" ;;
      x) set -x ;;
    esac
  done
  shift $((OPTIND - 1))
  TAG=$1
}

echo_stderr() {
  echo "$@" >&2
}

log_debug() {
  if [ "${LOG_PRIORITY:-6}" -ge 10 ]; then
    echo_stderr "debug: $*"
  fi
}

log_info() {
  echo_stderr "info: $*"
}

log_err() {
  echo_stderr "error: $*"
}

log_set_priority() {
  LOG_PRIORITY="$1"
}

uname_os() {
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$os" in
    cygwin_nt* | mingw* | msys_nt*) os="windows" ;;
  esac
  echo "$os"
}

uname_arch() {
  arch=$(uname -m)
  case "$arch" in
    x86_64 | amd64) arch="amd64" ;;
    aarch64 | arm64) arch="arm64" ;;
    i386 | i686) arch="386" ;;
  esac
  echo "$arch"
}

 resolve_tag() {
  if [ -n "$TAG" ] && [ "$TAG" != "latest" ]; then
    echo "$TAG"
    return
  fi
  latest_url="https://api.github.com/repos/${OWNER}/${REPO}/releases/latest"
  if command -v curl >/dev/null 2>&1; then
    tag=$(curl -sSfL "$latest_url" 2>/dev/null | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1 || true)
  elif command -v wget >/dev/null 2>&1; then
    tag=$(wget -qO- "$latest_url" 2>/dev/null | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1 || true)
  fi
  echo "$tag"
}

http_download() {
  local_file=$1
  source_url=$2
  log_debug "http_download $source_url -> $local_file"
  if command -v curl >/dev/null 2>&1; then
    curl -sSfL -o "$local_file" "$source_url"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$local_file" "$source_url"
  else
    log_err "neither curl nor wget found"
    return 1
  fi
}

verify_sha256() {
  target_file=$1
  checksums_file=$2
  basename_file=$(basename "$target_file")
  expected=$(grep -E "[[:space:]]${basename_file}\$" "$checksums_file" | awk '{print $1}' | head -n 1)
  if [ -z "$expected" ]; then
    log_debug "no checksum entry found for $basename_file; skipping verification"
    return 0
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$target_file" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$target_file" | awk '{print $1}')
  else
    log_debug "no sha256 tool found; skipping verification"
    return 0
  fi
  if [ "$actual" != "$expected" ]; then
    log_err "checksum mismatch for $basename_file: expected $expected, got $actual"
    return 1
  fi
  log_info "verified sha256 checksum for $basename_file"
}

build_from_source() {
  ref=${1:-main}
  log_info "building ${BINARY} from source (${OWNER}/${REPO}@${ref})..."
  if ! command -v go >/dev/null 2>&1; then
    log_err "go toolchain is required to build from source when prebuilt release asset is unavailable"
    exit 1
  fi
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT
  if command -v git >/dev/null 2>&1; then
    if ! git clone --depth 1 --branch "$ref" "https://github.com/${OWNER}/${REPO}.git" "$tmpdir/repo" 2>/dev/null; then
      git clone --depth 1 "https://github.com/${OWNER}/${REPO}.git" "$tmpdir/repo"
    fi
  else
    tar_url="https://github.com/${OWNER}/${REPO}/archive/refs/heads/main.tar.gz"
    http_download "$tmpdir/src.tar.gz" "$tar_url"
    mkdir -p "$tmpdir/repo"
    tar -xzf "$tmpdir/src.tar.gz" -C "$tmpdir/repo" --strip-components=1
  fi
  mkdir -p "$BINDIR"
  ext=""
  if [ "$OS" = "windows" ]; then
    ext=".exe"
  fi
  (
    cd "$tmpdir/repo/server"
    CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${ref#v}" -o "$tmpdir/${BINARY}${ext}" .
  )
  install -m 0755 "$tmpdir/${BINARY}${ext}" "${BINDIR}/${BINARY}${ext}"
  log_info "installed ${BINDIR}/${BINARY}${ext}"
}

OWNER="rusmanplatd"
REPO="goravel-vscode-extension"
BINARY="goravel-lsp"

parse_args "$@"
OS=$(uname_os)
ARCH=$(uname_arch)
TAG=$(resolve_tag)

mkdir -p "$BINDIR"
# Convert BINDIR to absolute path
BINDIR=$(cd "$BINDIR" && pwd)

ext=""
archive_ext="tar.gz"
if [ "$OS" = "windows" ]; then
  ext=".exe"
  archive_ext="zip"
fi

if [ -n "$TAG" ] && [ "$TAG" != "main" ] && [ "$TAG" != "source" ]; then
  VERSION=${TAG#v}
  ARCHIVE_NAME="${BINARY}-${VERSION}-${OS}-${ARCH}.${archive_ext}"
  DOWNLOAD_URL="https://github.com/${OWNER}/${REPO}/releases/download/${TAG}/${ARCHIVE_NAME}"
  CHECKSUM_URL="https://github.com/${OWNER}/${REPO}/releases/download/${TAG}/${BINARY}-${VERSION}-checksums.txt"

  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT

  log_info "downloading ${DOWNLOAD_URL}..."
  if http_download "${tmpdir}/${ARCHIVE_NAME}" "$DOWNLOAD_URL" 2>/dev/null; then
    if http_download "${tmpdir}/checksums.txt" "$CHECKSUM_URL" 2>/dev/null; then
      verify_sha256 "${tmpdir}/${ARCHIVE_NAME}" "${tmpdir}/checksums.txt"
    fi
    if [ "$archive_ext" = "zip" ]; then
      unzip -q "${tmpdir}/${ARCHIVE_NAME}" -d "$tmpdir"
    else
      tar -xzf "${tmpdir}/${ARCHIVE_NAME}" -C "$tmpdir"
    fi
    install -m 0755 "${tmpdir}/${BINARY}${ext}" "${BINDIR}/${BINARY}${ext}"
    log_info "installed ${BINDIR}/${BINARY}${ext} (${TAG})"
    exit 0
  else
    log_info "prebuilt binary for ${TAG} (${OS}/${ARCH}) not found in release; falling back to source build"
  fi
fi

build_from_source "${TAG:-main}"

