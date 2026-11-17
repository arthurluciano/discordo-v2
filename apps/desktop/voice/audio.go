package voice

import (
	"errors"
	"io"
	"sync"
)

// Device is a microphone or a speaker. An empty ID means the system's default.
type Device struct {
	ID, Name string
}

// Each OS file implements, with no cgo:
//
//	inputs() / outputs() ([]Device, error)
//	openOutput(id string, pull func([]int16)) (io.Closer, error) // pull fills Frame samples, every 20 ms
//	openInput(id string, push func([]int16)) (io.Closer, error)  // push gets Frame samples, every 20 ms
//
// pull and push run on a thread of the OS's audio stack (never its real-time one): they must not block for long.

var errNoAudio = errors.New("áudio indisponível neste sistema")

// Inputs lists the microphones; Outputs the speakers and headphones.
func Inputs() ([]Device, error)  { return inputs() }
func Outputs() ([]Device, error) { return outputs() }

// OpenOutput plays what pull gives, 48 kHz mono, on the device (id "" for the default).
func OpenOutput(id string, pull func([]int16)) (io.Closer, error) { return openOutput(id, pull) }

// OpenInput hands each 20 ms of the microphone (48 kHz mono) to push.
func OpenInput(id string, push func([]int16)) (io.Closer, error) { return openInput(id, push) }

// closeOnce makes a stream's Close idempotent.
type closeOnce struct {
	once sync.Once
	fn   func()
}

func (c *closeOnce) Close() error {
	c.once.Do(c.fn)
	return nil
}
