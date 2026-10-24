package app

import (
	"log"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32                        = windows.NewLazySystemDLL("shell32.dll")
	procILCreateFromPathW          = shell32.NewProc("ILCreateFromPathW")
	procILFindLastID               = shell32.NewProc("ILFindLastID")
	procILFree                     = shell32.NewProc("ILFree")
	procSHOpenFolderAndSelectItems = shell32.NewProc("SHOpenFolderAndSelectItems")
)

// reveal opens Explorer on each folder of paths with its paths selected. The shell call
// waits on Explorer and pumps messages, so it runs on a COM thread of its own.
func reveal(paths []string) {
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
			defer windows.CoUninitialize()
		}
		for _, f := range byFolder(paths) {
			if err := selectInFolder(f.dir, f.paths); err != nil {
				log.Printf("app: reveal in %s: %v", f.dir, err)
			}
		}
	}()
}

func selectInFolder(dir string, paths []string) error {
	folder, err := pidl(dir)
	if err != nil {
		return err
	}
	defer procILFree.Call(folder)
	// SHOpenFolderAndSelectItems takes each item relative to the folder: the last ID of
	// its absolute PIDL.
	var children []uintptr
	for _, p := range paths {
		abs, err := pidl(p)
		if err != nil {
			continue
		}
		defer procILFree.Call(abs)
		last, _, _ := procILFindLastID.Call(abs)
		children = append(children, last)
	}
	if len(children) == 0 {
		return nil
	}
	hr, _, _ := procSHOpenFolderAndSelectItems.Call(folder, uintptr(len(children)), uintptr(unsafe.Pointer(&children[0])), 0)
	if int32(hr) < 0 {
		return windows.Errno(hr)
	}
	return nil
}

func pidl(path string) (uintptr, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	id, _, err := procILCreateFromPathW.Call(uintptr(unsafe.Pointer(p)))
	if id == 0 {
		return 0, err
	}
	return id, nil
}
