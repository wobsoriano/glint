#!/usr/bin/env bash
# Plan predicate 1 and 2: formatting, vet, both platform builds, tests, and the packaged builds.
# Run from the repository root: bash tools/verify/static.sh [-short]
set -u
short="${1:-}"
echo "== gofmt -l"
gofmt -l $(git ls-files '*.go')
echo "== go vet ./... (windows)"
go vet ./...; echo "exit $?"
echo "== go build ./... (windows/amd64)"
go build ./...; echo "exit $?"
echo "== GOOS=darwin GOARCH=arm64 go build ./..."
GOOS=darwin GOARCH=arm64 go build ./...; echo "exit $?"
echo "== GOOS=darwin GOARCH=arm64 go vet ./..."
GOOS=darwin GOARCH=arm64 go vet ./...; echo "exit $?"
echo "== go tool mygo build"
go tool mygo build; echo "exit $?"
echo "== go tool mygo build -platform darwin/universal"
go tool mygo build -platform darwin/universal; echo "exit $?"
ls -la build/*/
echo "== go test $short ./..."
go test $short -count=1 ./... 2>&1; echo "exit $?"
