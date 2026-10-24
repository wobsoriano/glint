//go:build !windows

package media

import "time"

type Recorder struct{}

func StartRecorder(o RecordOptions) (*Recorder, error) { return nil, unsupported("screen recording") }

func (r *Recorder) Pause()                    {}
func (r *Recorder) Resume()                   {}
func (r *Recorder) IsPaused() bool            { return true }
func (r *Recorder) SessionDir() string        { return "" }
func (r *Recorder) Elapsed() time.Duration    { return 0 }
func (r *Recorder) MicLevel() (float32, bool) { return 0, false }
func (r *Recorder) Warnings() []string        { return nil }
func (r *Recorder) IsLive() bool              { return false }
func (r *Recorder) Stop() RecordSummary       { return RecordSummary{} }
func (r *Recorder) Cancel()                   {}

func Cameras() ([]CameraDevice, error) { return nil, unsupported("camera enumeration") }
