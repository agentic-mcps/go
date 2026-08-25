#!/usr/bin/env bash
set -euo pipefail

case "$(uname -s)" in
  Darwin) release_os=darwin ;;
  Linux) release_os=linux ;;
  *) exit 0 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) release_arch=amd64 ;;
  arm64|aarch64) release_arch=arm64 ;;
  *) exit 0 ;;
esac

fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/agentic-go-install-test.XXXXXX")"
trap 'rm -rf "$fixture_root"' EXIT
release_root="$fixture_root/releases/v0.3.0"
payload="$fixture_root/payload"
install_root="$fixture_root/bin"
mkdir -p "$release_root" "$payload/LICENSES"
for binary in agentic-go agentic-go-gopls agentic-go-vet; do
  printf '#!/usr/bin/env sh\nprintf "%s fixture\\n"\n' "$binary" > "$payload/$binary"
  chmod 0755 "$payload/$binary"
done
printf 'license\n' > "$payload/LICENSE"
printf 'notices\n' > "$payload/THIRD_PARTY_NOTICES.md"
printf 'gopls license\n' > "$payload/LICENSES/gopls-BSD.txt"
printf 'dependency notices\n' > "$payload/LICENSES/gopls-dependencies.txt"

archive="agentic-go_0.3.0_${release_os}_${release_arch}.tar.gz"
tar -czf "$release_root/$archive" -C "$payload" .
if command -v shasum >/dev/null 2>&1; then
  digest="$(shasum -a 256 "$release_root/$archive" | awk '{print $1}')"
else
  digest="$(sha256sum "$release_root/$archive" | awk '{print $1}')"
fi
printf '%s  %s\n' "$digest" "$archive" > "$release_root/checksums.txt"

AGENTIC_GO_RELEASE_BASE_URL="file://$release_root" bash "$(dirname "$0")/install.sh" 0.3.0 "$install_root" >/dev/null
for binary in agentic-go agentic-go-gopls agentic-go-vet; do
  [ -x "$install_root/$binary" ] || { echo "missing installed $binary" >&2; exit 1; }
  "$install_root/$binary" | grep -q "$binary fixture"
done

printf '0%.0s' {1..64} > "$release_root/checksums.txt"
printf '  %s\n' "$archive" >> "$release_root/checksums.txt"
if AGENTIC_GO_RELEASE_BASE_URL="file://$release_root" bash "$(dirname "$0")/install.sh" 0.3.0 "$fixture_root/rejected" >/dev/null 2>&1; then
  echo 'installer accepted an invalid checksum' >&2
  exit 1
fi

echo 'installer tests passed'
