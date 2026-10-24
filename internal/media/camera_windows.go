package media

import (
	"errors"

	"github.com/wobsoriano/glint/internal/media/capture"
)

type cameraTrack struct{ path string }

func startCamera(id CameraID, path string, clock *capture.Clock) (*cameraTrack, error) {
	return nil, errors.New("camera recording is not ported yet")
}

func (c *cameraTrack) finish() error { return nil }

func Cameras() ([]CameraDevice, error) { return nil, nil }
