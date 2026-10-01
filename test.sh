#!/usr/bin/env bash
# Holocaust drainer — full test gate: vet, race tests, EIP-155 vector.
set -euo pipefail
cd "$(dirname "$0")"

echo "[*] go vet ./..."
go vet ./...

echo "[*] tests (race)"
go test ./... -race -count=1

echo "[*] EIP-155 vector detail"
go test ./cmd/drain/ -count=1 -v 2>&1 | grep -E "^(--- |=== RUN|PASS|FAIL|ok)"

echo "[OK] all gates passed"
