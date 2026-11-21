//go:build !darwin && !windows

package voice

import "errors"

var errNoCameraHere = errors.New("câmera ainda não funciona neste sistema")

type CameraCapture struct{}

func Cameras() ([]Camera, error) { return nil, errNoCameraHere }

func StartCamera(string, func(Picture), func(error)) (*CameraCapture, error) {
	return nil, errNoCameraHere
}

func (*CameraCapture) Stop() {}
