package voice

// macOS video: VideoToolbox's H.264 encoder and decoder, Core Media's buffers and Core Video's pixel buffers,
// through purego. Pictures are CVPixelBuffers in '420v' backed by IOSurfaces: what ScreenCaptureKit gives the
// encoder and what the window draws without a copy (gpu.Renderer.ImportPixelBuffer).

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	pixel420v   = 0x34323076 // kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange
	codecH264   = 0x61766331 // kCMVideoCodecType_H264 'avc1'
	cfNumberI32 = 3          // kCFNumberSInt32Type
	cfNumberF64 = 13         // kCFNumberFloat64Type
)

// cmTime is CMTime. Passed by value it is larger than 16 bytes: arm64 passes a pointer to a copy, which is what
// the functions below declare.
type cmTime struct {
	Value     int64
	Timescale int32
	Flags     uint32
	Epoch     int64
}

var vt struct {
	once sync.Once
	err  error

	cfDictionaryCreate func(alloc uintptr, keys, values *uintptr, n int, kcb, vcb uintptr) uintptr
	cfNumberCreate     func(alloc uintptr, typ int, v unsafe.Pointer) uintptr
	cfRetain           func(uintptr) uintptr
	cfRelease          func(uintptr)

	formatFromH264 func(alloc uintptr, n uint, sets *uintptr, sizes *uint, nalLen int32, out *uintptr) int32
	formatParamSet func(f uintptr, i uint, ptr *uintptr, size, count *uint, nalLen *int32) int32
	formatDims     func(f uintptr) uint64
	blockCreate    func(alloc, mem uintptr, blockLen uint, blockAlloc, custom uintptr, off, dataLen uint, flags uint32, out *uintptr) int32
	blockReplace   func(src unsafe.Pointer, bb uintptr, off, n uint) int32
	blockLength    func(bb uintptr) uint
	blockCopy      func(bb uintptr, off, n uint, dst unsafe.Pointer) int32
	sampleCreate   func(alloc, data, format uintptr, nSamples, nTiming int, timing uintptr, nSizes int, sizes *uint, out *uintptr) int32
	sampleData     func(sb uintptr) uintptr
	sampleFormat   func(sb uintptr) uintptr
	sampleImage    func(sb uintptr) uintptr
	decCreate      func(alloc, format, spec, attrs uintptr, cb *[2]uintptr, out *uintptr) int32
	decDecode      func(s, sb uintptr, flags uint32, ref uintptr, info *uint32) int32
	decWait        func(s uintptr) int32
	decInvalidate  func(s uintptr)
	encCreate      func(alloc uintptr, w, h int32, codec uint32, spec, srcAttrs, dataAlloc, cb, ref uintptr, out *uintptr) int32
	setProperty    func(s, key, value uintptr) int32
	encPrepare     func(s uintptr) int32
	encEncode      func(s, pb uintptr, pts, dur *cmTime, props, ref uintptr, info *uint32) int32
	encComplete    func(s uintptr, until *cmTime) int32
	encInvalidate  func(s uintptr)
	pbCreate       func(alloc uintptr, w, h uint, format uint32, attrs uintptr, out *uintptr) int32
	pbLock         func(pb uintptr, flags uint64) int32
	pbUnlock       func(pb uintptr, flags uint64) int32
	pbPlaneBase    func(pb uintptr, plane uint) uintptr
	pbPlaneStride  func(pb uintptr, plane uint) uint
	pbPlaneHeight  func(pb uintptr, plane uint) uint
	pbWidth        func(pb uintptr) uint
	pbHeight       func(pb uintptr) uint

	kTrue, kFalse, dictKeys, dictValues                   uintptr
	kPixelFormat, kIOSurface, kMetal                      uintptr
	kRealTime, kProfile, kBaseline, kReorder, kBitRate    uintptr
	kKeyInterval, kKeyIntervalSecs, kFrameRate, kForceKey uintptr
	attrs420v                                             uintptr // the pixel buffer attributes we ask for and make
	forceKeyProps                                         uintptr // {ForceKeyFrame: true}
	decoderCB, encoderCB                                  uintptr

	mu       sync.Mutex
	decoders map[uintptr]*Decoder
	encoders map[uintptr]*Encoder
	nextID   uintptr
}

// deref reads the pointer-sized value at a C address (a framework's exported constant).
func deref(addr uintptr) uintptr { return **(**uintptr)(unsafe.Pointer(&addr)) }

// cBytes views n bytes of C memory.
func cBytes(addr uintptr, n int) []byte { return unsafe.Slice(*(**byte)(unsafe.Pointer(&addr)), n) }

func loadVT() error {
	vt.once.Do(func() { vt.err = loadVTOnce() })
	return vt.err
}

func loadVTOnce() (err error) {
	defer func() {
		if r := recover(); r != nil { // RegisterLibFunc panics on a missing symbol
			err = fmt.Errorf("VideoToolbox: %v", r)
		}
	}()

	open := func(name string) (uintptr, error) {
		return purego.Dlopen("/System/Library/Frameworks/"+name+".framework/"+name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	}

	libs := map[string]uintptr{}
	for _, name := range []string{"CoreFoundation", "CoreMedia", "CoreVideo", "VideoToolbox"} {
		h, err := open(name)
		if err != nil {
			return err
		}

		libs[name] = h
	}

	cf, cm, cv, vtb := libs["CoreFoundation"], libs["CoreMedia"], libs["CoreVideo"], libs["VideoToolbox"]
	reg := purego.RegisterLibFunc
	reg(&vt.cfDictionaryCreate, cf, "CFDictionaryCreate")
	reg(&vt.cfNumberCreate, cf, "CFNumberCreate")
	reg(&vt.cfRetain, cf, "CFRetain")
	reg(&vt.cfRelease, cf, "CFRelease")
	reg(&vt.formatFromH264, cm, "CMVideoFormatDescriptionCreateFromH264ParameterSets")
	reg(&vt.formatParamSet, cm, "CMVideoFormatDescriptionGetH264ParameterSetAtIndex")
	reg(&vt.formatDims, cm, "CMVideoFormatDescriptionGetDimensions")
	reg(&vt.blockCreate, cm, "CMBlockBufferCreateWithMemoryBlock")
	reg(&vt.blockReplace, cm, "CMBlockBufferReplaceDataBytes")
	reg(&vt.blockLength, cm, "CMBlockBufferGetDataLength")
	reg(&vt.blockCopy, cm, "CMBlockBufferCopyDataBytes")
	reg(&vt.sampleCreate, cm, "CMSampleBufferCreateReady")
	reg(&vt.sampleData, cm, "CMSampleBufferGetDataBuffer")
	reg(&vt.sampleFormat, cm, "CMSampleBufferGetFormatDescription")
	reg(&vt.sampleImage, cm, "CMSampleBufferGetImageBuffer")
	reg(&vt.decCreate, vtb, "VTDecompressionSessionCreate")
	reg(&vt.decDecode, vtb, "VTDecompressionSessionDecodeFrame")
	reg(&vt.decWait, vtb, "VTDecompressionSessionWaitForAsynchronousFrames")
	reg(&vt.decInvalidate, vtb, "VTDecompressionSessionInvalidate")
	reg(&vt.encCreate, vtb, "VTCompressionSessionCreate")
	reg(&vt.setProperty, vtb, "VTSessionSetProperty")
	reg(&vt.encPrepare, vtb, "VTCompressionSessionPrepareToEncodeFrames")
	reg(&vt.encEncode, vtb, "VTCompressionSessionEncodeFrame")
	reg(&vt.encComplete, vtb, "VTCompressionSessionCompleteFrames")
	reg(&vt.encInvalidate, vtb, "VTCompressionSessionInvalidate")
	reg(&vt.pbCreate, cv, "CVPixelBufferCreate")
	reg(&vt.pbLock, cv, "CVPixelBufferLockBaseAddress")
	reg(&vt.pbUnlock, cv, "CVPixelBufferUnlockBaseAddress")
	reg(&vt.pbPlaneBase, cv, "CVPixelBufferGetBaseAddressOfPlane")
	reg(&vt.pbPlaneStride, cv, "CVPixelBufferGetBytesPerRowOfPlane")
	reg(&vt.pbPlaneHeight, cv, "CVPixelBufferGetHeightOfPlane")
	reg(&vt.pbWidth, cv, "CVPixelBufferGetWidth")
	reg(&vt.pbHeight, cv, "CVPixelBufferGetHeight")

	sym := func(lib uintptr, name string) uintptr {
		p, err := purego.Dlsym(lib, name)
		if err != nil {
			panic(name)
		}

		return p
	}

	vt.kTrue, vt.kFalse = deref(sym(cf, "kCFBooleanTrue")), deref(sym(cf, "kCFBooleanFalse"))
	vt.dictKeys, vt.dictValues = sym(cf, "kCFTypeDictionaryKeyCallBacks"), sym(cf, "kCFTypeDictionaryValueCallBacks")
	vt.kPixelFormat = deref(sym(cv, "kCVPixelBufferPixelFormatTypeKey"))
	vt.kIOSurface = deref(sym(cv, "kCVPixelBufferIOSurfacePropertiesKey"))
	vt.kMetal = deref(sym(cv, "kCVPixelBufferMetalCompatibilityKey"))
	vt.kRealTime = deref(sym(vtb, "kVTCompressionPropertyKey_RealTime"))
	vt.kProfile = deref(sym(vtb, "kVTCompressionPropertyKey_ProfileLevel"))
	vt.kBaseline = deref(sym(vtb, "kVTProfileLevel_H264_ConstrainedBaseline_AutoLevel"))
	vt.kReorder = deref(sym(vtb, "kVTCompressionPropertyKey_AllowFrameReordering"))
	vt.kBitRate = deref(sym(vtb, "kVTCompressionPropertyKey_AverageBitRate"))
	vt.kKeyInterval = deref(sym(vtb, "kVTCompressionPropertyKey_MaxKeyFrameInterval"))
	vt.kKeyIntervalSecs = deref(sym(vtb, "kVTCompressionPropertyKey_MaxKeyFrameIntervalDuration"))
	vt.kFrameRate = deref(sym(vtb, "kVTCompressionPropertyKey_ExpectedFrameRate"))
	vt.kForceKey = deref(sym(vtb, "kVTEncodeFrameOptionKey_ForceKeyFrame"))

	format := uint32(pixel420v)
	empty := cfDict(nil, nil)
	vt.attrs420v = cfDict([]uintptr{vt.kPixelFormat, vt.kIOSurface, vt.kMetal}, []uintptr{cfInt(int32(format)), empty, vt.kTrue})
	vt.forceKeyProps = cfDict([]uintptr{vt.kForceKey}, []uintptr{vt.kTrue})

	vt.decoderCB = purego.NewCallback(func(ref, frameRef uintptr, status int32, flags uint32, image uintptr) {
		vt.mu.Lock()
		d := vt.decoders[ref]
		vt.mu.Unlock()

		if d != nil && status == 0 && image != 0 {
			d.out(Picture(vt.cfRetain(image)))
		}
	})
	vt.encoderCB = purego.NewCallback(func(ref, frameRef uintptr, status int32, flags uint32, sample uintptr) {
		vt.mu.Lock()
		e := vt.encoders[ref]
		vt.mu.Unlock()

		if e != nil && status == 0 && sample != 0 {
			e.output(sample)
		}
	})
	vt.decoders, vt.encoders = map[uintptr]*Decoder{}, map[uintptr]*Encoder{}

	return nil
}

func cfInt(v int32) uintptr { return vt.cfNumberCreate(0, cfNumberI32, unsafe.Pointer(&v)) }

func cfFloat(v float64) uintptr { return vt.cfNumberCreate(0, cfNumberF64, unsafe.Pointer(&v)) }

// cfDict makes a CFDictionary (it retains keys and values: the caller keeps its own references).
func cfDict(keys, values []uintptr) uintptr {
	var k, v *uintptr
	if len(keys) > 0 {
		k, v = &keys[0], &values[0]
	}

	return vt.cfDictionaryCreate(0, k, v, len(keys), vt.dictKeys, vt.dictValues)
}

// Picture is a decoded or captured frame: a retained CVPixelBuffer ('420v'). Release it when done.
type Picture uintptr

func (p Picture) Retain() Picture {
	if p != 0 {
		vt.cfRetain(uintptr(p))
	}

	return p
}

func (p Picture) Release() {
	if p != 0 {
		vt.cfRelease(uintptr(p))
	}
}

func (p Picture) Size() (w, h int) { return int(vt.pbWidth(uintptr(p))), int(vt.pbHeight(uintptr(p))) }

// Decoder turns H.264 access units (Annex B, as pion's depacketizer gives them) into Pictures.
type Decoder struct {
	id      uintptr
	out     func(Picture)
	session uintptr
	format  uintptr
	sps     []byte
	pps     []byte
	avcc    []byte
}

// NewDecoder calls out (on a VideoToolbox thread) with each picture, which out owns.
func NewDecoder(out func(Picture)) (*Decoder, error) {
	if err := loadVT(); err != nil {
		return nil, err
	}

	d := &Decoder{out: out}

	vt.mu.Lock()
	vt.nextID++
	d.id = vt.nextID
	vt.decoders[d.id] = d
	vt.mu.Unlock()

	return d, nil
}

var errNeedKeyframe = errors.New("h264: waiting for a keyframe")

// Decode decodes one access unit. errNeedKeyframe means nothing can be shown until the parameter sets come.
func (d *Decoder) Decode(au []byte) error {
	d.avcc = d.avcc[:0]
	for _, nal := range splitAnnexB(au) {
		switch nal[0] & 0x1f {
		case 7:
			if string(nal) != string(d.sps) {
				d.sps = append(d.sps[:0], nal...)
				d.reset()
			}
		case 8:
			if string(nal) != string(d.pps) {
				d.pps = append(d.pps[:0], nal...)
				d.reset()
			}
		case 9: // access unit delimiter: not for VideoToolbox
		default:
			n := len(nal)
			d.avcc = append(d.avcc, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
			d.avcc = append(d.avcc, nal...)
		}
	}

	if d.session == 0 {
		if len(d.sps) == 0 || len(d.pps) == 0 {
			return errNeedKeyframe
		}

		if err := d.open(); err != nil {
			return err
		}
	}

	if len(d.avcc) == 0 {
		return nil
	}

	var bb, sb uintptr
	if st := vt.blockCreate(0, 0, uint(len(d.avcc)), 0, 0, 0, uint(len(d.avcc)), 0, &bb); st != 0 {
		return fmt.Errorf("CMBlockBufferCreateWithMemoryBlock: %d", st)
	}
	defer vt.cfRelease(bb)

	vt.blockReplace(unsafe.Pointer(&d.avcc[0]), bb, 0, uint(len(d.avcc)))

	size := uint(len(d.avcc))
	if st := vt.sampleCreate(0, bb, d.format, 1, 0, 0, 1, &size, &sb); st != 0 {
		return fmt.Errorf("CMSampleBufferCreateReady: %d", st)
	}
	defer vt.cfRelease(sb)

	var info uint32
	if st := vt.decDecode(d.session, sb, 0, 0, &info); st != 0 {
		d.reset() // a broken session (-12909 and friends) starts over at the next keyframe
		return fmt.Errorf("VTDecompressionSessionDecodeFrame: %d", st)
	}

	return nil
}

func (d *Decoder) open() error {
	sets := []uintptr{uintptr(unsafe.Pointer(&d.sps[0])), uintptr(unsafe.Pointer(&d.pps[0]))}
	sizes := []uint{uint(len(d.sps)), uint(len(d.pps))}
	st := vt.formatFromH264(0, 2, &sets[0], &sizes[0], 4, &d.format)
	runtime.KeepAlive(d.sps)
	runtime.KeepAlive(d.pps)
	if st != 0 {
		return fmt.Errorf("CMVideoFormatDescriptionCreateFromH264ParameterSets: %d", st)
	}

	cb := [2]uintptr{vt.decoderCB, d.id}
	if st := vt.decCreate(0, d.format, 0, vt.attrs420v, &cb, &d.session); st != 0 {
		vt.cfRelease(d.format)
		d.format = 0
		return fmt.Errorf("VTDecompressionSessionCreate: %d", st)
	}

	return nil
}

// reset drops the session: new parameter sets (another resolution) need a new one.
func (d *Decoder) reset() {
	if d.session != 0 {
		vt.decWait(d.session)
		vt.decInvalidate(d.session)
		vt.cfRelease(d.session)
		d.session = 0
	}

	if d.format != 0 {
		vt.cfRelease(d.format)
		d.format = 0
	}
}

func (d *Decoder) Close() {
	d.reset()

	vt.mu.Lock()
	delete(vt.decoders, d.id)
	vt.mu.Unlock()
}

// Encoder turns Pictures into H.264 access units (Annex B, parameter sets before each keyframe) in Constrained
// Baseline: no B-frames, so every frame goes out as soon as it is encoded, and every browser decodes it.
type Encoder struct {
	id       uintptr
	out      func(au []byte, key bool)
	session  uintptr
	w, h     int
	frames   int64
	fps      int
	mu       sync.Mutex
	forceKey bool
	buf      []byte
	life     sync.Mutex // Encode against Close, which another goroutine may call
}

// NewEncoder encodes w×h pictures at about fps and bitrate (bits per second); out gets each access unit on a
// VideoToolbox thread (au is reused afterwards).
func NewEncoder(w, h, fps, bitrate int, out func(au []byte, key bool)) (*Encoder, error) {
	if err := loadVT(); err != nil {
		return nil, err
	}

	e := &Encoder{out: out, w: w, h: h, fps: fps}

	vt.mu.Lock()
	vt.nextID++
	e.id = vt.nextID
	vt.encoders[e.id] = e
	vt.mu.Unlock()

	if st := vt.encCreate(0, int32(w), int32(h), codecH264, 0, vt.attrs420v, 0, vt.encoderCB, e.id, &e.session); st != 0 {
		e.Close()
		return nil, fmt.Errorf("VTCompressionSessionCreate: %d", st)
	}

	numbers := []uintptr{cfInt(int32(bitrate)), cfInt(int32(fps * 4)), cfFloat(4), cfInt(int32(fps))}
	for _, p := range []struct{ key, value uintptr }{
		{vt.kRealTime, vt.kTrue}, {vt.kProfile, vt.kBaseline}, {vt.kReorder, vt.kFalse},
		{vt.kBitRate, numbers[0]}, {vt.kKeyInterval, numbers[1]}, {vt.kKeyIntervalSecs, numbers[2]}, {vt.kFrameRate, numbers[3]},
	} {
		if p.value != 0 {
			vt.setProperty(e.session, p.key, p.value)
		}
	}

	for _, n := range numbers {
		vt.cfRelease(n)
	}

	vt.encPrepare(e.session)

	return e, nil
}

// Size is the picture size the encoder was made for.
func (e *Encoder) Size() (w, h int) { return e.w, e.h }

// ForceKeyframe makes the next frame a keyframe (a viewer lost pictures, or a new one joined).
func (e *Encoder) ForceKeyframe() {
	e.mu.Lock()
	e.forceKey = true
	e.mu.Unlock()
}

// Encode queues a picture; the encoder retains it as long as it needs it.
func (e *Encoder) Encode(p Picture) error {
	e.mu.Lock()
	props := uintptr(0)
	if e.forceKey || e.frames == 0 {
		props, e.forceKey = vt.forceKeyProps, false
	}

	pts := cmTime{Value: e.frames, Timescale: int32(e.fps), Flags: 1}
	e.frames++
	e.mu.Unlock()

	e.life.Lock()
	defer e.life.Unlock()

	if e.session == 0 {
		return errors.New("h264: encoder closed")
	}

	var info uint32
	if st := vt.encEncode(e.session, uintptr(p), &pts, &cmTime{}, props, 0, &info); st != 0 {
		return fmt.Errorf("VTCompressionSessionEncodeFrame: %d", st)
	}

	return nil
}

// Flush waits for every queued frame to come out.
func (e *Encoder) Flush() { vt.encComplete(e.session, &cmTime{}) }

func (e *Encoder) Close() {
	e.life.Lock()
	defer e.life.Unlock()

	if e.session != 0 {
		vt.encComplete(e.session, &cmTime{})
		vt.encInvalidate(e.session)
		vt.cfRelease(e.session)
		e.session = 0
	}

	vt.mu.Lock()
	delete(vt.encoders, e.id)
	vt.mu.Unlock()
}

// output converts one encoded sample (AVCC, 4-byte lengths) to Annex B, with SPS and PPS before a keyframe.
func (e *Encoder) output(sample uintptr) {
	bb := vt.sampleData(sample)
	if bb == 0 {
		return
	}

	n := vt.blockLength(bb)
	avcc := make([]byte, n)
	if n == 0 || vt.blockCopy(bb, 0, n, unsafe.Pointer(&avcc[0])) != 0 {
		return
	}

	e.buf = e.buf[:0]
	key := false
	for i := 0; i+4 <= len(avcc); {
		size := int(avcc[i])<<24 | int(avcc[i+1])<<16 | int(avcc[i+2])<<8 | int(avcc[i+3])
		i += 4
		if size <= 0 || i+size > len(avcc) {
			break
		}

		nal := avcc[i : i+size]
		i += size
		if nal[0]&0x1f == 5 && !key {
			key = true
			e.buf = append(e.buf, e.parameterSets(sample)...)
		}

		e.buf = append(e.buf, 0, 0, 0, 1)
		e.buf = append(e.buf, nal...)
	}

	if len(e.buf) > 0 {
		e.out(e.buf, key)
	}
}

func (e *Encoder) parameterSets(sample uintptr) []byte {
	f := vt.sampleFormat(sample)
	var out []byte
	for i := uint(0); i < 2; i++ {
		var ptr uintptr
		var size, count uint
		var nalLen int32
		if vt.formatParamSet(f, i, &ptr, &size, &count, &nalLen) != 0 || ptr == 0 {
			continue
		}

		out = append(out, 0, 0, 0, 1)
		out = append(out, cBytes(ptr, int(size))...)
	}

	return out
}

// NewPicture makes a w×h picture filled by fill(y, uv, yStride, uvStride): for tests and synthetic sources.
func NewPicture(w, h int, fill func(y, uv []byte, yStride, uvStride int)) (Picture, error) {
	if err := loadVT(); err != nil {
		return 0, err
	}

	var pb uintptr
	if st := vt.pbCreate(0, uint(w), uint(h), pixel420v, vt.attrs420v, &pb); st != 0 {
		return 0, fmt.Errorf("CVPixelBufferCreate: %d", st)
	}

	p := Picture(pb)
	p.planes(false, fill)

	return p, nil
}

// Read gives fill the picture's planes, read-only (tests).
func (p Picture) Read(fill func(y, uv []byte, yStride, uvStride int)) { p.planes(true, fill) }

func (p Picture) planes(readOnly bool, fill func(y, uv []byte, yStride, uvStride int)) {
	pb, flags := uintptr(p), uint64(0)
	if readOnly {
		flags = 1 // kCVPixelBufferLock_ReadOnly
	}

	vt.pbLock(pb, flags)
	defer vt.pbUnlock(pb, flags)

	ys, uvs := int(vt.pbPlaneStride(pb, 0)), int(vt.pbPlaneStride(pb, 1))
	y := cBytes(vt.pbPlaneBase(pb, 0), ys*int(vt.pbPlaneHeight(pb, 0)))
	uv := cBytes(vt.pbPlaneBase(pb, 1), uvs*int(vt.pbPlaneHeight(pb, 1)))
	fill(y, uv, ys, uvs)
}
