package transcribe

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListsOnlyCompleteModels(t *testing.T) {
	data := t.TempDir()
	os.MkdirAll(ModelsDir(data), 0o755)
	if err := os.WriteFile(ModelPath(data, ModelBaseEn), make([]byte, 1234), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(ModelPath(data, ModelTinyEn))
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(int64(ModelTinyEn.SizeBytes()))
	f.Close()
	eq(t, InstalledModels(data), []Model{ModelTinyEn})
}

func TestModelFilesMatchRust(t *testing.T) {
	eq(t, ModelPath("d", ModelBaseEn), filepath.Join("d", "models", "ggml-base.en.bin"))
	eq(t, ModelSmall.URL(), "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin")
	eq(t, ModelBase.IsMultilingual(), true)
}

func TestCancelledDownloadLeavesNothingBehind(t *testing.T) {
	if whisperRuntime == nil {
		t.Skip("no whisper.cpp runtime for this platform")
	}
	data := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DownloadModel(ctx, data, ModelTinyEn, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	entries, _ := os.ReadDir(ModelsDir(data))
	if len(entries) != 0 {
		t.Fatalf("left %v behind", entries)
	}
}

func stallingServer(t *testing.T, reply string) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Read(make([]byte, 4096))
		c.Write([]byte(reply))
		time.Sleep(30 * time.Second)
	}()
	return "http://" + ln.Addr().String() + "/model.bin"
}

func fetchFrom(t *testing.T, url string) (time.Duration, error) {
	part := filepath.Join(t.TempDir(), "model.bin.part")
	started := time.Now()
	err := fetch(context.Background(), url, part, 100, 500*time.Millisecond, func(uint64) {})
	return time.Since(started), err
}

func TestServerThatNeverAnswersTimesOut(t *testing.T) {
	elapsed, err := fetchFrom(t, stallingServer(t, ""))
	if elapsed > time.Second {
		t.Fatalf("gave up after %v", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "timeout awaiting response headers") {
		t.Fatalf("got %v", err)
	}
}

func TestBodyThatStopsArrivingTimesOut(t *testing.T) {
	elapsed, err := fetchFrom(t, stallingServer(t, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nfirst ten."))
	if elapsed > time.Second {
		t.Fatalf("gave up after %v", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "no data for 500ms") {
		t.Fatalf("got %v", err)
	}
}
