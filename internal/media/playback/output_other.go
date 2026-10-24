//go:build !windows

package playback

import "errors"

func (o *AudioOutput) start(fill func([]float32)) error {
	close(o.done)
	return errors.Join(errors.New("playback: audio output is not implemented on this platform"), errors.ErrUnsupported)
}
