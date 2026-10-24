#!/usr/bin/env bash
# The macOS gap list: every non-test file that builds for darwin but not windows and is a stub
# (a *_other.go file), with its build line and functions, and every function body in the
# darwin build that returns ErrUnsupported or errUnsupported. Run from the repository root.
set -u
echo "## stub files compiled on darwin"
find internal main.go -name '*_other.go' -not -path '*/cmd/*' | sort | while read -r f; do
	echo "=== $f  [$(grep -m1 '^//go:build' "$f")]"
	grep -n '^func ' "$f"
done
echo
echo "## lines in darwin-built files that return an unsupported error"
GOOS=darwin GOARCH=arm64 go list -f '{{$d := .Dir}}{{range .GoFiles}}{{$d}}/{{.}}{{"\n"}}{{end}}' ./... |
	tr '\\' '/' | grep -v '/cmd/' | xargs grep -n -i 'return.*unsupported\|unsupported)' 2>/dev/null |
	sed "s#^$(pwd -W 2>/dev/null || pwd)/##"
