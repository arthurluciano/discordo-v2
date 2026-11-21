//go:build windows

package voice

// Windows screen capture: GDI copies the main display (with the cursor) into memory at the chosen rate, and Go
// converts it to NV12 at the chosen size.
//
// shortcut: GDI is a CPU copy of the whole display, fine for 1080p at 30 fps on today's machines; a window, the
// screen's sound (WASAPI loopback) and other displays are not offered. Windows.Graphics.Capture gives all three.

import (
	"errors"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	getDC            = user32.NewProc("GetDC")
	releaseDC        = user32.NewProc("ReleaseDC")
	getSystemMetrics = user32.NewProc("GetSystemMetrics")
	getCursorInfo    = user32.NewProc("GetCursorInfo")
	getIconInfo      = user32.NewProc("GetIconInfo")
	drawIconEx       = user32.NewProc("DrawIconEx")
	enumMonitors     = user32.NewProc("EnumDisplayMonitors")
	getMonitorInfo   = user32.NewProc("GetMonitorInfoW")

	createCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	createDIBSection   = gdi32.NewProc("CreateDIBSection")
	selectObject       = gdi32.NewProc("SelectObject")
	deleteObject       = gdi32.NewProc("DeleteObject")
	deleteDC           = gdi32.NewProc("DeleteDC")
	bitBlt             = gdi32.NewProc("BitBlt")
)

var errNoWindowShare = errors.New("compartilhar uma janela ainda não funciona no Windows: escolha Tela")

type ScreenCapture struct {
	opt  atomic.Pointer[ScreenOptions]
	stop chan struct{}
	once sync.Once
}

// monitorInfo is MONITORINFOEXW.
type monitorInfo struct {
	size       uint32
	rect, work [4]int32 // left, top, right, bottom in the virtual screen
	flags      uint32   // MONITORINFOF_PRIMARY 1
	device     [32]uint16
}

var monitorsFound []monitorInfo // EnumDisplayMonitors' callback fills it (under monitorsMu)

var (
	monitorsMu sync.Mutex
	monitorCB  = syscall.NewCallback(func(h, _, _, _ uintptr) uintptr {
		mi := monitorInfo{}
		mi.size = uint32(unsafe.Sizeof(mi))
		if ok, _, _ := getMonitorInfo.Call(h, uintptr(unsafe.Pointer(&mi))); ok != 0 {
			monitorsFound = append(monitorsFound, mi)
		}

		return 1
	})
)

func monitors() []monitorInfo {
	monitorsMu.Lock()
	defer monitorsMu.Unlock()

	monitorsFound = nil
	enumMonitors.Call(0, 0, monitorCB, 0)

	return monitorsFound
}

// Displays are the monitors, the main one first, for the share dialog to choose from.
func Displays() []Display {
	var list []Display
	for i, m := range monitors() {
		d := Display{ID: windows.UTF16ToString(m.device[:]), W: int(m.rect[2] - m.rect[0]), H: int(m.rect[3] - m.rect[1]), Main: m.flags&1 != 0}
		d.Name = "Tela " + strconv.Itoa(i+1)
		if d.Main {
			list = append([]Display{d}, list...)
		} else {
			list = append(list, d)
		}
	}

	return list
}

// PickScreen captures opt.Display (the main one when ""): Windows has no picker here, and windows are not offered yet.
func PickScreen(opt ScreenOptions, onFrame func(Picture), onSound func([]int16), onEnd func(error)) (*ScreenCapture, error) {
	if opt.Window {
		return nil, errNoWindowShare
	}

	return CaptureMainDisplay(opt, onFrame, onSound, onEnd)
}

// CaptureMainDisplay captures the main display; onFrame gets every picture (on the capture thread; it retains what
// it keeps), and onEnd is called if the capture cannot start.
func CaptureMainDisplay(opt ScreenOptions, onFrame func(Picture), onSound func([]int16), onEnd func(error)) (*ScreenCapture, error) {
	c := &ScreenCapture{stop: make(chan struct{})}
	c.opt.Store(&opt)

	started := make(chan error, 1)
	go c.run(onFrame, started)
	if err := <-started; err != nil {
		return nil, err
	}

	return c, nil
}

func (c *ScreenCapture) Update(opt ScreenOptions) { c.opt.Store(&opt) }

func (c *ScreenCapture) Stop() { c.once.Do(func() { close(c.stop) }) }

func (c *ScreenCapture) run(onFrame func(Picture), started chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// The display's place in the virtual screen: the chosen one, else the main one at the origin.
	sw, _, _ := getSystemMetrics.Call(0) // SM_CXSCREEN
	sh, _, _ := getSystemMetrics.Call(1) // SM_CYSCREEN
	x0, y0, w, h := 0, 0, int(sw), int(sh)
	if id := c.opt.Load().Display; id != "" {
		for _, m := range monitors() {
			if windows.UTF16ToString(m.device[:]) == id {
				x0, y0, w, h = int(m.rect[0]), int(m.rect[1]), int(m.rect[2]-m.rect[0]), int(m.rect[3]-m.rect[1])
			}
		}
	}

	screen, _, _ := getDC.Call(0)
	mem, _, _ := createCompatibleDC.Call(screen)

	// A top-down 32-bit DIB: BGRA rows, w*4 bytes apart.
	info := struct {
		size                 uint32
		w, h                 int32
		planes, bits         uint16
		compression, imgSize uint32
		xppm, yppm           int32
		used, important      uint32
		colors               [4]byte
	}{size: 40, w: int32(w), h: -int32(h), planes: 1, bits: 32}
	var bits uintptr
	dib, _, _ := createDIBSection.Call(mem, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)

	if screen == 0 || mem == 0 || dib == 0 || bits == 0 || w <= 0 || h <= 0 {
		started <- errors.New("não foi possível capturar a tela")
		if dib != 0 {
			deleteObject.Call(dib)
		}

		if mem != 0 {
			deleteDC.Call(mem)
		}

		releaseDC.Call(0, screen)

		return
	}

	old, _, _ := selectObject.Call(mem, dib)
	defer func() {
		selectObject.Call(mem, old)
		deleteObject.Call(dib)
		deleteDC.Call(mem)
		releaseDC.Call(0, screen)
	}()

	started <- nil
	pix := unsafe.Slice((*byte)(ptr(bits)), w*h*4)

	for {
		opt := *c.opt.Load()
		next := time.Now().Add(time.Second / time.Duration(max(1, opt.FPS)))

		if ok, _, _ := bitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), screen, uintptr(x0), uintptr(y0), 0x00CC0020); ok != 0 { // SRCCOPY
			drawCursor(mem, x0, y0)
			ow, oh := fitSize(w, h, opt.MaxWidth, opt.MaxHeight)
			if p, err := NewPicture(ow, oh, func(y, uv []byte, ys, uvs int) { bgraToNV12(pix, w, h, y, uv, ow, oh) }); err == nil {
				onFrame(p)
				p.Release()
			}
		} // a failed copy (the secure desktop is up): skip the frame

		select {
		case <-c.stop:
			return
		case <-time.After(time.Until(next)):
		}
	}
}

// drawCursor draws the mouse pointer where it is, as viewers expect to see it.
func drawCursor(dc uintptr, x0, y0 int) {
	ci := struct {
		size, flags uint32
		cursor      uintptr
		x, y        int32
	}{}
	ci.size = uint32(unsafe.Sizeof(ci))
	if ok, _, _ := getCursorInfo.Call(uintptr(unsafe.Pointer(&ci))); ok == 0 || ci.flags&1 == 0 { // CURSOR_SHOWING
		return
	}

	ii := struct {
		icon        int32
		hotX, hotY  uint32
		mask, color uintptr
	}{}
	if ok, _, _ := getIconInfo.Call(ci.cursor, uintptr(unsafe.Pointer(&ii))); ok != 0 {
		deleteObject.Call(ii.mask)
		if ii.color != 0 {
			deleteObject.Call(ii.color)
		}
	}

	x, y := ci.x-int32(ii.hotX)-int32(x0), ci.y-int32(ii.hotY)-int32(y0)
	drawIconEx.Call(dc, uintptr(x), uintptr(y), ci.cursor, 0, 0, 0, 0, 3) // DI_NORMAL
}
