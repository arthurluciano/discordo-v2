//go:build !darwin

package voice

import "errors"

// shortcut: video (watching and sharing screens) is macOS only; Windows needs Media Foundation's H.264 MFTs and a
// capture source, Linux a decoder library: add them behind these same names.
var errNoVideo = errors.New("vídeo ainda não funciona neste sistema")

// Picture is a frame; there are none here.
type Picture uintptr

func (p Picture) Retain() Picture                                { return p }
func (p Picture) Release()                                       {}
func (p Picture) Size() (w, h int)                               { return 0, 0 }
func (p Picture) Read(func(y, uv []byte, yStride, uvStride int)) {}

type Decoder struct{}

func NewDecoder(func(Picture)) (*Decoder, error) { return nil, errNoVideo }
func (*Decoder) Decode([]byte) error             { return errNoVideo }
func (*Decoder) Close()                          {}

type Encoder struct{}

func NewEncoder(w, h, fps, bitrate int, out func([]byte, bool)) (*Encoder, error) {
	return nil, errNoVideo
}
func (*Encoder) Size() (w, h int)     { return 0, 0 }
func (*Encoder) ForceKeyframe()       {}
func (*Encoder) Encode(Picture) error { return errNoVideo }
func (*Encoder) Flush()               {}
func (*Encoder) Close()               {}

func NewPicture(w, h int, fill func(y, uv []byte, yStride, uvStride int)) (Picture, error) {
	return 0, errNoVideo
}

var errNeedKeyframe = errors.New("h264: waiting for a keyframe")
