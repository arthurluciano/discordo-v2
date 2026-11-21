package voice

// macOS screen capture: ScreenCaptureKit through purego's Objective-C runtime. The system picker
// (SCContentSharingPicker, macOS 14+) chooses the display or window and needs no Screen Recording grant; the
// stream hands each frame (an IOSurface-backed '420v' CVPixelBuffer) to Go on a dispatch queue of ours.

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// ScreenCapture is a capture being chosen or running.
type ScreenCapture struct {
	opt     ScreenOptions
	onFrame func(Picture)
	onSound func(pcm []int16) // 20 ms or so of 48 kHz stereo, interleaved
	onEnd   func(error)

	mu         sync.Mutex
	stream     objc.ID
	srcW, srcH int // the chosen content's size in pixels
	ended      bool
}

type cgRect struct{ X, Y, W, H float64 }

var sc struct {
	once sync.Once
	err  error

	queue, audioQueue      uintptr
	audioList              func(sb uintptr, sizeOut *uintptr, list *audioBufferList, size uintptr, a1, a2 uintptr, flags uint32, block *uintptr) int32
	clsOutput, clsDelegate objc.Class
	clsObserver            objc.Class
	observer               objc.ID
	sampleImage            func(sb uintptr) uintptr
	queueCreate            func(label string, attr uintptr) uintptr
	asyncF                 func(queue, ctx, fn uintptr)
	mainQueue, mainFn      uintptr

	mainMu  sync.Mutex
	mainRun []func()

	mu      sync.Mutex
	byObj   map[objc.ID]*ScreenCapture // our output and delegate objects -> their capture
	picking *ScreenCapture
}

var (
	selAlloc   = objc.RegisterName("alloc")
	selInit    = objc.RegisterName("init")
	selRelease = objc.RegisterName("release")
	selRetain  = objc.RegisterName("retain")
)

func loadSC() error {
	sc.once.Do(func() { sc.err = loadSCOnce() })
	return sc.err
}

func loadSCOnce() error {
	if err := loadVT(); err != nil {
		return err
	}

	for _, name := range []string{"Foundation", "AppKit", "ScreenCaptureKit"} {
		if _, err := purego.Dlopen("/System/Library/Frameworks/"+name+".framework/"+name, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			return err
		}
	}

	if objc.GetClass("SCContentSharingPicker") == 0 {
		return errors.New("compartilhar a tela pede o macOS 14 ou mais novo")
	}

	sys, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW)
	if err != nil {
		return err
	}

	purego.RegisterLibFunc(&sc.queueCreate, sys, "dispatch_queue_create")
	purego.RegisterLibFunc(&sc.asyncF, sys, "dispatch_async_f")
	if sc.mainQueue, err = purego.Dlsym(sys, "_dispatch_main_q"); err != nil {
		return err
	}

	sc.mainFn = purego.NewCallback(func(uintptr) {
		sc.mainMu.Lock()
		run := sc.mainRun
		sc.mainRun = nil
		sc.mainMu.Unlock()

		for _, f := range run {
			f()
		}
	})

	cm, _ := purego.Dlopen("/System/Library/Frameworks/CoreMedia.framework/CoreMedia", purego.RTLD_NOW)
	purego.RegisterLibFunc(&sc.sampleImage, cm, "CMSampleBufferGetImageBuffer")
	purego.RegisterLibFunc(&sc.audioList, cm, "CMSampleBufferGetAudioBufferListWithRetainedBlockBuffer")
	sc.queue = sc.queueCreate("br.com.discordo.screen", 0)
	sc.audioQueue = sc.queueCreate("br.com.discordo.screen-audio", 0) // encoding sound must not hold pictures up
	sc.byObj = map[objc.ID]*ScreenCapture{}

	nsObject := objc.GetClass("NSObject")
	sc.clsOutput, err = objc.RegisterClass("DiscordoScreenOutput", nsObject, protocols("SCStreamOutput"), nil, []objc.MethodDef{{
		Cmd: objc.RegisterName("stream:didOutputSampleBuffer:ofType:"),
		Fn: func(self objc.ID, _ objc.SEL, stream objc.ID, sample uintptr, typ int) {
			c := captureOf(self)
			if c == nil || sample == 0 {
				return
			}

			if typ == 1 { // SCStreamOutputTypeAudio
				if pcm := samplePCM(sample); len(pcm) > 0 && c.onSound != nil {
					c.onSound(pcm)
				}

				return
			}

			if typ != 0 {
				return
			}

			// Idle frames (nothing changed on screen) carry no picture.
			if pb := sc.sampleImage(sample); pb != 0 {
				c.onFrame(Picture(pb))
			}
		},
	}})
	if err != nil {
		return err
	}

	sc.clsDelegate, err = objc.RegisterClass("DiscordoScreenDelegate", nsObject, protocols("SCStreamDelegate"), nil, []objc.MethodDef{{
		Cmd: objc.RegisterName("stream:didStopWithError:"),
		Fn: func(self objc.ID, _ objc.SEL, stream, nsErr objc.ID) {
			if c := captureOf(self); c != nil {
				c.end(nsError(nsErr, "a captura da tela parou"))
			}
		},
	}})
	if err != nil {
		return err
	}

	sc.clsObserver, err = objc.RegisterClass("DiscordoPickerObserver", nsObject, protocols("SCContentSharingPickerObserver"), nil, []objc.MethodDef{
		{Cmd: objc.RegisterName("contentSharingPicker:didUpdateWithFilter:forStream:"),
			Fn: func(self objc.ID, _ objc.SEL, picker, filter, stream objc.ID) {
				sc.mu.Lock()
				c := sc.picking
				sc.picking = nil
				sc.mu.Unlock()

				if c != nil {
					if err := c.start(filter); err != nil {
						c.end(err)
					}
				}
			}},
		{Cmd: objc.RegisterName("contentSharingPicker:didCancelForStream:"),
			Fn: func(self objc.ID, _ objc.SEL, picker, stream objc.ID) {
				sc.mu.Lock()
				c := sc.picking
				sc.picking = nil
				sc.mu.Unlock()

				if c != nil {
					c.end(ErrPickerCancelled)
				}
			}},
		{Cmd: objc.RegisterName("contentSharingPickerStartDidFailWithError:"),
			Fn: func(self objc.ID, _ objc.SEL, nsErr objc.ID) {
				sc.mu.Lock()
				c := sc.picking
				sc.picking = nil
				sc.mu.Unlock()

				if c != nil {
					c.end(nsError(nsErr, "o seletor de tela falhou"))
				}
			}},
	})
	if err != nil {
		return err
	}

	sc.observer = objc.ID(sc.clsObserver).Send(selAlloc).Send(selInit)
	picker := objc.ID(objc.GetClass("SCContentSharingPicker")).Send(objc.RegisterName("sharedPicker"))
	picker.Send(objc.RegisterName("addObserver:"), sc.observer)

	return nil
}

// protocols is the named protocol, when the runtime knows it (declaring it lets the system check conformance).
func protocols(name string) []*objc.Protocol {
	if p := objc.GetProtocol(name); p != nil {
		return []*objc.Protocol{p}
	}

	return nil
}

// onMain runs f on the main thread (AppKit's picker lives there), later.
func onMain(f func()) {
	sc.mainMu.Lock()
	sc.mainRun = append(sc.mainRun, f)
	sc.mainMu.Unlock()

	sc.asyncF(sc.mainQueue, 0, sc.mainFn)
}

// audioBufferList is CoreAudio's AudioBufferList with room for two buffers (stereo, non-interleaved).
type audioBufferList struct {
	n       uint32
	_       uint32
	buffers [2]struct {
		channels, size uint32
		data           uintptr
	}
}

// samplePCM is a sample buffer of ScreenCaptureKit's sound (32-bit float, one buffer per channel, or one
// interleaved) as 16-bit interleaved stereo.
func samplePCM(sample uintptr) []int16 {
	var list audioBufferList
	var block uintptr
	if sc.audioList(sample, nil, &list, unsafe.Sizeof(list), 0, 0, 0, &block) != 0 {
		return nil
	}
	defer vt.cfRelease(block)

	b := list.buffers
	var left, right []float32
	switch {
	case list.n >= 2:
		left, right = cFloats(b[0].data, int(b[0].size/4)), cFloats(b[1].data, int(b[1].size/4))
	case list.n == 1 && b[0].channels == 2:
		lr := cFloats(b[0].data, int(b[0].size/4))
		pcm := make([]int16, len(lr))
		for i, v := range lr {
			pcm[i] = toInt16(v)
		}

		return pcm
	case list.n == 1:
		left = cFloats(b[0].data, int(b[0].size/4))
		right = left
	default:
		return nil
	}

	n := min(len(left), len(right))
	pcm := make([]int16, 2*n)
	for i := range n {
		pcm[2*i], pcm[2*i+1] = toInt16(left[i]), toInt16(right[i])
	}

	return pcm
}

func cFloats(addr uintptr, n int) []float32 {
	if addr == 0 || n <= 0 {
		return nil
	}

	return unsafe.Slice(*(**float32)(unsafe.Pointer(&addr)), n)
}

func toInt16(v float32) int16 { return int16(max(-1, min(1, v)) * 32767) }

func captureOf(obj objc.ID) *ScreenCapture {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	return sc.byObj[obj]
}

// nsError is an NSError's localized description, or fallback.
func nsError(e objc.ID, fallback string) error {
	if e == 0 {
		return errors.New(fallback)
	}

	desc := e.Send(objc.RegisterName("localizedDescription"))
	if s := goString(desc); s != "" {
		return errors.New(s)
	}

	return errors.New(fallback)
}

func goString(nsString objc.ID) string {
	if nsString == 0 {
		return ""
	}

	p := objc.Send[uintptr](nsString, objc.RegisterName("UTF8String"))
	if p == 0 {
		return ""
	}

	n := 0
	for cBytes(p+uintptr(n), 1)[0] != 0 {
		n++
	}

	return string(cBytes(p, n))
}

// PickScreen shows the system's picker. Once the user chooses, the capture starts and onFrame gets every new
// picture (on a capture thread; it must not keep the picture without retaining it), and onSound its sound when
// opt.Sound (on another thread): a window's app, or the whole computer's for a display, never this app's own.
// onEnd is called once: when the picker is cancelled (ErrPickerCancelled), the capture fails, or the system stops
// it. Any thread.
func PickScreen(opt ScreenOptions, onFrame func(Picture), onSound func([]int16), onEnd func(error)) (*ScreenCapture, error) {
	if err := loadSC(); err != nil {
		return nil, err
	}

	c := &ScreenCapture{opt: opt, onFrame: onFrame, onSound: onSound, onEnd: onEnd}

	sc.mu.Lock()
	if old := sc.picking; old != nil {
		sc.picking = nil
		sc.mu.Unlock()
		old.end(ErrPickerCancelled)
		sc.mu.Lock()
	}
	sc.picking = c
	sc.mu.Unlock()

	mode := 1 << 4 // SCContentSharingPickerModeSingleDisplay
	if opt.Window {
		mode = 1 << 0 // SingleWindow
	}

	onMain(func() {
		cfg := objc.ID(objc.GetClass("SCContentSharingPickerConfiguration")).Send(selAlloc).Send(selInit)
		cfg.Send(objc.RegisterName("setAllowedPickerModes:"), uint(mode))
		cfg.Send(objc.RegisterName("setAllowsChangingSelectedContent:"), false)

		picker := objc.ID(objc.GetClass("SCContentSharingPicker")).Send(objc.RegisterName("sharedPicker"))
		picker.Send(objc.RegisterName("setDefaultConfiguration:"), cfg)
		cfg.Send(selRelease)
		picker.Send(objc.RegisterName("setActive:"), true)
		picker.Send(objc.RegisterName("present"))
	})

	return c, nil
}

// CaptureMainDisplay captures the main display without the picker. It needs the Screen Recording grant (tests).
func CaptureMainDisplay(opt ScreenOptions, onFrame func(Picture), onSound func([]int16), onEnd func(error)) (*ScreenCapture, error) {
	if err := loadSC(); err != nil {
		return nil, err
	}

	found := make(chan objc.ID, 1)
	blk := objc.NewBlock(func(_ objc.Block, content, nsErr objc.ID) {
		if content != 0 {
			content.Send(selRetain)
		}

		found <- content
	})
	defer blk.Release()

	objc.ID(objc.GetClass("SCShareableContent")).Send(
		objc.RegisterName("getShareableContentExcludingDesktopWindows:onScreenWindowsOnly:completionHandler:"), false, true, blk)

	content := <-found
	if content == 0 {
		return nil, errors.New("sem acesso à gravação de tela")
	}
	defer content.Send(selRelease)

	displays := content.Send(objc.RegisterName("displays"))
	if objc.Send[uint](displays, objc.RegisterName("count")) == 0 {
		return nil, errors.New("nenhuma tela")
	}

	display := displays.Send(objc.RegisterName("objectAtIndex:"), uint(0))
	empty := objc.ID(objc.GetClass("NSArray")).Send(objc.RegisterName("array"))
	filter := objc.ID(objc.GetClass("SCContentFilter")).Send(selAlloc).Send(objc.RegisterName("initWithDisplay:excludingWindows:"), display, empty)
	defer filter.Send(selRelease)

	c := &ScreenCapture{opt: opt, onFrame: onFrame, onSound: onSound, onEnd: onEnd}

	return c, c.start(filter)
}

// start makes and starts the stream for a chosen filter.
func (c *ScreenCapture) start(filter objc.ID) error {
	c.mu.Lock()
	ended := c.ended
	c.mu.Unlock()

	if ended {
		return nil
	}

	rect := objc.Send[cgRect](filter, objc.RegisterName("contentRect"))
	scale := float64(objc.Send[float32](filter, objc.RegisterName("pointPixelScale")))
	c.mu.Lock()
	c.srcW, c.srcH = int(rect.W*scale), int(rect.H*scale)
	cfg := c.config()
	c.mu.Unlock()
	defer cfg.Send(selRelease)

	output := objc.ID(sc.clsOutput).Send(selAlloc).Send(selInit)
	delegate := objc.ID(sc.clsDelegate).Send(selAlloc).Send(selInit)
	sc.mu.Lock()
	sc.byObj[output], sc.byObj[delegate] = c, c
	sc.mu.Unlock()

	stream := objc.ID(objc.GetClass("SCStream")).Send(selAlloc).Send(objc.RegisterName("initWithFilter:configuration:delegate:"), filter, cfg, delegate)
	if stream == 0 {
		return errors.New("não foi possível criar a captura da tela")
	}

	var nsErr objc.ID
	if !objc.Send[bool](stream, objc.RegisterName("addStreamOutput:type:sampleHandlerQueue:error:"), output, 0, sc.queue, &nsErr) {
		stream.Send(selRelease)
		return nsError(nsErr, "não foi possível receber a captura da tela")
	}

	// The sound's output is always there: Update turns the sound on and off.
	if !objc.Send[bool](stream, objc.RegisterName("addStreamOutput:type:sampleHandlerQueue:error:"), output, 1, sc.audioQueue, &nsErr) {
		stream.Send(selRelease)
		return nsError(nsErr, "não foi possível receber o som da tela")
	}

	started := objc.NewBlock(func(_ objc.Block, nsErr objc.ID) {
		if nsErr != 0 {
			c.end(nsError(nsErr, "a captura da tela não começou"))
		}
	})
	defer started.Release()

	c.mu.Lock()
	c.stream = stream
	c.mu.Unlock()
	stream.Send(objc.RegisterName("startCaptureWithCompletionHandler:"), started)

	return nil
}

// config is the stream's configuration for the options and the source (c.mu held); release it.
func (c *ScreenCapture) config() objc.ID {
	w, h := fitSize(c.srcW, c.srcH, c.opt.MaxWidth, c.opt.MaxHeight)
	cfg := objc.ID(objc.GetClass("SCStreamConfiguration")).Send(selAlloc).Send(selInit)

	interval := cmTime{Value: 1, Timescale: int32(max(1, c.opt.FPS)), Flags: 1}
	cfg.Send(objc.RegisterName("setWidth:"), uint(w))
	cfg.Send(objc.RegisterName("setHeight:"), uint(h))
	cfg.Send(objc.RegisterName("setMinimumFrameInterval:"), unsafe.Pointer(&interval)) // CMTime by value: a pointer on arm64
	cfg.Send(objc.RegisterName("setPixelFormat:"), uint32(pixel420v))
	cfg.Send(objc.RegisterName("setShowsCursor:"), true)
	cfg.Send(objc.RegisterName("setQueueDepth:"), 5)
	cfg.Send(objc.RegisterName("setCapturesAudio:"), c.opt.Sound)
	cfg.Send(objc.RegisterName("setExcludesCurrentProcessAudio:"), true) // the call itself stays out
	cfg.Send(objc.RegisterName("setSampleRate:"), 48000)
	cfg.Send(objc.RegisterName("setChannelCount:"), 2)

	return cfg
}

// Update changes the size and rate of a capture (Window is kept: it chose the source).
func (c *ScreenCapture) Update(opt ScreenOptions) {
	c.mu.Lock()
	defer c.mu.Unlock()

	opt.Window = c.opt.Window
	c.opt = opt
	if c.stream == 0 {
		return
	}

	cfg := c.config()
	c.stream.Send(objc.RegisterName("updateConfiguration:completionHandler:"), cfg, uintptr(0))
	cfg.Send(selRelease)
}

// Stop ends the capture (or the picking); onEnd is not called for it.
func (c *ScreenCapture) Stop() {
	c.mu.Lock()
	c.ended = true
	stream := c.stream
	c.stream = 0
	c.mu.Unlock()

	sc.mu.Lock()
	if sc.picking == c {
		sc.picking = nil
	}

	for obj, owner := range sc.byObj {
		if owner == c {
			delete(sc.byObj, obj) // its objects stay alive with the stream; their calls now find nothing
		}
	}
	sc.mu.Unlock()

	if stream != 0 {
		stream.Send(objc.RegisterName("stopCaptureWithCompletionHandler:"), uintptr(0))
		stream.Send(selRelease)
	}

	onMain(func() {
		picker := objc.ID(objc.GetClass("SCContentSharingPicker")).Send(objc.RegisterName("sharedPicker"))
		picker.Send(objc.RegisterName("setActive:"), false)
	})
}

// end reports the end once and stops.
func (c *ScreenCapture) end(err error) {
	c.mu.Lock()
	done := c.ended
	c.mu.Unlock()

	if done {
		return
	}

	c.Stop()
	if c.onEnd != nil {
		c.onEnd(err)
	}
}

// Displays: none here; the system's picker asks which display.
func Displays() []Display { return nil }
