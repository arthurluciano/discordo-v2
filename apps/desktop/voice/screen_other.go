//go:build !darwin && !windows

package voice

import "errors"

// shortcut: screen capture is macOS only (ScreenCaptureKit); Windows needs Windows.Graphics.Capture or DXGI
// duplication behind these same names.
var errNoScreen = errors.New("compartilhar tela ainda não funciona neste sistema")

type ScreenCapture struct{}

func PickScreen(ScreenOptions, func(Picture), func([]int16), func(error)) (*ScreenCapture, error) {
	return nil, errNoScreen
}

func CaptureMainDisplay(ScreenOptions, func(Picture), func([]int16), func(error)) (*ScreenCapture, error) {
	return nil, errNoScreen
}

func (*ScreenCapture) Update(ScreenOptions) {}

func (*ScreenCapture) Stop() {}
