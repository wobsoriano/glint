package transcribe

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	loadedMu sync.Mutex
	loaded   = map[string]*whisperLib{}

	// The Windows runtime keeps every callback for the life of the process, so these are made
	// once and shared by all runs.
	callbacksOnce sync.Once
	progressCB    uintptr
	abortCB       uintptr
	logCB         uintptr
)

func makeCallbacks() {
	progressCB = windows.NewCallback(func(_, _, percent, user uintptr) uintptr {
		onProgress(user, int32(percent))
		return 0
	})
	abortCB = windows.NewCallback(func(user uintptr) uintptr {
		if onAbort(user) {
			return 1
		}
		return 0
	})
	logCB = windows.NewCallback(func(_, _, _ uintptr) uintptr { return 0 })
}

// loadWhisper loads whisper.dll from dir once per process, after checking each library
// against its pinned hash.
func loadWhisper(dir string) (*whisperLib, error) {
	loadedMu.Lock()
	defer loadedMu.Unlock()
	if l, ok := loaded[dir]; ok {
		return l, nil
	}
	if err := verifyRuntime(dir); err != nil {
		return nil, err
	}
	// Search the DLL's own folder so whisper.dll finds the ggml libraries beside it.
	h, err := windows.LoadLibraryEx(filepath.Join(dir, "whisper.dll"), 0,
		windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_DEFAULT_DIRS)
	if err != nil {
		return nil, fmt.Errorf("loading whisper.dll: %w", err)
	}
	var missing error
	proc := func(name string) uintptr {
		p, err := windows.GetProcAddress(h, name)
		if err != nil && missing == nil {
			missing = fmt.Errorf("whisper.dll: %s: %w", name, err)
		}
		return p
	}
	var (
		pLogSet        = proc("whisper_log_set")
		pInit          = proc("whisper_init_from_file_with_params_no_state")
		pFree          = proc("whisper_free")
		pMultilingual  = proc("whisper_is_multilingual")
		pEOT           = proc("whisper_token_eot")
		pInitState     = proc("whisper_init_state")
		pFreeState     = proc("whisper_free_state")
		pDefaultParams = proc("whisper_full_default_params")
		pFull          = proc("whisper_full_with_state")
		pNSegments     = proc("whisper_full_n_segments_from_state")
		pNTokens       = proc("whisper_full_n_tokens_from_state")
		pTokenText     = proc("whisper_full_get_token_text_from_state")
		pTokenData     = proc("whisper_full_get_token_data_from_state")
		pLangID        = proc("whisper_full_lang_id_from_state")
		pLangStr       = proc("whisper_lang_str")
	)
	if missing != nil {
		return nil, missing
	}
	callbacksOnce.Do(makeCallbacks)
	syscall.SyscallN(pLogSet, logCB, 0)

	// Structs wider than 8 bytes cross the Windows x64 ABI by reference, in both directions:
	// arguments as a pointer to the caller's copy, results through a hidden first pointer.
	// C keeps using these pointers while whisper.cpp calls back into Go, which can grow and move
	// the goroutine stack, so they are pinned on the heap.
	l := &whisperLib{
		initFromFile: func(path string, p *contextParams) uintptr {
			cpath, err := windows.BytePtrFromString(path)
			if err != nil {
				return 0
			}
			var pin runtime.Pinner
			defer pin.Unpin()
			pin.Pin(cpath)
			pin.Pin(p)
			r, _, _ := syscall.SyscallN(pInit, uintptr(unsafe.Pointer(cpath)), uintptr(unsafe.Pointer(p)))
			return r
		},
		free: func(ctx uintptr) { syscall.SyscallN(pFree, ctx) },
		isMultilingual: func(ctx uintptr) bool {
			r, _, _ := syscall.SyscallN(pMultilingual, ctx)
			return int32(r) != 0
		},
		tokenEOT: func(ctx uintptr) int32 {
			r, _, _ := syscall.SyscallN(pEOT, ctx)
			return int32(r)
		},
		initState: func(ctx uintptr) uintptr {
			r, _, _ := syscall.SyscallN(pInitState, ctx)
			return r
		},
		freeState: func(st uintptr) { syscall.SyscallN(pFreeState, st) },
		defaultParams: func(strategy int32) *fullParams {
			p := new(fullParams)
			syscall.SyscallN(pDefaultParams, uintptr(unsafe.Pointer(p)), uintptr(strategy))
			return p
		},
		full: func(ctx, st uintptr, p *fullParams, samples []float32) int32 {
			if len(samples) == 0 {
				samples = make([]float32, 1)[:0:1]
			}
			var pin runtime.Pinner
			defer pin.Unpin()
			pin.Pin(p)
			pin.Pin(unsafe.SliceData(samples))
			for _, s := range []*byte{p.language, p.initialPrompt} {
				if s != nil {
					pin.Pin(s)
				}
			}
			r, _, _ := syscall.SyscallN(pFull, ctx, st, uintptr(unsafe.Pointer(p)),
				uintptr(unsafe.Pointer(unsafe.SliceData(samples))), uintptr(len(samples)))
			return int32(r)
		},
		nSegments: func(st uintptr) int {
			r, _, _ := syscall.SyscallN(pNSegments, st)
			return int(int32(r))
		},
		nTokens: func(st uintptr, seg int) int {
			r, _, _ := syscall.SyscallN(pNTokens, st, uintptr(seg))
			return int(int32(r))
		},
		tokenText: func(ctx, st uintptr, seg, tok int) []byte {
			r, _, _ := syscall.SyscallN(pTokenText, ctx, st, uintptr(seg), uintptr(tok))
			return cBytes(r)
		},
		tokenData: func(st uintptr, seg, tok int) tokenData {
			var d tokenData
			syscall.SyscallN(pTokenData, uintptr(unsafe.Pointer(&d)), st, uintptr(seg), uintptr(tok))
			return d
		},
		langID: func(st uintptr) int32 {
			r, _, _ := syscall.SyscallN(pLangID, st)
			return int32(r)
		},
		langStr: func(id int32) string {
			r, _, _ := syscall.SyscallN(pLangStr, uintptr(id))
			return string(cBytes(r))
		},
		progressCallback: progressCB,
		abortCallback:    abortCB,
	}
	loaded[dir] = l
	return l, nil
}

// cBytes copies a NUL-terminated C string owned by whisper.cpp.
func cBytes(p uintptr) []byte {
	if p == 0 {
		return nil
	}
	ptr := *(**byte)(unsafe.Pointer(&p))
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(ptr), n)) != 0 {
		n++
	}
	return append([]byte(nil), unsafe.Slice(ptr, n)...)
}
