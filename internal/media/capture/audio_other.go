//go:build !windows

package capture

import "errors"

func Microphones() ([]AudioDevice, error) { return nil, errors.ErrUnsupported }

func DefaultOutput() (AudioDevice, error) { return AudioDevice{}, errors.ErrUnsupported }

func StartTrack(src Source, path string, channels int, clock *Clock, tap chan<- []float32) (*Track, error) {
	return nil, errors.ErrUnsupported
}

func StartMicMonitor(id DeviceID) (*MicMonitor, error) { return nil, errors.ErrUnsupported }
