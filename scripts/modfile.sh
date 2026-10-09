#!/bin/sh
cd "$(dirname "$0")/.." || exit 1

command -v git >/dev/null 2>&1 || exit 0
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || exit 0
git diff --quiet HEAD -- go.mod go.sum 2>/dev/null && exit 0
go list -deps ./... >/dev/null 2>&1 && exit 0

dir=bin/modfile
mkdir -p "$dir"
git show HEAD:go.mod >"$dir/go.mod" 2>/dev/null || exit 0
git show HEAD:go.sum >"$dir/go.sum" 2>/dev/null || exit 0
go list -modfile="$dir/go.mod" -deps ./... >/dev/null 2>&1 || exit 0

echo "Note: your go.mod or go.sum has local changes that do not build (an old Go can make them)." >&2
echo "Note: this build uses the committed go.mod and go.sum, and your files stay as they are." >&2
echo "Note: to drop the local changes, run: git checkout -- go.mod go.sum" >&2
echo "-modfile=$dir/go.mod"
