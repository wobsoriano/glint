# Standing orders for rewrite workers

Read `docs/go-port/plan.md` first. These orders hold for every unit of the rewrite.

1. Write only inside the paths your brief's SCOPE names. You may change `go.mod` and `go.sum`, but
   only through `go get` and `go mod tidy`.
2. Commit on your own branch. Do not push, merge, rebase, force-push or touch other branches.
   End each commit message with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
3. No cgo. Windows code uses `golang.org/x/sys/windows`, `syscall.SyscallN` and COM vtables.
   macOS code uses `github.com/ebitengine/purego`. Every package builds for windows/amd64 and
   darwin/arm64.
4. Read the MyGo docs and examples of the pinned version before using an API:
   `$(go env GOMODCACHE)/github.com/egoist/mygo@v0.6.3/docs` and `.../examples`.
   The source is beside them. Read it when the docs are silent. Do not guess at an API.
5. The Rust code under `src/` and `crates/` is the spec. Match its behavior and its on-disk
   formats: JSON field names, enum spellings, defaults, file names.
6. Do not move the mouse or type on the desktop unless your brief allows it. Other workers
   share this machine. Reading the screen is fine. Never write to `%APPDATA%\Glint`, which
   holds the user's real Library. Use `GLINT_DATA_DIR` or a temp dir.
7. Before you report done, run `gofmt -l`, `go vet ./...`, `go build ./...`,
   `GOOS=darwin GOARCH=arm64 go build ./...` and `go test` for your packages. All must be clean.
8. Prove behavior on the real thing: compare pixels, decode the files you wrote, run the OS API.
   Compiling is not proof. Keep the proof script in the repo (a test, or a `cmd/` tool under
   your package) so the coordinator can rerun it.
9. Comments explain a non-obvious why. Do not narrate the code.
10. Report: branch, head SHA, what you built, the exact verification commands and their
    output, deviations from the brief, and open items. Say what you did not verify.
