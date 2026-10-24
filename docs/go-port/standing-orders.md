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
   The API reference is https://pkg.go.dev/github.com/egoist/mygo (use the v0.6.3 version), and
   the source is beside the docs. Read them when the guides are silent. Do not guess at an API.
5. The Rust code under `src/` and `crates/` is the spec. Match its behavior and its on-disk
   formats: JSON field names, enum spellings, defaults, file names.
6. Never write to `%APPDATA%\Glint`, which holds the user's real Library. Run the app with
   `GLINT_DATA_DIR` pointing at a temp dir. Reading the screen is always fine. Moving the mouse,
   typing or bringing windows to the front is allowed only when your brief says so, and only
   inside the desktop lock, because other workers share this machine:
   `go run ./tools/desklock -- powershell -NoProfile -File drive.ps1`. Hold it for short
   bursts (a minute or two), and leave the desktop as you found it. PowerShell driver helpers
   (Glide, Click, DClick, Drag, Keys, TypeText, Shot, Win, Place, Front) are in
   `tools/drive/drive.ps1`. Dot-source that file only. Do not use `promo/real/lib.ps1`: it
   launches and kills the Rust Glint. Name your own variables so they cannot be mistaken
   for the app you test, and never start the Rust build in `target/release`, which writes to
   the real Library.
7. Before you report done, run `gofmt -l`, `go vet ./...`, `go build ./...`,
   `GOOS=darwin GOARCH=arm64 go build ./...` and `go test` for your packages. All must be clean.
8. Prove behavior on the real thing: compare pixels, decode the files you wrote, run the OS API.
   Compiling is not proof. Keep the proof script in the repo (a test, or a `cmd/` tool under
   your package) so the coordinator can rerun it.
9. Comments explain a non-obvious why. Do not narrate the code.
10. Report: branch, head SHA, what you built, the exact verification commands and their
    output, deviations from the brief, and open items. Say what you did not verify.
