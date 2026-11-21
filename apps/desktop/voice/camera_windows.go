//go:build windows

package voice

// Windows camera: Media Foundation's video capture sources, read synchronously by a source reader that converts to
// NV12 (advanced video processing: MJPG and YUY2 webcams too), on a thread of the multithreaded apartment.

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	mfdll = windows.NewLazySystemDLL("mf.dll")
	mfrw  = windows.NewLazySystemDLL("mfreadwrite.dll")

	mfEnumDeviceSources = mfdll.NewProc("MFEnumDeviceSources")
	mfCreateAttributes  = mfplat.NewProc("MFCreateAttributes")
	mfCreateReader      = mfrw.NewProc("MFCreateSourceReaderFromMediaSource")
	coTaskMemFree       = ole32.NewProc("CoTaskMemFree")
)

// GUIDs (mingw-w64 mfidl.h 7355-7365, mfreadwrite.h 187, mfidl.h 2782).
var (
	mfDevSourceType    = mustGUID("c60ac5fe-252a-478f-a0ef-bc8fa5f7cad3") // MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE
	mfDevSourceVidcap  = mustGUID("8ac3587a-4ae7-42d8-99e0-0a6013eef90f") // ..._SOURCE_TYPE_VIDCAP_GUID
	mfDevSourceName    = mustGUID("60d0e559-52f8-4fa2-bbce-acdb34a8ec01") // ..._FRIENDLY_NAME
	mfDevSourceLink    = mustGUID("58f0aad8-22bf-4f8a-bb3d-d2c4978c6e2f") // ..._SOURCE_TYPE_VIDCAP_SYMBOLIC_LINK
	mfReaderAdvancedVP = mustGUID("0f81da2c-b537-4672-a8b2-a681b17307a3") // MF_SOURCE_READER_ENABLE_ADVANCED_VIDEO_PROCESSING
	iidMFMediaSource   = mustGUID("279a808d-aec7-40c8-9c6b-a6b492c78a66")
)

// Vtable slots (mingw-w64 mfobjects.h IMFAttributesVtbl, IMFActivateVtbl; mfreadwrite.h IMFSourceReaderVtbl).
const (
	vtAttrGetAllocatedString = 13
	vtActivateObject         = 33
	vtShutdownObject         = 34
	vtReaderGetCurrentType   = 6
	vtReaderSetCurrentType   = 7
	vtReaderReadSample       = 9

	firstVideoStream  = 0xfffffffc // MF_SOURCE_READER_FIRST_VIDEO_STREAM
	readerError       = 0x1        // MF_SOURCE_READERF_ERROR
	readerEnd         = 0x2        // MF_SOURCE_READERF_ENDOFSTREAM
	readerTypeChanged = 0x20       // MF_SOURCE_READERF_CURRENTMEDIATYPECHANGED
)

type CameraCapture struct {
	stop chan struct{}
}

// cameraSources are the system's cameras' activation objects (release each), on a COM thread.
func cameraSources() ([]uintptr, error) {
	var attrs uintptr
	if r, _, _ := mfCreateAttributes.Call(uintptr(unsafe.Pointer(&attrs)), 1); int32(r) < 0 {
		return nil, hrErr("MFCreateAttributes", int32(r))
	}
	defer release(attrs)

	com(attrs, vtAttrSetGUID, uintptr(unsafe.Pointer(&mfDevSourceType)), uintptr(unsafe.Pointer(&mfDevSourceVidcap)))

	var arr uintptr
	var n uint32
	if r, _, _ := mfEnumDeviceSources.Call(attrs, uintptr(unsafe.Pointer(&arr)), uintptr(unsafe.Pointer(&n))); int32(r) < 0 {
		return nil, hrErr("MFEnumDeviceSources", int32(r))
	}

	if arr == 0 {
		return nil, nil
	}

	acts := append([]uintptr(nil), unsafe.Slice((*uintptr)(ptr(arr)), n)...)
	coTaskMemFree.Call(arr)

	return acts, nil
}

// attrString reads a string attribute.
func attrString(obj uintptr, key *windows.GUID) string {
	var p uintptr
	var n uint32
	if com(obj, vtAttrGetAllocatedString, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&n))) < 0 || p == 0 {
		return ""
	}
	defer coTaskMemFree.Call(p)

	return windows.UTF16PtrToString((*uint16)(ptr(p)))
}

// Cameras lists the cameras the system has; the ID is the device's symbolic link.
func Cameras() ([]Camera, error) {
	if err := mfOnce(); err != nil {
		return nil, err
	}

	var cams []Camera
	var err error
	w := newWorker()
	w.do(func() {
		var acts []uintptr
		if acts, err = cameraSources(); err != nil {
			return
		}

		for _, a := range acts {
			cams = append(cams, Camera{ID: attrString(a, &mfDevSourceLink), Name: attrString(a, &mfDevSourceName)})
			release(a)
		}
	})
	close(w.jobs)

	return cams, err
}

// StartCamera starts the camera id ("" or a missing one: the first); onFrame gets every picture on the capture
// thread (it retains what it keeps), and onEnd is called if the camera stops by itself.
func StartCamera(id string, onFrame func(Picture), onEnd func(error)) (*CameraCapture, error) {
	if err := mfOnce(); err != nil {
		return nil, err
	}

	c := &CameraCapture{stop: make(chan struct{})}
	started := make(chan error, 1)
	go c.run(id, onFrame, onEnd, started)
	if err := <-started; err != nil {
		return nil, err
	}

	return c, nil
}

// Stop stops the camera; a picture being read may still come.
func (c *CameraCapture) Stop() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
}

func (c *CameraCapture) run(id string, onFrame func(Picture), onEnd func(error), started chan<- error) {
	runtime.LockOSThread() // never unlocked: the thread ends with the goroutine
	coInitializeEx.Call(0, 0)

	acts, err := cameraSources()
	if err != nil {
		started <- err
		return
	}

	defer func() {
		for _, a := range acts {
			release(a)
		}
	}()

	if len(acts) == 0 {
		started <- errNoCamera
		return
	}

	act := acts[0]
	for _, a := range acts {
		if id != "" && attrString(a, &mfDevSourceLink) == id {
			act = a
		}
	}

	var source uintptr
	if hr := com(act, vtActivateObject, uintptr(unsafe.Pointer(&iidMFMediaSource)), uintptr(unsafe.Pointer(&source))); hr < 0 {
		started <- errors.New("a câmera está em uso ou indisponível")
		return
	}

	defer func() {
		com(act, vtShutdownObject)
		release(source)
	}()

	var attrs, reader uintptr
	if r, _, _ := mfCreateAttributes.Call(uintptr(unsafe.Pointer(&attrs)), 1); int32(r) < 0 {
		started <- hrErr("MFCreateAttributes", int32(r))
		return
	}

	com(attrs, vtAttrSetUINT32, uintptr(unsafe.Pointer(&mfReaderAdvancedVP)), 1)
	r, _, _ := mfCreateReader.Call(source, attrs, uintptr(unsafe.Pointer(&reader)))
	release(attrs)
	if int32(r) < 0 {
		started <- hrErr("MFCreateSourceReaderFromMediaSource", int32(r))
		return
	}
	defer release(reader)

	nv12, err := mediaType(&mfVideoFormatNV12, nil, nil)
	if err != nil {
		started <- err
		return
	}

	hr := com(reader, vtReaderSetCurrentType, firstVideoStream, 0, nv12)
	release(nv12)
	if hr < 0 {
		started <- hrErr("SetCurrentMediaType", hr)
		return
	}

	w, h, stride := readerSize(reader)
	started <- nil

	for {
		select {
		case <-c.stop:
			return
		default:
		}

		var index, flags uint32
		var ts int64
		var sample uintptr
		hr := com(reader, vtReaderReadSample, firstVideoStream, 0, uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&flags)),
			uintptr(unsafe.Pointer(&ts)), uintptr(unsafe.Pointer(&sample)))

		switch {
		case hr < 0 || flags&readerError != 0:
			release(sample)
			if onEnd != nil {
				onEnd(errors.New("a câmera parou"))
			}

			return
		case flags&readerEnd != 0:
			release(sample)
			if onEnd != nil {
				onEnd(errors.New("a câmera foi desconectada"))
			}

			return
		case flags&readerTypeChanged != 0:
			w, h, stride = readerSize(reader)
		}

		if sample == 0 {
			continue
		}

		_ = sampleBytes(sample, func(b []byte) {
			if w <= 0 || h <= 0 || len(b) < stride*h*3/2 {
				return
			}

			ow, oh := fitSize(w, h, CameraWidth, CameraHeight)
			p, err := NewPicture(ow, oh, func(y, uv []byte, _, _ int) {
				scaleNV12(b, b[stride*h:], stride, w, h, y, uv, ow, oh)
			})
			if err == nil {
				onFrame(p)
				p.Release()
			}
		})
		release(sample)
	}
}

// readerSize is the reader's output: its size and row stride.
func readerSize(reader uintptr) (w, h, stride int) {
	var mt uintptr
	if com(reader, vtReaderGetCurrentType, firstVideoStream, uintptr(unsafe.Pointer(&mt))) < 0 {
		return 0, 0, 0
	}
	defer release(mt)

	var size uint64
	com(mt, vtAttrGetUINT64, uintptr(unsafe.Pointer(&mfMTFrameSize)), uintptr(unsafe.Pointer(&size)))
	w, h = int(size>>32), int(uint32(size))

	var s uint32
	stride = w
	if com(mt, vtAttrGetUINT32, uintptr(unsafe.Pointer(&mfMTDefaultStride)), uintptr(unsafe.Pointer(&s))) >= 0 && int32(s) >= int32(w) {
		stride = int(int32(s))
	}

	return w, h, stride
}
