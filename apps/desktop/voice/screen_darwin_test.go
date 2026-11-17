package voice

import (
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestCaptureMainDisplay captures the main display for a second and encodes what comes. It needs the Screen
// Recording grant, so it runs only with DISCORDO_SCREEN_TEST=1.
func TestCaptureMainDisplay(t *testing.T) {
	if os.Getenv("DISCORDO_SCREEN_TEST") == "" {
		t.Skip("set DISCORDO_SCREEN_TEST=1 (needs the Screen Recording grant)")
	}

	var mu sync.Mutex
	var frames, aus, sound int
	var w, h int
	var encodeTime time.Duration
	var enc *Encoder
	cpu0 := cpuTime()

	c, err := CaptureMainDisplay(ScreenOptions{MaxWidth: 1920, MaxHeight: 1080, FPS: 30, Sound: true}, func(p Picture) {
		mu.Lock()
		defer mu.Unlock()

		frames++
		w, h = p.Size()
		if enc == nil {
			enc, _ = NewEncoder(w, h, 30, 6_000_000, func([]byte, bool) { aus++ })
		}

		start := time.Now()
		_ = enc.Encode(p)
		encodeTime += time.Since(start)
	}, func(pcm []int16) {
		mu.Lock()
		sound += len(pcm) / 2
		mu.Unlock()
	}, func(err error) { t.Errorf("ended: %v", err) })
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Second)
	cpu := cpuTime() - cpu0

	// A lower quality while running: the next pictures are smaller.
	c.Update(ScreenOptions{MaxWidth: 1280, MaxHeight: 720, FPS: 30, Sound: true})
	time.Sleep(700 * time.Millisecond)
	c.Stop()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if enc != nil {
		enc.Flush()
		enc.Close()
	}

	if frames == 0 || w == 0 || w%2 != 0 || h%2 != 0 || w > 1280 || h > 720 {
		t.Fatalf("%d frames of %dx%d", frames, w, h)
	}

	// The sound comes (silence included) at 48 kHz: about 2.7 s of it.
	if sound < 48000 {
		t.Errorf("%d sound frames in 2.7 s", sound)
	}

	t.Logf("%d frames of %dx%d in 2 s, %d access units, encode %v/frame, %d sound frames, process CPU %.0f%% of a core",
		frames, w, h, aus, encodeTime/time.Duration(frames), sound, cpu.Seconds()/2*100)
}

// cpuTime is the CPU this process has used, user plus system.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)

	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
