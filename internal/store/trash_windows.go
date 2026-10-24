package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Recycle Bin, through IFileOperation as the Rust build did. Windows deletes an item for
// good when it cannot recycle it (too big for the bin, or on a drive without one, as network
// shares are), and with no UI nothing asks first. A progress sink refuses those deletes, so
// Trash fails instead of destroying a capture, as MyGo's Shell.TrashItem does.

var (
	ole32                           = windows.NewLazySystemDLL("ole32.dll")
	shell32                         = windows.NewLazySystemDLL("shell32.dll")
	procCoCreateInstance            = ole32.NewProc("CoCreateInstance")
	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")

	clsidFileOperation            = windows.GUID{Data1: 0x3ad05575, Data2: 0x8857, Data3: 0x4850, Data4: [8]byte{0x92, 0x77, 0x11, 0xb8, 0x5b, 0xdb, 0x8e, 0x09}}
	iidIFileOperation             = windows.GUID{Data1: 0x947aab5f, Data2: 0x0a5c, Data3: 0x4c13, Data4: [8]byte{0xb4, 0xd6, 0x4b, 0xf7, 0x83, 0x6f, 0xc9, 0xf8}}
	iidIFileOperationProgressSink = windows.GUID{Data1: 0x04b0f1a7, Data2: 0x9490, Data3: 0x44bc, Data4: [8]byte{0x96, 0xe1, 0x42, 0x96, 0xa3, 0x12, 0x52, 0xe2}}
	iidIShellItem                 = windows.GUID{Data1: 0x43826d1e, Data2: 0xe718, Data3: 0x42ee, Data4: [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}
	iidIUnknown                   = windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

const (
	clsctxInprocServer = 0x1
	operationFlags     = 0x0004 | 0x0010 | 0x0400 | 0x0200 | // FOF_SILENT, NOCONFIRMATION, NOERRORUI, NOCONFIRMMKDIR
		0x0040 | 0x00080000 | 0x20000000 // FOF_ALLOWUNDO, FOFX_RECYCLEONDELETE, FOFX_ADDUNDORECORD
	tsfDeleteRecycleIfPossible = 0x80
	sOK                        = 0
	eAbort                     = 0x80004004
	eNoInterface               = 0x80004002

	vtRelease                 = 2
	vtSetOperationFlags       = 5
	vtDeleteItem              = 18
	vtPerformOperations       = 21
	vtGetAnyOperationsAborted = 22
)

// trashSink is an IFileOperationProgressSink whose only real method is PreDeleteItem.
type trashSink struct {
	vtbl    *[19]uintptr
	refused bool
}

var (
	sinkVtbl     [19]uintptr
	sinkVtblOnce sync.Once
	sinksMu      sync.Mutex
	sinks        = map[uintptr]*trashSink{}
)

func sinkFor(this uintptr) *trashSink {
	sinksMu.Lock()
	defer sinksMu.Unlock()
	return sinks[this]
}

func initSinkVtbl() {
	cb := syscall.NewCallback
	// ok<n> succeeds with n arguments after this.
	ok0 := cb(func(this uintptr) uintptr { return sOK })
	ok1 := cb(func(this, a uintptr) uintptr { return sOK })
	ok2 := cb(func(this, a, b uintptr) uintptr { return sOK })
	ok3 := cb(func(this, a, b, c uintptr) uintptr { return sOK })
	ok4 := cb(func(this, a, b, c, d uintptr) uintptr { return sOK })
	ok5 := cb(func(this, a, b, c, d, e uintptr) uintptr { return sOK })
	ok6 := cb(func(this, a, b, c, d, e, f uintptr) uintptr { return sOK })
	ok7 := cb(func(this, a, b, c, d, e, f, g uintptr) uintptr { return sOK })
	// The sink outlives the operation, so reference counting has nothing to free.
	ref := cb(func(this uintptr) uintptr { return 1 })
	sinkVtbl = [19]uintptr{
		cb(func(this, riid, out uintptr) uintptr { // QueryInterface
			iid := *(*windows.GUID)(native(riid))
			if iid != iidIUnknown && iid != iidIFileOperationProgressSink {
				*(*uintptr)(native(out)) = 0
				return eNoInterface
			}
			*(*uintptr)(native(out)) = this
			return sOK
		}),
		ref, ref, // AddRef, Release
		ok0, ok1, // StartOperations, FinishOperations
		ok3, ok5, // PreRenameItem, PostRenameItem
		ok4, ok6, // PreMoveItem, PostMoveItem
		ok4, ok6, // PreCopyItem, PostCopyItem
		cb(func(this, flags, item uintptr) uintptr { // PreDeleteItem
			if uint32(flags)&tsfDeleteRecycleIfPossible != 0 {
				return sOK
			}
			if s := sinkFor(this); s != nil {
				s.refused = true
			}
			return eAbort
		}),
		ok4,      // PostDeleteItem
		ok3, ok7, // PreNewItem, PostNewItem
		ok2,           // UpdateProgress
		ok0, ok0, ok0, // ResetTimer, PauseTimer, ResumeTimer
	}
}

func trash(paths []string) error {
	errc := make(chan error, 1)
	// IFileOperation wants a single-threaded apartment. A fresh locked thread, which exits
	// with the goroutine, keeps that apartment off whatever thread the caller is on.
	go func() {
		runtime.LockOSThread()
		errc <- trashOnThisThread(paths)
	}()
	return <-errc
}

func trashOnThisThread(paths []string) error {
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		return fmt.Errorf("initializing COM: %w", err)
	}
	defer windows.CoUninitialize()
	sinkVtblOnce.Do(initSinkVtbl)

	sink := &trashSink{vtbl: &sinkVtbl}
	sinkPtr := uintptr(unsafe.Pointer(sink))
	sinksMu.Lock()
	sinks[sinkPtr] = sink
	sinksMu.Unlock()
	var op uintptr
	defer func() {
		if op != 0 {
			comCall(op, vtRelease)
		}
		sinksMu.Lock()
		delete(sinks, sinkPtr)
		sinksMu.Unlock()
	}()

	if r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOperation)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIFileOperation)), uintptr(unsafe.Pointer(&op))); failed(r) || op == 0 {
		return hresult("creating IFileOperation", r)
	}
	if r := comCall(op, vtSetOperationFlags, operationFlags); failed(r) {
		return hresult("IFileOperation.SetOperationFlags", r)
	}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		name, err := windows.UTF16PtrFromString(abs)
		if err != nil {
			return err
		}
		var item uintptr
		if r, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(name)), 0,
			uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&item))); failed(r) || item == 0 {
			return hresult("locating "+abs, r)
		}
		r := comCall(op, vtDeleteItem, item, sinkPtr)
		comCall(item, vtRelease)
		if failed(r) {
			return hresult("IFileOperation.DeleteItem", r)
		}
	}
	r := comCall(op, vtPerformOperations)
	var aborted int32
	comCall(op, vtGetAnyOperationsAborted, uintptr(unsafe.Pointer(&aborted)))
	switch {
	case sink.refused:
		return errors.New("moving to the Recycle Bin: it would be deleted permanently instead")
	case failed(r):
		return hresult("moving to the Recycle Bin", r)
	case aborted != 0:
		return errors.New("moving to the Recycle Bin was cancelled")
	}
	return nil
}

// native turns an address COM handed over into a pointer without go vet's uintptr warning;
// the memory is COM's, so the garbage collector never moves it.
func native(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }

func comCall(obj uintptr, method int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(native(obj))
	fn := *(*uintptr)(unsafe.Add(native(vtbl), method*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

func hresult(what string, hr uintptr) error {
	return fmt.Errorf("%s: %w", what, syscall.Errno(uint32(hr)))
}
