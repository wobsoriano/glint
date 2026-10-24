//go:build !windows

package media

import "image"

type CameraPreview struct{}

func StartCameraPreview(id CameraID) (*CameraPreview, error) {
	return nil, unsupported("camera preview")
}

func (p *CameraPreview) Frames() <-chan *image.RGBA { return nil }
func (p *CameraPreview) Size() (width, height int)  { return 0, 0 }
func (p *CameraPreview) Stop() error                { return nil }
