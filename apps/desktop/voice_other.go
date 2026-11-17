//go:build !darwin

package main

import (
	"runtime"

	"discordo/desktop/voice"

	"gonativeui/gpu"
)

// voiceTexture: there are no pictures here yet (voice.Picture is always 0 off macOS).
func (d *Discordo) voiceTexture(p voice.Picture) (gpu.TextureID, int, int) { return 0, 0, 0 }

// voiceScreenUnsupported says why sharing the screen does not work here.
var voiceScreenUnsupported = map[string]string{
	"linux":   "Compartilhar tela ainda não funciona no Linux.",
	"windows": "Compartilhar tela ainda não funciona no Windows.",
}[runtime.GOOS]
