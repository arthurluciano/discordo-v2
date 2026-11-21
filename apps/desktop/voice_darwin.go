package main

import (
	"discordo/desktop/voice"

	"gonativeui/gpu"
)

// voiceTexture shows a picture this frame (zero copy: its IOSurface planes are drawn in place) and releases it.
func (d *Discordo) voiceTexture(p voice.Picture) (tex gpu.TextureID, w, h int) {
	if p == 0 {
		return 0, 0, 0
	}
	defer p.Release() // the renderer keeps it until the GPU is done

	w, h = p.Size()
	tex, err := d.a.R.ImportPixelBuffer(uintptr(p), 0)
	if err != nil {
		return 0, 0, 0
	}

	return tex, w, h
}

// voiceScreenUnsupported says why sharing the screen does not work here ("": it does).
const voiceScreenUnsupported = ""
