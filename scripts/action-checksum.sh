#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo 'usage: action-checksum.sh CHECKSUMS ARCHIVE' >&2
  exit 2
fi

checksums="$1"
archive_path="$2"
archive="$(basename "$archive_path")"
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
  ' "$checksums"
} 2>/dev/null)" || {
  echo "agentic-go: checksums.txt must contain exactly one entry for $archive" >&2
  exit 2
}

if ! [[ "$expected" =~ ^[0-9A-Fa-f]{64}$ ]]; then
  echo "agentic-go: invalid SHA-256 entry for $archive" >&2
  exit 2
fi

actual="$(shasum -a 256 "$archive_path" | awk '{print $1}')"
if [ "$actual" != "$expected" ]; then
  echo "agentic-go: checksum mismatch for $archive" >&2
  exit 2
fi
