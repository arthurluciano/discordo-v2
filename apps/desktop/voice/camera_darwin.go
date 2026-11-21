package voice

// macOS camera: AVFoundation's capture session through purego's Objective-C runtime. The output asks for
// IOSurface-backed '420v' pixel buffers at the size we send, so each frame is a Picture as it comes: the encoder
// takes it and the window draws it, with no copy.

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// CameraCapture is a camera running.
type CameraCapture struct {
	onFrame func(Picture)

	mu       sync.Mutex
	session  objc.ID
	output   objc.ID
	delegate objc.ID
}

var cam struct {
	once sync.Once
	err  error

	mediaVideo      objc.ID // AVMediaTypeVideo
	preset720       objc.ID // AVCaptureSessionPreset1280x720
	preset480       objc.ID // AVCaptureSessionPreset640x480
	kWidth, kHeight uintptr
	queue           uintptr
	clsDelegate     objc.Class

	mu    sync.Mutex
	byObj map[objc.ID]*CameraCapture
}

func loadCam() error {
	cam.once.Do(func() { cam.err = loadCamOnce() })
	return cam.err
}

func loadCamOnce() (err error) {
	if err := loadVT(); err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil { // RegisterLibFunc panics on a missing symbol
			err = errors.New("AVFoundation indisponível")
		}
	}()

	libs := map[string]uintptr{}
	for _, name := range []string{"Foundation", "AVFoundation", "CoreVideo"} {
		h, err := purego.Dlopen("/System/Library/Frameworks/"+name+".framework/"+name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return err
		}

		libs[name] = h
	}

	sym := func(lib uintptr, name string) uintptr {
		p, err := purego.Dlsym(lib, name)
		if err != nil {
			panic(name)
		}

		return deref(p)
	}

	av, cv := libs["AVFoundation"], libs["CoreVideo"]
	cam.mediaVideo = objc.ID(sym(av, "AVMediaTypeVideo"))
	cam.preset720 = objc.ID(sym(av, "AVCaptureSessionPreset1280x720"))
	cam.preset480 = objc.ID(sym(av, "AVCaptureSessionPreset640x480"))
	cam.kWidth, cam.kHeight = sym(cv, "kCVPixelBufferWidthKey"), sym(cv, "kCVPixelBufferHeightKey")

	sys, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW)
	if err != nil {
		return err
	}

	var queueCreate func(label string, attr uintptr) uintptr
	purego.RegisterLibFunc(&queueCreate, sys, "dispatch_queue_create")
	cam.queue = queueCreate("br.com.discordo.camera", 0)
	cam.byObj = map[objc.ID]*CameraCapture{}

	cam.clsDelegate, err = objc.RegisterClass("DiscordoCameraOutput", objc.GetClass("NSObject"),
		protocols("AVCaptureVideoDataOutputSampleBufferDelegate"), nil, []objc.MethodDef{{
			Cmd: objc.RegisterName("captureOutput:didOutputSampleBuffer:fromConnection:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID, sample uintptr, _ objc.ID) {
				cam.mu.Lock()
				c := cam.byObj[self]
				cam.mu.Unlock()

				if c == nil || sample == 0 {
					return
				}

				if pb := vt.sampleImage(sample); pb != 0 {
					c.onFrame(Picture(pb))
				}
			},
		}})

	return err
}

// nsString makes an autoreleased NSString.
func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	id := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), unsafe.Pointer(&b[0]))
	runtime.KeepAlive(b)

	return id
}

// Cameras lists the cameras the system has.
func Cameras() ([]Camera, error) {
	if err := loadCam(); err != nil {
		return nil, err
	}

	list := objc.ID(objc.GetClass("AVCaptureDevice")).Send(objc.RegisterName("devicesWithMediaType:"), cam.mediaVideo)
	n := objc.Send[uint](list, objc.RegisterName("count"))

	cams := make([]Camera, 0, n)
	for i := range n {
		dev := list.Send(objc.RegisterName("objectAtIndex:"), i)
		cams = append(cams, Camera{
			ID:   goString(dev.Send(objc.RegisterName("uniqueID"))),
			Name: goString(dev.Send(objc.RegisterName("localizedName"))),
		})
	}

	return cams, nil
}

// StartCamera starts the camera id ("" or a missing one: the system's default); onFrame gets every picture on a
// capture thread (it retains what it keeps). It blocks until the camera runs: not on the UI thread.
func StartCamera(id string, onFrame func(Picture), onEnd func(error)) (*CameraCapture, error) {
	if err := loadCam(); err != nil {
		return nil, err
	}

	devClass := objc.ID(objc.GetClass("AVCaptureDevice"))
	var dev objc.ID
	if id != "" {
		dev = devClass.Send(objc.RegisterName("deviceWithUniqueID:"), nsString(id))
	}

	if dev == 0 {
		dev = devClass.Send(objc.RegisterName("defaultDeviceWithMediaType:"), cam.mediaVideo)
	}

	if dev == 0 {
		return nil, errNoCamera
	}

	nsErr := new(objc.ID)
	input := objc.ID(objc.GetClass("AVCaptureDeviceInput")).Send(objc.RegisterName("deviceInputWithDevice:error:"), dev, unsafe.Pointer(nsErr))
	if input == 0 {
		return nil, nsError(*nsErr, "não foi possível abrir a câmera")
	}

	session := objc.ID(objc.GetClass("AVCaptureSession")).Send(selAlloc).Send(selInit)
	session.Send(objc.RegisterName("beginConfiguration"))

	// 16:9 when the camera has 720p, else its 4:3 at 640×480.
	w, h := CameraWidth, CameraHeight
	if objc.Send[bool](session, objc.RegisterName("canSetSessionPreset:"), cam.preset720) {
		session.Send(objc.RegisterName("setSessionPreset:"), cam.preset720)
	} else if objc.Send[bool](session, objc.RegisterName("canSetSessionPreset:"), cam.preset480) {
		session.Send(objc.RegisterName("setSessionPreset:"), cam.preset480)
		h = CameraWidth * 3 / 4
	}

	if !objc.Send[bool](session, objc.RegisterName("canAddInput:"), input) {
		session.Send(selRelease)
		return nil, errors.New("a câmera está em uso ou indisponível")
	}

	session.Send(objc.RegisterName("addInput:"), input)

	output := objc.ID(objc.GetClass("AVCaptureVideoDataOutput")).Send(selAlloc).Send(selInit)
	empty := cfDict(nil, nil)
	settings := cfDict([]uintptr{vt.kPixelFormat, vt.kIOSurface, cam.kWidth, cam.kHeight},
		[]uintptr{cfInt(pixel420v), empty, cfInt(int32(w)), cfInt(int32(h))})
	output.Send(objc.RegisterName("setVideoSettings:"), objc.ID(settings))
	vt.cfRelease(settings)
	vt.cfRelease(empty)
	output.Send(objc.RegisterName("setAlwaysDiscardsLateVideoFrames:"), true)

	c := &CameraCapture{onFrame: onFrame, session: session, output: output}
	c.delegate = objc.ID(cam.clsDelegate).Send(selAlloc).Send(selInit)
	cam.mu.Lock()
	cam.byObj[c.delegate] = c
	cam.mu.Unlock()

	output.Send(objc.RegisterName("setSampleBufferDelegate:queue:"), c.delegate, cam.queue)
	if !objc.Send[bool](session, objc.RegisterName("canAddOutput:"), output) {
		c.Stop()
		return nil, errors.New("a câmera não entrega vídeo")
	}

	session.Send(objc.RegisterName("addOutput:"), output)
	session.Send(objc.RegisterName("commitConfiguration"))
	session.Send(objc.RegisterName("startRunning"))

	// shortcut: a camera unplugged mid-call just stops sending pictures (onEnd is never called); observe
	// AVCaptureSessionRuntimeErrorNotification if that needs saying.
	_ = onEnd

	return c, nil
}

// Stop stops the camera; no picture comes after it returns.
func (c *CameraCapture) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.session == 0 {
		return
	}

	c.session.Send(objc.RegisterName("stopRunning"))
	c.output.Send(objc.RegisterName("setSampleBufferDelegate:queue:"), objc.ID(0), uintptr(0))

	cam.mu.Lock()
	delete(cam.byObj, c.delegate)
	cam.mu.Unlock()

	c.delegate.Send(selRelease)
	c.output.Send(selRelease)
	c.session.Send(selRelease)
	c.session, c.output, c.delegate = 0, 0, 0
}
