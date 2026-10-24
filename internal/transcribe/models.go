package transcribe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Model is a whisper.cpp model, spelled as the Rust serde enum is.
type Model string

const (
	ModelTinyEn Model = "tiny-en"
	ModelBaseEn Model = "base-en"
	ModelBase   Model = "base"
	ModelSmall  Model = "small"
)

var AllModels = []Model{ModelTinyEn, ModelBaseEn, ModelBase, ModelSmall}

type modelInfo struct {
	file   string
	size   uint64
	label  string
	multi  bool
	aheads int32
	// dtwLagMs is how far after the middle of a token's sound the model's DTW time for it
	// lands, measured with cmd/wordtimecheck; small is not measured.
	dtwLagMs int64
}

var models = map[Model]modelInfo{
	ModelTinyEn: {"ggml-tiny.en.bin", 77_704_715, "Tiny (English)", false, aheadsTinyEn, 200},
	ModelBaseEn: {"ggml-base.en.bin", 147_964_211, "Base (English)", false, aheadsBaseEn, 0},
	ModelBase:   {"ggml-base.bin", 147_951_465, "Base (multilingual)", true, aheadsBase, 60},
	ModelSmall:  {"ggml-small.bin", 487_601_967, "Small (multilingual)", true, aheadsSmall, 0},
}

func (m Model) FileName() string { return models[m].file }

// SizeBytes is the exact published size, used to verify a download and to recognize a
// complete install.
func (m Model) SizeBytes() uint64    { return models[m].size }
func (m Model) Label() string        { return models[m].label }
func (m Model) IsMultilingual() bool { return models[m].multi }
func (m Model) URL() string {
	return "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/" + m.FileName()
}

func ModelsDir(dataDir string) string { return filepath.Join(dataDir, "models") }

func ModelPath(dataDir string, m Model) string {
	return filepath.Join(ModelsDir(dataDir), m.FileName())
}

func IsInstalled(dataDir string, m Model) bool {
	fi, err := os.Stat(ModelPath(dataDir, m))
	return err == nil && uint64(fi.Size()) == m.SizeBytes()
}

func InstalledModels(dataDir string) []Model {
	var out []Model
	for _, m := range AllModels {
		if IsInstalled(dataDir, m) {
			out = append(out, m)
		}
	}
	return out
}

// DownloadModel makes m usable: it installs the whisper.cpp runtime beside the models when it
// is missing, then downloads m into <dataDir>/models, calling progress(done, total) as bytes
// arrive. It returns errors.ErrUnsupported where no whisper.cpp runtime is published. Bytes land in a `.part` file that is renamed only after its size checks out, so an
// interrupted or cancelled download never looks installed. Cancelling ctx returns ctx.Err().
func DownloadModel(ctx context.Context, dataDir string, m Model, progress func(done, total uint64)) (string, error) {
	if whisperRuntime == nil {
		return "", errors.ErrUnsupported
	}
	if progress == nil {
		progress = func(uint64, uint64) {}
	}
	dest := ModelPath(dataDir, m)
	runtimeLeft := runtimeDownloadSize(ModelsDir(dataDir))
	modelLeft := m.SizeBytes()
	if IsInstalled(dataDir, m) {
		modelLeft = 0
	}
	total := runtimeLeft + modelLeft
	if total == 0 {
		progress(1, 1)
		return dest, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(ModelsDir(dataDir), 0o755); err != nil {
		return "", err
	}
	if runtimeLeft > 0 {
		if err := installRuntime(ctx, ModelsDir(dataDir), func(done uint64) { progress(done, total) }); err != nil {
			return "", err
		}
	}
	if modelLeft > 0 {
		part := dest + ".part"
		err := fetch(ctx, m.URL(), part, modelLeft, stall, func(done uint64) { progress(runtimeLeft+done, total) })
		if err != nil {
			os.Remove(part)
			return "", err
		}
		if err := os.Rename(part, dest); err != nil {
			return "", fmt.Errorf("renaming into %s: %w", dest, err)
		}
	}
	return dest, nil
}

const stall = 30 * time.Second

// fetch downloads url into path, which must come to exactly size bytes. A whole-body timeout
// would cut off a slow but healthy download of a large model, so each read gets the idle
// budget instead.
func fetch(ctx context.Context, url, path string, size uint64, idle time.Duration, progress func(done uint64)) error {
	client := &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: idle}).DialContext,
		TLSHandshakeTimeout:   idle,
		ResponseHeaderTimeout: idle,
	}}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		if e := context.Cause(ctx); e != nil {
			return e
		}
		return fmt.Errorf("requesting %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("requesting %s: %s", url, resp.Status)
	}
	if resp.ContentLength >= 0 && uint64(resp.ContentLength) != size {
		return fmt.Errorf("%s reports %d bytes, expected %d", url, resp.ContentLength, size)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	timer := time.AfterFunc(idle, func() { cancel(fmt.Errorf("downloading %s: no data for %v", url, idle)) })
	defer timer.Stop()
	buf := make([]byte, 256*1024)
	var done uint64
	progress(0)
	for {
		n, rerr := resp.Body.Read(buf)
		timer.Reset(idle)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			done += uint64(n)
			progress(done)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if e := context.Cause(ctx); e != nil {
				return e
			}
			return fmt.Errorf("downloading %s: %w", url, rerr)
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if done != size {
		return fmt.Errorf("downloaded %d bytes of %s, expected %d", done, url, size)
	}
	return nil
}
