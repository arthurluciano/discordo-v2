package voice

// Windows: the waveform API (winmm), which needs no COM and no callbacks into Go: the driver signals an event
// whenever a buffer is done, and a goroutine refills (or drains) it.
// shortcut: winmm adds some latency over WASAPI and names devices with at most 31 characters; move to WASAPI
// (IAudioClient in event mode) if either bothers.

import (
	"fmt"
	"io"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winmm         = windows.NewLazySystemDLL("winmm.dll")
	outGetNumDevs = winmm.NewProc("waveOutGetNumDevs")
	outGetDevCaps = winmm.NewProc("waveOutGetDevCapsW")
	outOpen       = winmm.NewProc("waveOutOpen")
	outPrepare    = winmm.NewProc("waveOutPrepareHeader")
	outUnprepare  = winmm.NewProc("waveOutUnprepareHeader")
	outWrite      = winmm.NewProc("waveOutWrite")
	outReset      = winmm.NewProc("waveOutReset")
	outClose      = winmm.NewProc("waveOutClose")
	inGetNumDevs  = winmm.NewProc("waveInGetNumDevs")
	inGetDevCaps  = winmm.NewProc("waveInGetDevCapsW")
	inOpen        = winmm.NewProc("waveInOpen")
	inPrepare     = winmm.NewProc("waveInPrepareHeader")
	inUnprepare   = winmm.NewProc("waveInUnprepareHeader")
	inAddBuffer   = winmm.NewProc("waveInAddBuffer")
	inStart       = winmm.NewProc("waveInStart")
	inReset       = winmm.NewProc("waveInReset")
	inClose       = winmm.NewProc("waveInClose")
)

const (
	waveMapper    = ^uintptr(0) // WAVE_MAPPER: the default device
	callbackEvent = 0x50000
	whdrDone      = 1
	waveBuffers   = 4 // 80 ms queued: winmm wants a little more than the other systems
)

type waveFormat struct {
	FormatTag, Channels           uint16
	SamplesPerSec, AvgBytesPerSec uint32
	BlockAlign, BitsPerSample     uint16
	CbSize                        uint16
}

type waveHdr struct {
	Data                        *int16
	BufferLength, BytesRecorded uint32
	User                        uintptr
	Flags, Loops                uint32
	Next, Reserved              uintptr
}

// waveCaps covers WAVEINCAPSW and WAVEOUTCAPSW (which has 4 more bytes at the end).
type waveCaps struct {
	Mid, Pid      uint16
	DriverVersion uint32
	Name          [32]uint16
	Formats       uint32
	Channels, _   uint16
	Support       uint32
}

// wbuf is one buffer the driver holds between calls: Go memory, which the collector never moves; the stream keeps it alive.
type wbuf struct {
	hdr waveHdr
	pcm [Frame]int16
}

func listWave(num, caps *windows.LazyProc) ([]Device, error) {
	if err := num.Find(); err != nil {
		return nil, err
	}

	n, _, _ := num.Call()
	var list []Device
	for i := range n {
		var c waveCaps
		if r, _, _ := caps.Call(i, uintptr(unsafe.Pointer(&c)), unsafe.Sizeof(c)); r != 0 {
			continue
		}

		name := windows.UTF16ToString(c.Name[:])
		list = append(list, Device{ID: name, Name: name})
	}

	return list, nil
}

func inputs() ([]Device, error)  { return listWave(inGetNumDevs, inGetDevCaps) }
func outputs() ([]Device, error) { return listWave(outGetNumDevs, outGetDevCaps) }

// deviceIndex finds a device by name (indexes move as devices come and go); the default when it is gone.
func deviceIndex(id string, list func() ([]Device, error)) uintptr {
	devs, _ := list()
	for i, d := range devs {
		if id != "" && d.ID == id {
			return uintptr(i)
		}
	}

	return waveMapper
}

func mmErr(what string, r uintptr) error { return fmt.Errorf("%s: MMRESULT %d", what, r) }

func openOutput(id string, pull func([]int16)) (io.Closer, error) {
	return openWave(id, pull, nil)
}

func openInput(id string, push func([]int16)) (io.Closer, error) {
	return openWave(id, nil, push)
}

func openWave(id string, pull, push func([]int16)) (io.Closer, error) {
	output := pull != nil
	open, prepare, unprepare, reset, closeDev := outOpen, outPrepare, outUnprepare, outReset, outClose
	dev := deviceIndex(id, outputs)
	if !output {
		open, prepare, unprepare, reset, closeDev = inOpen, inPrepare, inUnprepare, inReset, inClose
		dev = deviceIndex(id, inputs)
	}

	if err := open.Find(); err != nil {
		return nil, err
	}

	event, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, err
	}

	f := waveFormat{FormatTag: 1, Channels: 1, SamplesPerSec: Rate, AvgBytesPerSec: Rate * 2, BlockAlign: 2, BitsPerSample: 16}
	var h uintptr
	if r, _, _ := open.Call(uintptr(unsafe.Pointer(&h)), dev, uintptr(unsafe.Pointer(&f)), uintptr(event), 0, callbackEvent); r != 0 {
		windows.CloseHandle(event)
		return nil, mmErr("waveOpen", r)
	}

	// queue hands a buffer to the driver: to play, or to fill.
	queue := func(b *wbuf) {
		b.hdr.BytesRecorded = 0
		atomic.StoreUint32(&b.hdr.Flags, atomic.LoadUint32(&b.hdr.Flags)&^whdrDone)
		if output {
			outWrite.Call(h, uintptr(unsafe.Pointer(&b.hdr)), unsafe.Sizeof(b.hdr))
		} else {
			inAddBuffer.Call(h, uintptr(unsafe.Pointer(&b.hdr)), unsafe.Sizeof(b.hdr))
		}
	}

	bufs := make([]*wbuf, waveBuffers)
	for i := range bufs {
		b := &wbuf{}
		b.hdr.Data, b.hdr.BufferLength = &b.pcm[0], Frame*2
		prepare.Call(h, uintptr(unsafe.Pointer(&b.hdr)), unsafe.Sizeof(b.hdr))
		bufs[i] = b
		queue(b) // output: silence first
	}

	if !output {
		inStart.Call(h)
	}

	var stop atomic.Bool
	done := make(chan struct{})

	go func() {
		defer close(done)

		var fifo []int16
		frame := make([]int16, Frame)
		for !stop.Load() {
			windows.WaitForSingleObject(event, 100)

			for _, b := range bufs {
				if stop.Load() || atomic.LoadUint32(&b.hdr.Flags)&whdrDone == 0 {
					continue
				}

				if output {
					pull(b.pcm[:])
				} else {
					fifo = append(fifo, b.pcm[:b.hdr.BytesRecorded/2]...)
					for len(fifo) >= Frame {
						copy(frame, fifo)
						push(frame)
						fifo = fifo[:copy(fifo, fifo[Frame:])]
					}
				}

				queue(b)
			}
		}
	}()

	return &closeOnce{fn: func() {
		stop.Store(true)
		windows.SetEvent(event)
		<-done

		reset.Call(h) // hands every buffer back
		for _, b := range bufs {
			unprepare.Call(h, uintptr(unsafe.Pointer(&b.hdr)), unsafe.Sizeof(b.hdr))
		}

		closeDev.Call(h)
		windows.CloseHandle(event)
	}}, nil
}
