//go:build !(windows && amd64)

package transcribe

// No whisper.cpp release ships a loadable library for this platform yet.
var whisperRuntime *runtimeAsset
