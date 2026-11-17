package voice

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// TestDevices runs against the real audio stack, so only on demand: DISCORDO_AUDIO_TEST=1 (output: plays silence),
// DISCORDO_AUDIO_TEST=mic (also records; the system may ask for the microphone).
func TestDevices(t *testing.T) {
	mode := os.Getenv("DISCORDO_AUDIO_TEST")
	if mode == "" {
		t.Skip("set DISCORDO_AUDIO_TEST=1 to use the real audio devices")
	}

	ins, err := Inputs()
	if err != nil {
		t.Fatal(err)
	}

	outs, err := Outputs()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("inputs %q\noutputs %q", ins, outs)

	var pulls atomic.Int32
	out, err := OpenOutput("", func(pcm []int16) {
		if len(pcm) != Frame {
			t.Errorf("pull of %d samples", len(pcm))
		}

		clear(pcm)
		pulls.Add(1)
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)
	out.Close()
	out.Close()

	// 500 ms is 25 frames, plus the queued ones; allow for a slow start.
	if n := pulls.Load(); n < 15 || n > 40 {
		t.Fatalf("%d pulls in 500 ms", n)
	}

	if mode != "mic" {
		return
	}

	var pushes atomic.Int32
	var loud atomic.Uint32
	in, err := OpenInput("", func(pcm []int16) {
		pushes.Add(1)
		if Level(pcm) > 0 {
			loud.Store(1)
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)
	in.Close()
	t.Logf("%d pushes, signal %v", pushes.Load(), loud.Load() == 1)
	if pushes.Load() < 10 {
		t.Fatalf("%d pushes in 500 ms", pushes.Load())
	}
}
