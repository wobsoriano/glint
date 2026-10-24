//go:build !windows

package transcribe

import "errors"

func loadWhisper(string) (*whisperLib, error) { return nil, errors.ErrUnsupported }
