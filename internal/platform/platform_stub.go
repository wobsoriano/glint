package platform

import "image"

// Displays lists the connected displays, primary first.
func Displays() ([]Display, error) { return nil, ErrUnsupported }

// CaptureRect returns the desktop's composited pixels inside r, opaque.
func CaptureRect(r Rect) (*image.RGBA, error) { return nil, ErrUnsupported }

// CaptureWindow returns one window's pixels, even where other windows cover it if the OS
// allows. With shadow, the image grows by a transparent margin holding a soft drop shadow.
func CaptureWindow(id int64, shadow bool) (*image.RGBA, error) { return nil, ErrUnsupported }

// Windows lists the visible top-level windows of other apps and Glint, front to back,
// leaving out cloaked, minimized and tool windows.
func Windows() ([]Window, error) { return nil, ErrUnsupported }

// CursorPos is the pointer in physical pixels of the virtual desktop.
func CursorPos() (int, int) { return 0, 0 }

// ExcludeFromCapture keeps the native window out of screenshots and recordings, or lets it in.
func ExcludeFromCapture(handle uintptr, exclude bool) error { return ErrUnsupported }

// Wallpaper is the path of the primary display's desktop picture, when it is a file.
func Wallpaper() (string, error) { return "", ErrUnsupported }

// SetLaunchAtLogin starts Glint with args when the user logs in, or stops doing so.
func SetLaunchAtLogin(enabled bool, args []string) error { return ErrUnsupported }
