package voice

// Linux: PulseAudio's simple API (libpulse-simple.so.0, also served by PipeWire's pipewire-pulse) through purego.
// Its calls block, so a goroutine per stream paces itself on the server: no callbacks into Go.
// shortcut: only the default devices; listing them needs the asynchronous API (pa_context), add it when asked.

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
)

type paSampleSpec struct {
	Format   uint32 // PA_SAMPLE_S16LE
	Rate     uint32
	Channels uint8
}

type paBufferAttr struct {
	MaxLength, TLength, PreBuf, MinReq, FragSize uint32
}

var pa struct {
	once  sync.Once
	err   error
	new   func(server uintptr, name string, dir int32, dev uintptr, stream string, ss *paSampleSpec, cmap uintptr, attr *paBufferAttr, err *int32) uintptr
	read  func(s uintptr, data unsafe.Pointer, n uintptr, err *int32) int32
	write func(s uintptr, data unsafe.Pointer, n uintptr, err *int32) int32
	free  func(s uintptr)
}

func loadPulse() error {
	pa.once.Do(func() {
		h, err := purego.Dlopen("libpulse-simple.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			pa.err = errors.Join(errNoAudio, err)
			return
		}

		purego.RegisterLibFunc(&pa.new, h, "pa_simple_new")
		purego.RegisterLibFunc(&pa.read, h, "pa_simple_read")
		purego.RegisterLibFunc(&pa.write, h, "pa_simple_write")
		purego.RegisterLibFunc(&pa.free, h, "pa_simple_free")
	})

	return pa.err
}

func inputs() ([]Device, error)  { return nil, loadPulse() }
func outputs() ([]Device, error) { return nil, loadPulse() }

func openOutput(_ string, pull func([]int16)) (io.Closer, error) { return openPulse(1, pull) }

func openInput(_ string, push func([]int16)) (io.Closer, error) { return openPulse(2, push) }

// openPulse opens a playback (dir 1) or record (dir 2) stream and runs fn on each 20 ms.
func openPulse(dir int32, fn func([]int16)) (io.Closer, error) {
	if err := loadPulse(); err != nil {
		return nil, err
	}

	const none = ^uint32(0) // the server's default
	attr := paBufferAttr{MaxLength: none, TLength: 3 * Frame * 2, PreBuf: none, MinReq: none, FragSize: Frame * 2}
	spec := paSampleSpec{Format: 3, Rate: Rate, Channels: 1}
	stream := "Chamada"
	if dir == 2 {
		stream = "Microfone"
	}

	var code int32
	s := pa.new(0, "Discordo", dir, 0, stream, &spec, 0, &attr, &code)
	if s == 0 {
		return nil, errors.Join(errNoAudio, errors.New("pa_simple_new falhou"))
	}

	var stop atomic.Bool
	go func() {
		defer pa.free(s) // here, so no read or write is in flight

		frame := make([]int16, Frame)
		for !stop.Load() {
			if dir == 1 {
				fn(frame)
				if pa.write(s, unsafe.Pointer(&frame[0]), Frame*2, &code) < 0 {
					return
				}
			} else {
				if pa.read(s, unsafe.Pointer(&frame[0]), Frame*2, &code) < 0 {
					return
				}

				fn(frame)
			}
		}
	}()

	return &closeOnce{fn: func() { stop.Store(true) }}, nil
}
