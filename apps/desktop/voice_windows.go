package main

import (
	"discordo/desktop/voice"

	"gonativeui/gpu"
)

// voiceTextures are the pictures shown on Windows: each converted to RGBA once and uploaded, kept while drawn.
type voiceTextures struct {
	shown map[voice.Picture]voiceTex // each holds a reference to its picture
	frame uint32
	spare []voiceTex // textures of pictures no longer drawn, reused by one of the same size
}

type voiceTex struct {
	tex  gpu.TextureID
	w, h int
	used uint32
	rgba []byte
}

var winTex = voiceTextures{shown: map[voice.Picture]voiceTex{}}

// voiceTexture shows a picture this frame and releases it. A picture not drawn for a frame gives its texture back.
func (d *Discordo) voiceTexture(p voice.Picture) (gpu.TextureID, int, int) {
	if p == 0 {
		return 0, 0, 0
	}
	defer p.Release()

	t := &winTex
	if n := d.a.UI.Frame(); n != t.frame {
		t.frame = n
		for pic, v := range t.shown {
			if n-v.used > 1 {
				delete(t.shown, pic)
				pic.Release()
				t.spare = append(t.spare, v)
			}
		}

		for len(t.spare) > 4 {
			d.a.R.FreeTexture(t.spare[0].tex)
			t.spare = t.spare[1:]
		}
	}

	if v, ok := t.shown[p]; ok {
		v.used = t.frame
		t.shown[p] = v
		return v.tex, v.w, v.h
	}

	w, h := p.Size()
	if w <= 0 || h <= 0 {
		return 0, 0, 0
	}

	var v voiceTex
	for i, s := range t.spare {
		if s.w == w && s.h == h {
			v = s
			t.spare = append(t.spare[:i], t.spare[i+1:]...)
			break
		}
	}

	if v.tex == 0 {
		v = voiceTex{tex: d.a.R.NewTexture(gpu.TexRGBA8, w, h), w: w, h: h, rgba: make([]byte, w*h*4)}
	}

	p.Read(func(y, uv []byte, ys, uvs int) { voice.NV12ToRGBA(y, uv, ys, uvs, w, h, v.rgba) })
	d.a.R.Upload(v.tex, 0, 0, w, h, v.rgba, w*4)
	v.used = t.frame
	t.shown[p.Retain()] = v

	return v.tex, w, h
}

// voiceScreenUnsupported says why sharing the screen does not work here ("": it does).
const voiceScreenUnsupported = ""
