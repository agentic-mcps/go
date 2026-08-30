#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo 'usage: install.sh VERSION [INSTALL_DIR]' >&2
  exit 2
fi

version="${1#v}"
if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "agentic-go: invalid exact version '$version'" >&2
  exit 2
fi

case "$(uname -s)" in
  Darwin) release_os=darwin ;;
  Linux) release_os=linux ;;
  *) echo "agentic-go: unsupported operating system $(uname -s)" >&2; exit 2 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) release_arch=amd64 ;;
  arm64|aarch64) release_arch=arm64 ;;
  *) echo "agentic-go: unsupported architecture $(uname -m)" >&2; exit 2 ;;
esac

install_dir="${2:-${AGENTIC_GO_INSTALL_DIR:-${HOME}/.local/bin}}"
if [ -z "$install_dir" ] || [ "$install_dir" = "/" ]; then
  echo 'agentic-go: refusing unsafe install directory' >&2
  exit 2
fi

archive="agentic-go_${version}_${release_os}_${release_arch}.tar.gz"
base_url="${AGENTIC_GO_RELEASE_BASE_URL:-https://github.com/agentic-mcps/go/releases/download/v${version}}"
download_root="$(mktemp -d "${TMPDIR:-/tmp}/agentic-go-install.XXXXXX")"
trap 'rm -rf "$download_root"' EXIT

curl --fail --location --silent --show-error "$base_url/$archive" -o "$download_root/$archive"
curl --fail --location --silent --show-error "$base_url/checksums.txt" -o "$download_root/checksums.txt"

expected="$({
  awk -v archive="$archive" '
    {
      name = $2
      sub(/^\*/, "", name)
      if (name == archive) {
        print $1
        matches++
      }
    }
    END { if (matches != 1) exit 1 }
  ' "$download_root/checksums.txt"
} 2>/dev/null)" || {
  echo "agentic-go: checksums.txt must contain exactly one entry for $archive" >&2
  exit 2
}
if ! [[ "$expected" =~ ^[0-9A-Fa-f]{64}$ ]]; then
  echo "agentic-go: invalid SHA-256 entry for $archive" >&2
  exit 2
fi
if command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$download_root/$archive" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$download_root/$archive" | awk '{print $1}')"
else
  echo 'agentic-go: no SHA-256 utility found (need shasum or sha256sum)' >&2
  exit 2
fi
if [ "$actual" != "$expected" ]; then
  echo "agentic-go: checksum mismatch for $archive" >&2
  exit 2
fi

tar -xzf "$download_root/$archive" -C "$download_root"
for binary in agentic-go agentic-go-gopls agentic-go-vet; do
  if [ ! -f "$download_root/$binary" ] || [ ! -x "$download_root/$binary" ]; then
    echo "agentic-go: archive does not contain executable $binary" >&2
    exit 2
  fi
done

mkdir -p "$install_dir"
for binary in agentic-go agentic-go-gopls agentic-go-vet; do
  staged="$install_dir/.${binary}.install.$$"
  install -m 0755 "$download_root/$binary" "$staged"
  mv -f "$staged" "$install_dir/$binary"
done

echo "Installed agentic-go $version and its pinned gopls sidecar in $install_dir"
