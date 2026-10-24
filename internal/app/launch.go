package app

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Capture is one kind of screenshot the user can start.
type Capture int

const (
	CaptureArea Capture = iota
	CaptureWindow
	CaptureScreen
	CaptureText
	CapturePrevious
)

var captureNames = map[string]Capture{
	"area": CaptureArea, "window": CaptureWindow, "screen": CaptureScreen, "text": CaptureText, "previous": CapturePrevious,
}

// RequestKind is what a command line, or a second instance forwarding one, asks for.
type RequestKind int

const (
	RequestCapture RequestKind = iota
	RequestLibrary
	RequestRecord
	RequestTrim
	RequestOpen
	RequestStudio
)

// Request is one thing a command line asks for. Capture is set for RequestCapture and Path
// for RequestTrim, RequestOpen and RequestStudio, as an absolute path.
type Request struct {
	Kind    RequestKind
	Capture Capture
	Path    string
}

// Launch is a parsed command line.
type Launch struct {
	Background bool
	Requests   []Request
}

const Usage = "usage: glint.exe [--background] [--library] [--record] [--capture area|window|screen|text|previous] [--studio <session dir>] [--trim <video>] [image, video or Library capture...]"

// VideoExtensions are the files Trim & Compress opens.
var VideoExtensions = []string{"mp4", "mov", "m4v", "wmv", "avi"}

func IsVideo(path string) bool {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	for _, v := range VideoExtensions {
		if strings.EqualFold(v, ext) {
			return true
		}
	}
	return false
}

// ParseArgs reads Glint's command line, the arguments after the program name. Relative paths
// resolve against cwd, the directory the command ran in.
func ParseArgs(args []string, cwd string) (Launch, error) {
	var l Launch
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		return filepath.Join(cwd, p)
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() (string, bool) {
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}
		switch {
		case a == "--background":
			l.Background = true
		case a == "--library":
			l.Requests = append(l.Requests, Request{Kind: RequestLibrary})
		case a == "--record":
			l.Requests = append(l.Requests, Request{Kind: RequestRecord})
		case a == "--capture":
			what, _ := next()
			c, ok := captureNames[what]
			if !ok {
				return Launch{}, fmt.Errorf("unknown capture kind %q\n%s", what, Usage)
			}
			l.Requests = append(l.Requests, Request{Kind: RequestCapture, Capture: c})
		case a == "--trim":
			p, ok := next()
			if !ok {
				return Launch{}, fmt.Errorf("--trim needs a video file\n%s", Usage)
			}
			l.Requests = append(l.Requests, Request{Kind: RequestTrim, Path: abs(p)})
		case a == "--studio":
			p, ok := next()
			if !ok {
				return Launch{}, fmt.Errorf("--studio needs a session directory\n%s", Usage)
			}
			l.Requests = append(l.Requests, Request{Kind: RequestStudio, Path: abs(p)})
		case strings.HasPrefix(a, "--"):
			return Launch{}, fmt.Errorf("unknown option %s\n%s", a, Usage)
		case IsVideo(a):
			l.Requests = append(l.Requests, Request{Kind: RequestTrim, Path: abs(a)})
		default:
			l.Requests = append(l.Requests, Request{Kind: RequestOpen, Path: abs(a)})
		}
	}
	return l, nil
}
