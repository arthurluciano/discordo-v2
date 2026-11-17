package voice

// macOS: Audio Queue Services (AudioToolbox) for both directions, Core Audio's object properties for the device list,
// all through purego (no cgo). The queues call back on their own thread (not the real-time I/O one), where Go may run.

import (
	"fmt"
	"io"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// AudioStreamBasicDescription.
type asbd struct {
	SampleRate                                  float64
	FormatID, FormatFlags, BytesPerPacket       uint32
	FramesPerPacket, BytesPerFrame, ChannelsPer uint32
	BitsPerChannel, Reserved                    uint32
}

// AudioQueueBuffer's head.
type aqBuffer struct {
	Capacity uint32
	Data     unsafe.Pointer
	Size     uint32
}

type propAddr struct{ Selector, Scope, Element uint32 }

func fourcc(s string) uint32 {
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
}

const (
	cfUTF8          = 0x08000100
	systemObject    = 1
	queueBuffers    = 3     // 60 ms queued each way
	lpcmSignedInt16 = 4 | 8 // kLinearPCMFormatFlagIsSignedInteger | IsPacked
)

var ca struct {
	once sync.Once
	err  error

	newOutput    func(f *asbd, cb, user, runLoop, mode uintptr, flags uint32, q *uintptr) int32
	newInput     func(f *asbd, cb, user, runLoop, mode uintptr, flags uint32, q *uintptr) int32
	allocBuffer  func(q uintptr, size uint32, buf **aqBuffer) int32
	enqueue      func(q uintptr, buf *aqBuffer, n uint32, descs uintptr) int32
	start        func(q, at uintptr) int32
	stop         func(q uintptr, immediate bool) int32
	dispose      func(q uintptr, immediate bool) int32
	setProperty  func(q uintptr, id uint32, data unsafe.Pointer, size uint32) int32
	propSize     func(obj uint32, a *propAddr, qs uint32, qual uintptr, size *uint32) int32
	propData     func(obj uint32, a *propAddr, qs uint32, qual uintptr, size *uint32, out unsafe.Pointer) int32
	cfString     func(alloc uintptr, s string, enc uint32) uintptr
	cfGetCString func(s uintptr, buf *byte, size int, enc uint32) bool
	cfRelease    func(uintptr)

	outputCB, inputCB uintptr // one each for the app's life: purego never frees callbacks

	mu      sync.Mutex
	streams map[uintptr]*aqStream // by the user data the queue calls back with
	nextID  uintptr
}

func load() error {
	ca.once.Do(func() {
		libs := map[string]uintptr{}
		for _, name := range []string{"AudioToolbox", "CoreAudio", "CoreFoundation"} {
			h, err := purego.Dlopen("/System/Library/Frameworks/"+name+".framework/"+name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				ca.err = err
				return
			}

			libs[name] = h
		}

		at, co, cf := libs["AudioToolbox"], libs["CoreAudio"], libs["CoreFoundation"]
		purego.RegisterLibFunc(&ca.newOutput, at, "AudioQueueNewOutput")
		purego.RegisterLibFunc(&ca.newInput, at, "AudioQueueNewInput")
		purego.RegisterLibFunc(&ca.allocBuffer, at, "AudioQueueAllocateBuffer")
		purego.RegisterLibFunc(&ca.enqueue, at, "AudioQueueEnqueueBuffer")
		purego.RegisterLibFunc(&ca.start, at, "AudioQueueStart")
		purego.RegisterLibFunc(&ca.stop, at, "AudioQueueStop")
		purego.RegisterLibFunc(&ca.dispose, at, "AudioQueueDispose")
		purego.RegisterLibFunc(&ca.setProperty, at, "AudioQueueSetProperty")
		purego.RegisterLibFunc(&ca.propSize, co, "AudioObjectGetPropertyDataSize")
		purego.RegisterLibFunc(&ca.propData, co, "AudioObjectGetPropertyData")
		purego.RegisterLibFunc(&ca.cfString, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&ca.cfGetCString, cf, "CFStringGetCString")
		purego.RegisterLibFunc(&ca.cfRelease, cf, "CFRelease")

		ca.outputCB = purego.NewCallback(func(user, q uintptr, buf *aqBuffer) {
			if s := streamOf(user); s != nil {
				s.refill(buf)
			}
		})
		ca.inputCB = purego.NewCallback(func(user, q uintptr, buf *aqBuffer, at uintptr, n uint32, descs uintptr) {
			if s := streamOf(user); s != nil {
				s.drain(buf)
			}
		})
		ca.streams = map[uintptr]*aqStream{}
	})

	return ca.err
}

type aqStream struct {
	q      uintptr
	id     uintptr
	pull   func([]int16)
	push   func([]int16)
	frame  []int16
	fifo   []int16 // input: what arrived beyond whole frames
	mu     sync.Mutex
	closed bool
}

func streamOf(id uintptr) *aqStream {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	return ca.streams[id]
}

func (s *aqStream) refill(buf *aqBuffer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	s.pull(s.frame)
	copy(unsafe.Slice((*int16)(buf.Data), Frame), s.frame)
	buf.Size = Frame * 2
	ca.enqueue(s.q, buf, 0, 0)
}

func (s *aqStream) drain(buf *aqBuffer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	s.fifo = append(s.fifo, unsafe.Slice((*int16)(buf.Data), buf.Size/2)...)
	for len(s.fifo) >= Frame {
		copy(s.frame, s.fifo[:Frame])
		s.push(s.frame)
		s.fifo = s.fifo[:copy(s.fifo, s.fifo[Frame:])]
	}

	ca.enqueue(s.q, buf, 0, 0)
}

func openOutput(id string, pull func([]int16)) (io.Closer, error) { return openQueue(id, pull, nil) }

func openInput(id string, push func([]int16)) (io.Closer, error) { return openQueue(id, nil, push) }

func openQueue(device string, pull, push func([]int16)) (io.Closer, error) {
	if err := load(); err != nil {
		return nil, err
	}

	f := asbd{SampleRate: Rate, FormatID: fourcc("lpcm"), FormatFlags: lpcmSignedInt16, BytesPerPacket: 2,
		FramesPerPacket: 1, BytesPerFrame: 2, ChannelsPer: 1, BitsPerChannel: 16}

	s := &aqStream{pull: pull, push: push, frame: make([]int16, Frame)}

	ca.mu.Lock()
	ca.nextID++
	s.id = ca.nextID
	ca.streams[s.id] = s
	ca.mu.Unlock()

	forget := func() {
		ca.mu.Lock()
		delete(ca.streams, s.id)
		ca.mu.Unlock()
	}

	var st int32
	if pull != nil {
		st = ca.newOutput(&f, ca.outputCB, s.id, 0, 0, 0, &s.q)
	} else {
		st = ca.newInput(&f, ca.inputCB, s.id, 0, 0, 0, &s.q)
	}

	if st != 0 {
		forget()
		return nil, osStatus("AudioQueueNew", st)
	}

	fail := func(what string, st int32) (io.Closer, error) {
		ca.dispose(s.q, true)
		forget()
		return nil, osStatus(what, st)
	}

	if device != "" {
		uid := ca.cfString(0, device, cfUTF8)
		st = ca.setProperty(s.q, fourcc("aqcd"), unsafe.Pointer(&uid), uint32(unsafe.Sizeof(uid)))
		ca.cfRelease(uid)
		if st != 0 {
			return fail("AudioQueueSetProperty(CurrentDevice)", st)
		}
	}

	for range queueBuffers {
		var buf *aqBuffer
		if st = ca.allocBuffer(s.q, Frame*2, &buf); st != 0 {
			return fail("AudioQueueAllocateBuffer", st)
		}

		if pull != nil {
			clear(unsafe.Slice((*int16)(buf.Data), Frame)) // starts on silence
			buf.Size = Frame * 2
		}

		ca.enqueue(s.q, buf, 0, 0)
	}

	if st = ca.start(s.q, 0); st != 0 {
		return fail("AudioQueueStart", st)
	}

	return &closeOnce{fn: func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		ca.stop(s.q, true)
		ca.dispose(s.q, true)
		forget()
	}}, nil
}

func inputs() ([]Device, error)  { return devices(fourcc("inpt")) }
func outputs() ([]Device, error) { return devices(fourcc("outp")) }

// devices lists the devices with streams in scope, by their UID (what a queue's CurrentDevice takes).
func devices(scope uint32) ([]Device, error) {
	if err := load(); err != nil {
		return nil, err
	}

	all := propAddr{fourcc("dev#"), fourcc("glob"), 0}
	var size uint32
	if st := ca.propSize(systemObject, &all, 0, 0, &size); st != 0 {
		return nil, osStatus("AudioObjectGetPropertyDataSize", st)
	}

	ids := make([]uint32, size/4)
	if len(ids) == 0 {
		return nil, nil
	}

	if st := ca.propData(systemObject, &all, 0, 0, &size, unsafe.Pointer(&ids[0])); st != 0 {
		return nil, osStatus("AudioObjectGetPropertyData", st)
	}

	var list []Device
	for _, id := range ids[:size/4] {
		streams := propAddr{fourcc("stm#"), scope, 0}
		if ca.propSize(id, &streams, 0, 0, &size) != 0 || size == 0 {
			continue
		}

		uid, name := cfProp(id, fourcc("uid ")), cfProp(id, fourcc("lnam"))
		if uid != "" {
			list = append(list, Device{ID: uid, Name: name})
		}
	}

	return list, nil
}

// cfProp reads a device's CFString property.
func cfProp(id, selector uint32) string {
	a := propAddr{selector, fourcc("glob"), 0}
	var ref uintptr
	size := uint32(unsafe.Sizeof(ref))
	if ca.propData(id, &a, 0, 0, &size, unsafe.Pointer(&ref)) != 0 || ref == 0 {
		return ""
	}
	defer ca.cfRelease(ref)

	buf := make([]byte, 512)
	if !ca.cfGetCString(ref, &buf[0], len(buf), cfUTF8) {
		return ""
	}

	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}

	return string(buf)
}

func osStatus(what string, st int32) error { return fmt.Errorf("%s: OSStatus %d", what, st) }
