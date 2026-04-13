#!/usr/bin/env bash
# Local presubmit — mirrors .github/workflows/ci.yml.
# Runs automatically as a pre-push hook; skip with: git push --no-verify
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

echo "▸ build"
go build ./cmd/bay

echo "▸ gofmt"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
  echo "These files need gofmt:"
  echo "$unformatted"
  exit 1
fi

echo "▸ go vet"
go vet ./...

echo "▸ staticcheck"
if command -v staticcheck &>/dev/null; then
  staticcheck ./...
else
  echo "  (skipped — install with: go install honnef.co/go/tools/cmd/staticcheck@2026.1)"
fi

echo "▸ tests"
go test ./... -race

echo "✓ presubmit passed"
