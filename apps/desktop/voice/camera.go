package voice

// My camera in a call: StartCamera captures it (camera_*.go), keeps its pictures as my preview, Frame(me,
// TrackCamera), and publishes them as the "camera" track (media.Camera on the server) in H.264.

import (
	"errors"
	"time"

	"github.com/pion/webrtc/v4/pkg/media"
)

// The camera's size, rate and bitrate (presets.ts's CAMERA_ENCODING is about this).
const (
	CameraWidth, CameraHeight = 640, 360
	cameraFPS                 = 30
	cameraBitrate             = 600_000
)

// Camera is a camera the system has.
type Camera = Device

var errNoCamera = errors.New("nenhuma câmera encontrada")

// cameraOut is my camera going out: its track, the encoder made for the pictures' size, the capture.
type cameraOut struct {
	track   sampleTrack
	remove  func() error
	enc     *Encoder
	capture *CameraCapture
}

// StartCamera turns my camera on (id "": the system's default) and publishes it. It negotiates: not on the UI
// thread.
func (s *Session) StartCamera(id string) error {
	s.mu.Lock()
	on := s.vid.camera != nil
	s.mu.Unlock()
	if on {
		return nil
	}

	out := &cameraOut{}

	// A viewer that lost pictures (or just started) asks for a keyframe; the SFU passes it on to us.
	pub, err := s.publish(TrackCamera, h264, func() {
		s.mu.Lock()
		enc := out.enc
		s.mu.Unlock()

		if enc != nil {
			enc.ForceKeyframe()
		}
	})
	if err != nil {
		return err
	}

	s.mu.Lock()
	out.track, out.remove = pub.track, pub.remove
	s.vid.camera = out
	s.mu.Unlock()

	c, err := StartCamera(id, func(p Picture) { s.cameraFrame(out, p) }, func(error) { go s.StopCamera() })
	if err != nil {
		_ = s.StopCamera()
		return err
	}

	s.mu.Lock()
	current := s.vid.camera == out
	if current {
		out.capture = c
	}
	s.mu.Unlock()

	if !current { // stopped while it started
		c.Stop()
		return nil
	}

	s.changed()

	return s.negotiate()
}

// cameraFrame shows a picture of my camera as my preview and sends it.
func (s *Session) cameraFrame(out *cameraOut, p Picture) {
	s.mu.Lock()
	live, me, enc := s.vid.camera == out && !s.closed, s.me, out.enc
	s.mu.Unlock()

	if !live {
		return
	}

	s.setFrame(videoKey(me, TrackCamera), p.Retain())

	// A new size needs a new encoder: its first frame is a keyframe.
	if w, h := p.Size(); enc == nil || func() bool { ew, eh := enc.Size(); return ew != w || eh != h }() {
		next, err := NewEncoder(w, h, cameraFPS, cameraBitrate, func(au []byte, key bool) {
			_ = out.track.WriteSample(media.Sample{Data: au, Duration: time.Second / cameraFPS})
		})
		if err != nil {
			s.setNote(videoKey(me, TrackCamera), "Não dá para enviar a câmera: "+err.Error()+".")
			return
		}

		s.mu.Lock()
		old := out.enc
		out.enc = next
		s.mu.Unlock()

		if old != nil {
			old.Close()
		}

		enc = next
	}

	_ = enc.Encode(p)
}

// StopCamera turns my camera off. It negotiates when it was going out: not on the UI thread.
func (s *Session) StopCamera() error {
	s.mu.Lock()
	out, me := s.vid.camera, s.me
	s.vid.camera = nil
	var c *CameraCapture
	var enc *Encoder
	if out != nil {
		c, enc = out.capture, out.enc
	}

	s.vid.frames[videoKey(me, TrackCamera)].Release()
	delete(s.vid.frames, videoKey(me, TrackCamera))
	s.mu.Unlock()
	s.changed()

	if out == nil {
		return nil
	}

	if c != nil {
		c.Stop()
	}

	if enc != nil {
		enc.Close()
	}

	if err := out.remove(); err != nil {
		return err
	}

	return s.negotiate()
}

// CameraOn says whether my camera is on.
func (s *Session) CameraOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.vid.camera != nil
}

// scaleNV12 scales an NV12 picture (planes y and uv with stride sy, sw×sh) to dw×dh (nearest pixel), tight.
func scaleNV12(srcY, srcUV []byte, stride, sw, sh int, y, uv []byte, dw, dh int) {
	for row := range dh {
		sr := row * sh / dh
		for col := range dw {
			y[row*dw+col] = srcY[sr*stride+col*sw/dw]
		}
	}

	for row := range dh / 2 {
		sr := row * (sh / 2) / (dh / 2)
		for col := 0; col < dw; col += 2 {
			sc := (col / 2 * (sw / 2) / (dw / 2)) * 2
			uv[row*dw+col] = srcUV[sr*stride+sc]
			uv[row*dw+col+1] = srcUV[sr*stride+sc+1]
		}
	}
}
