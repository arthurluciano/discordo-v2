//go:build windows

package voice

// Windows video: Media Foundation's H.264 decoder and encoder transforms (the system's software ones, present on
// every Windows 10 but the N editions without the Media Feature Pack), through syscall: no cgo. Pictures are NV12
// in Go memory; the window converts and uploads them (voice_windows.go).

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32  = windows.NewLazySystemDLL("ole32.dll")
	mfplat = windows.NewLazySystemDLL("mfplat.dll")

	coInitializeEx   = ole32.NewProc("CoInitializeEx")
	coCreateInstance = ole32.NewProc("CoCreateInstance")
	mfStartup        = mfplat.NewProc("MFStartup")
	mfCreateType     = mfplat.NewProc("MFCreateMediaType")
	mfCreateSample   = mfplat.NewProc("MFCreateSample")
	mfCreateBuffer   = mfplat.NewProc("MFCreateMemoryBuffer")
)

func mustGUID(s string) windows.GUID {
	g, err := windows.GUIDFromString("{" + s + "}")
	if err != nil {
		panic(err)
	}

	return g
}

// GUIDs (mfapi.h, mftransform.h, codecapi.h, wmcodecdsp.h).
var (
	clsidH264Decoder = mustGUID("62ce7e72-4c71-4d20-b15d-452831a87d9d")
	clsidH264Encoder = mustGUID("6ca50344-051a-4ded-9779-a43305165e35")
	iidTransform     = mustGUID("bf94c121-5b05-4e6f-8000-ba598961414d")
	iidCodecAPI      = mustGUID("901db4c7-31ce-41a2-85dc-8fa0bf41b8da")

	mfMediaTypeVideo  = mustGUID("73646976-0000-0010-8000-00aa00389b71")
	mfVideoFormatH264 = mustGUID("34363248-0000-0010-8000-00aa00389b71")
	mfVideoFormatNV12 = mustGUID("3231564e-0000-0010-8000-00aa00389b71")

	mfMTMajorType      = mustGUID("48eba18e-f8c9-4687-bf11-0a74c9f96a8f")
	mfMTSubtype        = mustGUID("f7e34c9a-42e8-4714-b74b-cb29d72c35e5")
	mfMTFrameSize      = mustGUID("1652c33d-d6b2-4012-b834-72030849a37d")
	mfMTFrameRate      = mustGUID("c459a2e8-3d2c-4e44-b132-fee5156c7bb0")
	mfMTAvgBitrate     = mustGUID("20332624-fb0d-4d9e-bd0d-cbf6786c102e")
	mfMTInterlaceMode  = mustGUID("e2724bb8-e676-4806-b4b2-a8d6efb44ccd")
	mfMTPixelAspect    = mustGUID("c6376a1e-8d0a-4027-be45-6d9a0ad39bb6")
	mfMTDefaultStride  = mustGUID("644b4e48-1e02-4516-b0eb-c01ca9d49ac6")
	mfMTProfile        = mustGUID("ad76a80b-2d5c-4e0b-b375-64e520137036") // MF_MT_MPEG2_PROFILE
	mfMTSequenceHeader = mustGUID("3c036de7-3ad0-4c9e-9216-ee6d6ac21cb3")
	mfMTDisplayArea    = mustGUID("d7388766-18fe-48c6-a177-ee894867c8c4") // MF_MT_MINIMUM_DISPLAY_APERTURE
	mfLowLatency       = mustGUID("9c27891a-ed7a-40e1-88e8-b22727a024ee") // also CODECAPI_AVLowLatencyMode
	mfCleanPoint       = mustGUID("9cdf01d8-a0f0-43ba-b077-eaa06cbd728a") // MFSampleExtension_CleanPoint
	codecForceKey      = mustGUID("398c1b98-8353-475a-9ef2-8f265d260345") // CODECAPI_AVEncVideoForceKeyFrame
)

// Vtable slots (unknwn.h, mfobjects.h, mftransform.h, strmif.h).
const (
	vtQueryInterface = 0
	vtRelease        = 2

	vtAttrGetUINT32 = 7 // IMFAttributes, which IMFMediaType and IMFSample extend
	vtAttrGetUINT64 = 8
	vtAttrGetGUID   = 10
	vtAttrGetBlob   = 15
	vtAttrBlobSize  = 14
	vtAttrSetUINT32 = 21
	vtAttrSetUINT64 = 22
	vtAttrSetGUID   = 24

	vtSampleSetTime     = 36 // IMFSample
	vtSampleSetDuration = 38
	vtSampleContiguous  = 41
	vtSampleAddBuffer   = 42

	vtBufferLock      = 3 // IMFMediaBuffer
	vtBufferUnlock    = 4
	vtBufferSetLength = 6

	vtMFTOutputInfo      = 7 // IMFTransform
	vtMFTAttributes      = 8
	vtMFTInputAvailable  = 13
	vtMFTOutputAvailable = 14
	vtMFTSetInputType    = 15
	vtMFTSetOutputType   = 16
	vtMFTOutputCurrent   = 18
	vtMFTMessage         = 23
	vtMFTProcessInput    = 24
	vtMFTProcessOutput   = 25

	vtCodecSetValue = 9 // ICodecAPI
)

const (
	hrNeedMoreInput = -1072861838 // MF_E_TRANSFORM_NEED_MORE_INPUT 0xC00D6D72
	hrStreamChange  = -1072861855 // MF_E_TRANSFORM_STREAM_CHANGE 0xC00D6D61
	hrNotAccepting  = -1072875851 // MF_E_NOTACCEPTING 0xC00D36B5
	hrNoMoreTypes   = -1072875847 // MF_E_NO_MORE_TYPES 0xC00D36B9

	msgFlush          = 0x00000000 // MFT_MESSAGE_COMMAND_FLUSH
	msgDrain          = 0x00000001 // MFT_MESSAGE_COMMAND_DRAIN
	msgBeginStreaming = 0x10000000 // MFT_MESSAGE_NOTIFY_BEGIN_STREAMING
	msgStartOfStream  = 0x10000003 // MFT_MESSAGE_NOTIFY_START_OF_STREAM

	providesSamples = 0x100 | 0x200 // MFT_OUTPUT_STREAM_PROVIDES_SAMPLES, _CAN_PROVIDE_SAMPLES

	progressive = 2          // MFVideoInterlace_Progressive
	baseline    = 66         // eAVEncH264VProfile_Base
	hns         = 10_000_000 // Media Foundation's clock: 100 ns units
)

// com calls method idx of a COM object. uintptrescapes keeps what the arguments point to alive and in place.
//
//go:uintptrescapes
func com(obj uintptr, idx int, args ...uintptr) int32 {
	vtbl := *(*unsafe.Pointer)(ptr(obj))
	fn := *(*uintptr)(unsafe.Add(vtbl, idx*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)

	return int32(r)
}

// ptr converts an address the system owns back to a pointer without tripping vet.
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

func release(obj uintptr) {
	if obj != 0 {
		com(obj, vtRelease)
	}
}

func hrErr(step string, hr int32) error { return fmt.Errorf("%s: 0x%08x", step, uint32(hr)) }

var errNoMF = errors.New("o Media Foundation não está instalado (Windows N: instale o Media Feature Pack)")

var mfOnce = sync.OnceValue(func() error {
	if mfplat.Load() != nil || mfStartup.Find() != nil {
		return errNoMF
	}

	if r, _, _ := mfStartup.Call(0x00020070, 0); int32(r) < 0 { // MF_VERSION, MFSTARTUP_FULL
		return hrErr("MFStartup", int32(r))
	}

	return nil
})

// worker is a thread of the multithreaded apartment that runs one transform's calls, in order.
type worker struct{ jobs chan func() }

func newWorker() *worker {
	w := &worker{jobs: make(chan func())}
	go func() {
		runtime.LockOSThread()    // never unlocked: the thread ends with the goroutine
		coInitializeEx.Call(0, 0) // COINIT_MULTITHREADED

		for f := range w.jobs {
			f()
		}
	}()

	return w
}

func (w *worker) do(f func()) {
	done := make(chan struct{})
	w.jobs <- func() { f(); close(done) }
	<-done
}

// mediaType makes a video IMFMediaType of subtype sub with the given UINT32 and UINT64 attributes.
func mediaType(sub *windows.GUID, u32 map[*windows.GUID]uint32, u64 map[*windows.GUID]uint64) (uintptr, error) {
	var mt uintptr
	if r, _, _ := mfCreateType.Call(uintptr(unsafe.Pointer(&mt))); int32(r) < 0 {
		return 0, hrErr("MFCreateMediaType", int32(r))
	}

	com(mt, vtAttrSetGUID, uintptr(unsafe.Pointer(&mfMTMajorType)), uintptr(unsafe.Pointer(&mfMediaTypeVideo)))
	com(mt, vtAttrSetGUID, uintptr(unsafe.Pointer(&mfMTSubtype)), uintptr(unsafe.Pointer(sub)))
	for k, v := range u32 {
		com(mt, vtAttrSetUINT32, uintptr(unsafe.Pointer(k)), uintptr(v))
	}

	for k, v := range u64 {
		com(mt, vtAttrSetUINT64, uintptr(unsafe.Pointer(k)), uintptr(v))
	}

	return mt, nil
}

// newSample makes an IMFSample with one memory buffer of size bytes; fill (when set) writes its contents.
func newSample(size int, fill func([]byte) int) (uintptr, error) {
	var sample, buf uintptr
	if r, _, _ := mfCreateSample.Call(uintptr(unsafe.Pointer(&sample))); int32(r) < 0 {
		return 0, hrErr("MFCreateSample", int32(r))
	}

	if r, _, _ := mfCreateBuffer.Call(uintptr(size), uintptr(unsafe.Pointer(&buf))); int32(r) < 0 {
		release(sample)
		return 0, hrErr("MFCreateMemoryBuffer", int32(r))
	}
	defer release(buf) // the sample keeps its own reference

	if fill != nil {
		var data uintptr
		var maxLen, cur uint32
		if hr := com(buf, vtBufferLock, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&cur))); hr < 0 {
			release(sample)
			return 0, hrErr("Lock", hr)
		}

		n := fill(unsafe.Slice((*byte)(ptr(data)), size))
		com(buf, vtBufferUnlock)
		com(buf, vtBufferSetLength, uintptr(n))
	}

	com(sample, vtSampleAddBuffer, buf)

	return sample, nil
}

// sampleBytes gives read its sample's data, contiguous.
func sampleBytes(sample uintptr, read func([]byte)) error {
	var buf uintptr
	if hr := com(sample, vtSampleContiguous, uintptr(unsafe.Pointer(&buf))); hr < 0 {
		return hrErr("ConvertToContiguousBuffer", hr)
	}
	defer release(buf)

	var data uintptr
	var maxLen, cur uint32
	if hr := com(buf, vtBufferLock, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&cur))); hr < 0 {
		return hrErr("Lock", hr)
	}

	read(unsafe.Slice((*byte)(ptr(data)), cur))
	com(buf, vtBufferUnlock)

	return nil
}

// outputBuffer is MFT_OUTPUT_DATA_BUFFER; outputInfo is MFT_OUTPUT_STREAM_INFO.
type outputBuffer struct {
	id     uint32
	sample uintptr
	status uint32
	events uintptr
}

type outputInfo struct{ flags, size, align uint32 }

// transform makes a Media Foundation transform by class.
func transform(clsid *windows.GUID) (uintptr, error) {
	var mft uintptr
	r, _, _ := coCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, 1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidTransform)), uintptr(unsafe.Pointer(&mft)))
	if int32(r) < 0 {
		return 0, hrErr("CoCreateInstance", int32(r))
	}

	return mft, nil
}

// pull takes every picture or access unit the transform has ready; size is what a sample we provide holds.
func pull(mft uintptr, info *outputInfo, size int, onType func() error, got func(sample uintptr) error) error {
	for {
		var out outputBuffer
		if info.flags&providesSamples == 0 {
			s, err := newSample(max(int(info.size), size), nil)
			if err != nil {
				return err
			}

			out.sample = s
		}

		var status uint32
		hr := com(mft, vtMFTProcessOutput, 0, 1, uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&status)))
		release(out.events)

		var err error
		switch {
		case hr == hrNeedMoreInput:
			release(out.sample)
			return nil
		case hr == hrStreamChange:
			err = onType()
		case hr < 0:
			err = hrErr("ProcessOutput", hr)
		default:
			err = got(out.sample)
		}

		release(out.sample)
		if err != nil {
			return err
		}
	}
}

// Pictures are frames in Go memory, named by a number: the same Retain/Release as macOS's pixel buffers.
type frame struct {
	w, h  int
	y, uv []byte // tight: strides w
	refs  atomic.Int32
}

var pictures = struct {
	sync.Mutex
	m    map[Picture]*frame
	next Picture
}{m: map[Picture]*frame{}}

// Picture is a decoded or captured frame (NV12). Release it when done.
type Picture uintptr

func addPicture(f *frame) Picture {
	f.refs.Store(1)
	pictures.Lock()
	defer pictures.Unlock()
	pictures.next++
	pictures.m[pictures.next] = f

	return pictures.next
}

func (p Picture) frame() *frame {
	if p == 0 {
		return nil
	}

	pictures.Lock()
	defer pictures.Unlock()

	return pictures.m[p]
}

func (p Picture) Retain() Picture {
	if f := p.frame(); f != nil {
		f.refs.Add(1)
	}

	return p
}

func (p Picture) Release() {
	if f := p.frame(); f != nil && f.refs.Add(-1) == 0 {
		pictures.Lock()
		delete(pictures.m, p)
		pictures.Unlock()
	}
}

func (p Picture) Size() (w, h int) {
	if f := p.frame(); f != nil {
		return f.w, f.h
	}

	return 0, 0
}

// Read gives fill the picture's planes, read-only.
func (p Picture) Read(fill func(y, uv []byte, yStride, uvStride int)) {
	if f := p.frame(); f != nil {
		fill(f.y, f.uv, f.w, f.w)
	}
}

// NewPicture makes a w×h picture filled by fill(y, uv, yStride, uvStride).
func NewPicture(w, h int, fill func(y, uv []byte, yStride, uvStride int)) (Picture, error) {
	if w <= 0 || h <= 0 || w%2 != 0 || h%2 != 0 {
		return 0, errors.New("picture: odd or empty size")
	}

	f := &frame{w: w, h: h, y: make([]byte, w*h), uv: make([]byte, w*h/2)}
	fill(f.y, f.uv, w, w)

	return addPicture(f), nil
}

// Decoder turns H.264 access units (Annex B, as pion's depacketizer gives them) into Pictures.
type Decoder struct {
	w    *worker
	mft  uintptr
	out  func(Picture)
	info outputInfo

	width, height, stride int // the output: coded size and row stride
	showW, showH          int // what to show of it
	pts                   int64
}

// NewDecoder calls out (on the decoder's thread) with each picture, which out owns.
func NewDecoder(out func(Picture)) (*Decoder, error) {
	if err := mfOnce(); err != nil {
		return nil, err
	}

	d := &Decoder{w: newWorker(), out: out}
	var err error
	d.w.do(func() { err = d.open() })
	if err != nil {
		d.Close()
		return nil, err
	}

	return d, nil
}

func (d *Decoder) open() error {
	var err error
	if d.mft, err = transform(&clsidH264Decoder); err != nil {
		return err
	}

	// Low latency: every picture comes out as soon as it is decoded, none held back for reordering.
	var attrs uintptr
	if com(d.mft, vtMFTAttributes, uintptr(unsafe.Pointer(&attrs))) >= 0 && attrs != 0 {
		com(attrs, vtAttrSetUINT32, uintptr(unsafe.Pointer(&mfLowLatency)), 1)
		release(attrs)
	}

	in, err := mediaType(&mfVideoFormatH264, nil, nil)
	if err != nil {
		return err
	}
	defer release(in)

	if hr := com(d.mft, vtMFTSetInputType, 0, in, 0); hr < 0 {
		return hrErr("SetInputType", hr)
	}

	if err := d.setOutput(); err != nil {
		return err
	}

	com(d.mft, vtMFTMessage, msgBeginStreaming, 0)
	com(d.mft, vtMFTMessage, msgStartOfStream, 0)

	return nil
}

// setOutput picks NV12 out, and reads its size: at the start and whenever the stream changes (a new resolution).
func (d *Decoder) setOutput() error {
	for i := 0; ; i++ {
		var mt uintptr
		if hr := com(d.mft, vtMFTOutputAvailable, 0, uintptr(i), uintptr(unsafe.Pointer(&mt))); hr < 0 {
			if hr == hrNoMoreTypes {
				return errors.New("o decodificador não entrega NV12")
			}

			return hrErr("GetOutputAvailableType", hr)
		}

		var sub windows.GUID
		com(mt, vtAttrGetGUID, uintptr(unsafe.Pointer(&mfMTSubtype)), uintptr(unsafe.Pointer(&sub)))
		if sub != mfVideoFormatNV12 {
			release(mt)
			continue
		}

		hr := com(d.mft, vtMFTSetOutputType, 0, mt, 0)
		if hr >= 0 {
			d.readType(mt)
		}

		release(mt)
		if hr < 0 {
			return hrErr("SetOutputType", hr)
		}

		com(d.mft, vtMFTOutputInfo, 0, uintptr(unsafe.Pointer(&d.info)))

		return nil
	}
}

func (d *Decoder) readType(mt uintptr) {
	var size uint64
	com(mt, vtAttrGetUINT64, uintptr(unsafe.Pointer(&mfMTFrameSize)), uintptr(unsafe.Pointer(&size)))
	d.width, d.height = int(size>>32), int(uint32(size))
	d.showW, d.showH = d.width, d.height

	var stride uint32
	d.stride = d.width
	if com(mt, vtAttrGetUINT32, uintptr(unsafe.Pointer(&mfMTDefaultStride)), uintptr(unsafe.Pointer(&stride))) >= 0 && int32(stride) >= int32(d.width) {
		d.stride = int(int32(stride))
	}

	// The coded size is a whole number of macroblocks (1088 lines for 1080): the aperture is what is shown.
	var area struct {
		offX, offY uint32 // MFOffset: fract, value
		cx, cy     int32
	}
	var n uint32
	if com(mt, vtAttrGetBlob, uintptr(unsafe.Pointer(&mfMTDisplayArea)), uintptr(unsafe.Pointer(&area)), unsafe.Sizeof(area), uintptr(unsafe.Pointer(&n))) >= 0 &&
		area.cx > 0 && area.cy > 0 && int(area.cx) <= d.width && int(area.cy) <= d.height {
		d.showW, d.showH = int(area.cx)&^1, int(area.cy)&^1
	}
}

// Decode decodes one access unit.
func (d *Decoder) Decode(au []byte) error {
	var err error
	d.w.do(func() { err = d.decode(au) })

	return err
}

func (d *Decoder) decode(au []byte) error {
	sample, err := newSample(len(au), func(b []byte) int { return copy(b, au) })
	if err != nil {
		return err
	}
	defer release(sample)

	com(sample, vtSampleSetTime, uintptr(d.pts))
	d.pts += hns / 30

	for try := 0; ; try++ {
		hr := com(d.mft, vtMFTProcessInput, 0, sample, 0)
		if hr == hrNotAccepting && try == 0 {
			if err := d.pull(); err != nil {
				return err
			}

			continue
		}

		if hr < 0 {
			return hrErr("ProcessInput", hr)
		}

		return d.pull()
	}
}

func (d *Decoder) pull() error {
	return pull(d.mft, &d.info, d.stride*d.height*3/2, d.setOutput, func(sample uintptr) error {
		return sampleBytes(sample, func(b []byte) {
			planes := d.stride * d.height
			if d.showW <= 0 || len(b) < planes+d.stride*(d.height/2) {
				return
			}

			w, h := d.showW, d.showH
			f := &frame{w: w, h: h, y: make([]byte, w*h), uv: make([]byte, w*h/2)}
			for row := range h {
				copy(f.y[row*w:(row+1)*w], b[row*d.stride:])
			}

			for row := range h / 2 {
				copy(f.uv[row*w:(row+1)*w], b[planes+row*d.stride:])
			}

			d.out(addPicture(f))
		})
	})
}

func (d *Decoder) Close() {
	if d.w == nil {
		return
	}

	d.w.do(func() {
		release(d.mft)
		d.mft = 0
	})
	close(d.w.jobs)
	d.w = nil
}

// Encoder turns Pictures into H.264 access units (Annex B, parameter sets before each keyframe) in Baseline: no
// B-frames, so every frame goes out as soon as it is encoded, and every browser decodes it.
type Encoder struct {
	w     *worker
	mft   uintptr
	codec uintptr // ICodecAPI: keyframes on demand (0 when the encoder has none)
	info  outputInfo
	out   func(au []byte, key bool)

	width, height, fps int
	params             []byte // SPS and PPS, Annex B
	key                atomic.Bool
	pts                int64
	au                 []byte
}

// NewEncoder encodes w×h pictures at about fps and bitrate (bits per second); out gets each access unit on the
// encoder's thread (au is reused afterwards).
func NewEncoder(w, h, fps, bitrate int, out func(au []byte, key bool)) (*Encoder, error) {
	if err := mfOnce(); err != nil {
		return nil, err
	}

	e := &Encoder{w: newWorker(), out: out, width: w, height: h, fps: max(1, fps)}
	var err error
	e.w.do(func() { err = e.open(bitrate) })
	if err != nil {
		e.Close()
		return nil, err
	}

	return e, nil
}

func (e *Encoder) open(bitrate int) error {
	var err error
	if e.mft, err = transform(&clsidH264Encoder); err != nil {
		return err
	}

	if com(e.mft, vtQueryInterface, uintptr(unsafe.Pointer(&iidCodecAPI)), uintptr(unsafe.Pointer(&e.codec))) >= 0 {
		v := variant{vt: 11, val: 0xffff} // VT_BOOL, VARIANT_TRUE
		com(e.codec, vtCodecSetValue, uintptr(unsafe.Pointer(&mfLowLatency)), uintptr(unsafe.Pointer(&v)))
	} else {
		e.codec = 0
	}

	size := uint64(e.width)<<32 | uint64(e.height)
	rate := uint64(e.fps)<<32 | 1
	common := map[*windows.GUID]uint64{&mfMTFrameSize: size, &mfMTFrameRate: rate, &mfMTPixelAspect: 1<<32 | 1}

	// Output first: the encoder offers inputs for the output it was given.
	outType, err := mediaType(&mfVideoFormatH264, map[*windows.GUID]uint32{
		&mfMTAvgBitrate: uint32(bitrate), &mfMTInterlaceMode: progressive, &mfMTProfile: baseline,
	}, common)
	if err != nil {
		return err
	}
	defer release(outType)

	if hr := com(e.mft, vtMFTSetOutputType, 0, outType, 0); hr < 0 {
		return hrErr("SetOutputType", hr)
	}

	inType, err := mediaType(&mfVideoFormatNV12, map[*windows.GUID]uint32{&mfMTInterlaceMode: progressive}, common)
	if err != nil {
		return err
	}
	defer release(inType)

	if hr := com(e.mft, vtMFTSetInputType, 0, inType, 0); hr < 0 {
		return hrErr("SetInputType", hr)
	}

	// The parameter sets, to put before a keyframe that comes without them.
	var cur uintptr
	if com(e.mft, vtMFTOutputCurrent, 0, uintptr(unsafe.Pointer(&cur))) >= 0 {
		var n uint32
		if com(cur, vtAttrBlobSize, uintptr(unsafe.Pointer(&mfMTSequenceHeader)), uintptr(unsafe.Pointer(&n))) >= 0 && n > 0 {
			e.params = make([]byte, n)
			com(cur, vtAttrGetBlob, uintptr(unsafe.Pointer(&mfMTSequenceHeader)), uintptr(unsafe.Pointer(&e.params[0])), uintptr(n), uintptr(unsafe.Pointer(&n)))
		}

		release(cur)
	}

	com(e.mft, vtMFTOutputInfo, 0, uintptr(unsafe.Pointer(&e.info)))
	com(e.mft, vtMFTMessage, msgBeginStreaming, 0)
	com(e.mft, vtMFTMessage, msgStartOfStream, 0)

	return nil
}

// variant is VARIANT on 64-bit Windows: a type, three reserved words, and a 16-byte union.
type variant struct {
	vt  uint16
	_   [3]uint16
	val uint64
	_   uint64
}

// Size is the picture size the encoder was made for.
func (e *Encoder) Size() (w, h int) { return e.width, e.height }

// ForceKeyframe makes the next frame a keyframe (a viewer lost pictures, or a new one joined).
func (e *Encoder) ForceKeyframe() { e.key.Store(true) }

// Encode encodes a picture of the encoder's size.
func (e *Encoder) Encode(p Picture) error {
	f := p.frame()
	if f == nil {
		return nil
	}

	if f.w != e.width || f.h != e.height {
		return errors.New("h264: picture size changed")
	}

	var err error
	e.w.do(func() { err = e.encode(f) })

	return err
}

func (e *Encoder) encode(f *frame) error {
	sample, err := newSample(len(f.y)+len(f.uv), func(b []byte) int { return copy(b[copy(b, f.y):], f.uv) + len(f.y) })
	if err != nil {
		return err
	}
	defer release(sample)

	com(sample, vtSampleSetTime, uintptr(e.pts))
	com(sample, vtSampleSetDuration, uintptr(hns/int64(e.fps)))
	e.pts += hns / int64(e.fps)

	if e.key.Swap(false) && e.codec != 0 {
		v := variant{vt: 19, val: 1} // VT_UI4
		com(e.codec, vtCodecSetValue, uintptr(unsafe.Pointer(&codecForceKey)), uintptr(unsafe.Pointer(&v)))
	}

	for try := 0; ; try++ {
		hr := com(e.mft, vtMFTProcessInput, 0, sample, 0)
		if hr == hrNotAccepting && try == 0 {
			if err := e.pull(); err != nil {
				return err
			}

			continue
		}

		if hr < 0 {
			return hrErr("ProcessInput", hr)
		}

		return e.pull()
	}
}

func (e *Encoder) pull() error {
	return pull(e.mft, &e.info, e.width*e.height, func() error { return nil }, func(sample uintptr) error {
		var clean uint32
		com(sample, vtAttrGetUINT32, uintptr(unsafe.Pointer(&mfCleanPoint)), uintptr(unsafe.Pointer(&clean)))
		key := clean != 0

		return sampleBytes(sample, func(b []byte) {
			e.au = e.au[:0]
			if key && !hasNAL(b, 7) {
				e.au = append(e.au, e.params...)
			}

			e.au = append(e.au, b...)
			e.out(e.au, key)
		})
	})
}

// Flush waits for every queued frame to come out.
func (e *Encoder) Flush() {
	if e.w == nil {
		return
	}

	e.w.do(func() {
		com(e.mft, vtMFTMessage, msgDrain, 0)
		_ = e.pull()
	})
}

func (e *Encoder) Close() {
	if e.w == nil {
		return
	}

	e.w.do(func() {
		if e.mft != 0 {
			com(e.mft, vtMFTMessage, msgFlush, 0)
		}

		release(e.codec)
		release(e.mft)
		e.mft, e.codec = 0, 0
	})
	close(e.w.jobs)
	e.w = nil
}

// hasNAL says whether an Annex B access unit has a NAL unit of type t.
func hasNAL(au []byte, t byte) bool {
	for i := 0; i+3 < len(au); i++ {
		if au[i] == 0 && au[i+1] == 0 && au[i+2] == 1 && au[i+3]&0x1f == t {
			return true
		}
	}

	return false
}
