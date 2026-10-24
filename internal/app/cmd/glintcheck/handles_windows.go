package main

import (
	"flag"
	"fmt"
	"slices"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdll                        = windows.NewLazySystemDLL("ntdll.dll")
	procNtQuerySystemInformation = ntdll.NewProc("NtQuerySystemInformation")
	procNtQueryObject            = ntdll.NewProc("NtQueryObject")
	procEnumChildWindows         = windows.NewLazySystemDLL("user32.dll").NewProc("EnumChildWindows")
	procFindWindowExW            = windows.NewLazySystemDLL("user32.dll").NewProc("FindWindowExW")
	procGetProcessIdOfThread     = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessIdOfThread")
	procGetThreadId              = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetThreadId")
	procNtQueryInformationThread = ntdll.NewProc("NtQueryInformationThread")
)

// handles lists a process's kernel handles as "handle <type> <name>" and its windows, child
// and message-only ones included, as "window <class>", one per line and sorted, so two
// snapshots diff into what a cycle left behind.
func handles(args []string) error {
	fs := flag.NewFlagSet("handles", flag.ExitOnError)
	pid := fs.Int("pid", 0, "process id")
	fs.Parse(args)
	if *pid == 0 {
		return fmt.Errorf("-pid is required")
	}
	lines, err := kernelHandles(uint32(*pid))
	if err != nil {
		return err
	}
	lines = append(lines, processWindows(uint32(*pid))...)
	slices.Sort(lines)
	for _, l := range lines {
		fmt.Println(l)
	}
	return nil
}

// handleEntry is SYSTEM_HANDLE_TABLE_ENTRY_INFO_EX.
type handleEntry struct {
	Object     uintptr
	PID        uintptr
	Handle     uintptr
	Access     uint32
	BackTrace  uint16
	TypeIndex  uint16
	Attributes uint32
	_          uint32
}

func kernelHandles(pid uint32) ([]string, error) {
	const systemExtendedHandleInformation = 64
	buf := make([]byte, 1<<20)
	for {
		var n uint32
		st, _, _ := procNtQuerySystemInformation.Call(systemExtendedHandleInformation, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
		if st == 0 {
			break
		}
		if uint32(st) != 0xC0000004 { // STATUS_INFO_LENGTH_MISMATCH
			return nil, fmt.Errorf("NtQuerySystemInformation: %#x", st)
		}
		buf = make([]byte, max(int(n)+1<<16, 2*len(buf)))
	}
	count := *(*uintptr)(unsafe.Pointer(&buf[0]))
	entries := unsafe.Slice((*handleEntry)(unsafe.Pointer(&buf[16])), count)
	types := objectTypes()
	proc, err := windows.OpenProcess(windows.PROCESS_DUP_HANDLE, false, pid)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(proc)
	var out []string
	for _, e := range entries {
		if uint32(e.PID) != pid {
			continue
		}
		typ := types[e.TypeIndex]
		if typ == "" {
			typ = fmt.Sprintf("type%d", e.TypeIndex)
		}
		out = append(out, fmt.Sprintf("handle %s %s", typ, objectName(proc, pid, e.Handle, typ)))
	}
	return out, nil
}

// objectTypes maps type indices to names from NtQueryObject's ObjectTypesInformation.
func objectTypes() map[uint16]string {
	buf := make([]byte, 1<<16)
	for {
		var n uint32
		st, _, _ := procNtQueryObject.Call(0, 3, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
		if st == 0 {
			break
		}
		if len(buf) > 1<<22 {
			return nil
		}
		buf = make([]byte, 2*len(buf))
	}
	types := map[uint16]string{}
	count := *(*uint32)(unsafe.Pointer(&buf[0]))
	off := uintptr(8)
	for range count {
		info := unsafe.Pointer(&buf[off])
		name := (*windows.NTUnicodeString)(info)
		// OBJECT_TYPE_INFORMATION is 0x68 bytes with TypeIndex at 0x5A; its name follows it.
		types[uint16(*(*uint8)(unsafe.Add(info, 0x5A)))] = name.String()
		off += 0x68 + (uintptr(name.MaximumLength)+7)&^7
	}
	return types
}

// objectName is the name of a handle of the kinds that have a telling one. A name query can
// hang on a pipe, so it runs with a deadline.
func objectName(proc windows.Handle, pid uint32, h uintptr, typ string) string {
	switch typ {
	case "Key", "Section", "File", "Event", "Mutant", "Semaphore", "ALPC Port", "Directory", "Thread":
	default:
		return ""
	}
	var dup windows.Handle
	if typ == "Thread" {
		const access = windows.THREAD_QUERY_INFORMATION | windows.SYNCHRONIZE
		if windows.DuplicateHandle(proc, windows.Handle(h), windows.CurrentProcess(), &dup, access, false, 0) != nil {
			return "?"
		}
		defer windows.CloseHandle(dup)
		return threadState(pid, dup)
	}
	if windows.DuplicateHandle(proc, windows.Handle(h), windows.CurrentProcess(), &dup, 0, false, windows.DUPLICATE_SAME_ACCESS) != nil {
		return "?"
	}
	defer windows.CloseHandle(dup)
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		var n uint32
		st, _, _ := procNtQueryObject.Call(uintptr(dup), 1, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
		if st != 0 {
			done <- "?"
			return
		}
		done <- (*windows.NTUnicodeString)(unsafe.Pointer(&buf[0])).String()
	}()
	select {
	case s := <-done:
		return s
	case <-time.After(200 * time.Millisecond):
		return "(no answer)"
	}
}

// threadState says whether a thread handle is of the process's own thread and whether that
// thread still runs: a handle kept to an exited thread is a leak.
func threadState(pid uint32, thread windows.Handle) string {
	owner, _, _ := procGetProcessIdOfThread.Call(uintptr(thread))
	state := "running"
	if ev, _ := windows.WaitForSingleObject(thread, 0); ev == windows.WAIT_OBJECT_0 {
		state = "exited"
	}
	switch uint32(owner) {
	case pid:
		tid, _, _ := procGetThreadId.Call(uintptr(thread))
		var start uintptr
		procNtQueryInformationThread.Call(uintptr(thread), 9, uintptr(unsafe.Pointer(&start)), unsafe.Sizeof(start), 0)
		return fmt.Sprintf("own, %s, tid %d, start %#x", state, tid, start)
	case 0:
		return "unknown owner, " + state
	}
	return "of another process, " + state
}

func processWindows(pid uint32) []string {
	var out []string
	record := func(h uintptr) bool {
		var p uint32
		windows.GetWindowThreadProcessId(windows.HWND(h), &p)
		if p != pid {
			return false
		}
		buf := make([]uint16, 128)
		procGetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 128)
		out = append(out, "window "+windows.UTF16ToString(buf))
		return true
	}
	// EnumChildWindows walks every descendant, not only the direct children.
	child := windows.NewCallback(func(h, _ uintptr) uintptr { record(h); return 1 })
	add := func(h uintptr) {
		if record(h) {
			procEnumChildWindows.Call(h, child, 0)
		}
	}
	windows.EnumWindows(windows.NewCallback(func(h, _ uintptr) uintptr { add(h); return 1 }), nil)
	const hwndMessage = ^uintptr(2) // HWND_MESSAGE, (HWND)-3
	for h := uintptr(0); ; {
		h, _, _ = procFindWindowExW.Call(hwndMessage, h, 0, 0)
		if h == 0 {
			break
		}
		add(h)
	}
	return out
}
