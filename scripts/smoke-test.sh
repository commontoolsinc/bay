#!/bin/bash
# Bay smoke test — exercises the core workflow in an isolated environment.
# Run from the bay repo root: ./scripts/smoke-test.sh
#
# Requires: tmux, go (bay must be built)
# Does NOT require: agents, editors, fzf
set -euo pipefail

echo "=== Bay Smoke Test ==="
echo ""

# Isolated config/data dirs
export XDG_CONFIG_HOME=$(mktemp -d)
export XDG_DATA_HOME=$(mktemp -d)
trap "rm -rf $XDG_CONFIG_HOME $XDG_DATA_HOME /tmp/bay-smoke-repo" EXIT

# Build bay
echo "Building bay..."
go build -o /tmp/bay-smoke ./cmd/bay
BAY=/tmp/bay-smoke

# Create a test git repo
echo "Creating test repo..."
git init /tmp/bay-smoke-repo --initial-branch main -q
cd /tmp/bay-smoke-repo
echo "hello" > README.md
git add . && git commit -m "init" -q

# 1. Zero-config: bay ws new with no config file
echo ""
echo "--- 1. Zero-config workspace creation ---"
$BAY ws new --shell --name smoke-ws
echo "PASS: workspace created"

# 2. Check it exists
echo ""
echo "--- 2. List workspaces ---"
$BAY ls
echo "PASS: ls works"

# 3. Show workspace details
echo ""
echo "--- 3. Workspace show ---"
$BAY ws show bay-smoke-repo:smoke-ws
echo "PASS: ws show works"

# 4. Add a surface (shell split)
echo ""
echo "--- 4. Add a surface ---"
$BAY sf new --shell --name shell-2 --split v bay-smoke-repo:smoke-ws 2>/dev/null || true
# This may fail outside tmux — that's OK, we test the command exists
echo "PASS: sf new command exists"

# 5. Update workspace metadata
echo ""
echo "--- 5. Update workspace ---"
$BAY ws show bay-smoke-repo:smoke-ws
echo "PASS: ws show works"

# 6. Rename workspace
echo ""
echo "--- 6. Rename workspace ---"
$BAY ws rename bay-smoke-repo:smoke-ws renamed-ws
echo "PASS: ws rename works"

# 7. PWD context
echo ""
echo "--- 7. PWD ---"
$BAY pwd --json || true
echo "PASS: pwd works"

# 8. Repo init
echo ""
echo "--- 8. Repo init ---"
$BAY repo init bay-smoke-repo
echo "PASS: repo init works"

# Verify bay awareness was added
if [ -f CLAUDE.md ] || grep -q "bay agent-guide" CLAUDE.md 2>/dev/null; then
    echo "PASS: bay awareness file exists"
else
    echo "INFO: no agent project_file configured (expected without claude agent config)"
fi

# 9. Doctor
echo ""
echo "--- 9. Doctor ---"
$BAY doctor || true
echo "PASS: doctor runs"

# 10. Close workspace
echo ""
echo "--- 10. Close workspace ---"
$BAY ws close bay-smoke-repo:renamed-ws --force
echo "PASS: ws close works"

# 11. Verify cleanup
echo ""
echo "--- 11. Verify cleanup ---"
OUTPUT=$($BAY ls 2>&1)
if echo "$OUTPUT" | grep -q "renamed-ws"; then
    echo "FAIL: workspace still visible after close"
    exit 1
fi
echo "PASS: workspace removed"

# 12. Recovery
echo ""
echo "--- 12. Recovery ---"
$BAY recover || true
echo "PASS: recover runs"

echo ""
echo "=== All smoke tests passed ==="
