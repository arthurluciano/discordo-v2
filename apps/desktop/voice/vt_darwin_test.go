package voice

import (
	"sync"
	"testing"
	"time"
)

// grey makes a picture whose luma is v, with a lighter square that moves with the frame number.
func grey(t testing.TB, w, h int, v byte, frame int) Picture {
	p, err := NewPicture(w, h, func(y, uv []byte, ys, uvs int) {
		for r := range h {
			row := y[r*ys : r*ys+w]
			for c := range row {
				row[c] = v
				if r/64 == 2 && (c/64) == frame%(w/64) {
					row[c] = 235
				}
			}
		}

		for i := range uv {
			uv[i] = 128
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	return p
}

// TestVideoToolboxRoundTrip encodes pictures to H.264 and decodes them back: sizes, keyframes, the picture's luma.
func TestVideoToolboxRoundTrip(t *testing.T) {
	for _, size := range [][2]int{{1280, 720}, {1920, 1080}} {
		w, h := size[0], size[1]
		const frames = 60

		var mu sync.Mutex
		var aus [][]byte
		var keys []bool
		enc, err := NewEncoder(w, h, 30, 6_000_000, func(au []byte, key bool) {
			mu.Lock()
			aus = append(aus, append([]byte(nil), au...))
			keys = append(keys, key)
			mu.Unlock()
		})
		if err != nil {
			t.Fatal(err)
		}

		start := time.Now()
		for i := range frames {
			p := grey(t, w, h, 100, i)
			if i == 40 {
				enc.ForceKeyframe()
			}

			if err := enc.Encode(p); err != nil {
				t.Fatal(err)
			}

			p.Release()
		}

		enc.Flush()
		encodeTime := time.Since(start)
		enc.Close()

		if len(aus) != frames || !keys[0] {
			t.Fatalf("%dx%d: %d access units, first key %v", w, h, len(aus), keys[0])
		}

		nKeys, bytes := 0, 0
		for i, k := range keys {
			bytes += len(aus[i])
			if k {
				nKeys++
			}
		}

		if !keys[40] {
			t.Errorf("frame 40 is not the keyframe asked for")
		}

		var pics []Picture
		dec, err := NewDecoder(func(p Picture) {
			mu.Lock()
			pics = append(pics, p)
			mu.Unlock()
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := dec.Decode(aus[1]); err != errNeedKeyframe {
			t.Fatalf("a delta frame first: %v, want errNeedKeyframe", err)
		}

		start = time.Now()
		for _, au := range aus {
			if err := dec.Decode(au); err != nil {
				t.Fatal(err)
			}
		}

		dec.Close()
		decodeTime := time.Since(start)

		if len(pics) != frames {
			t.Fatalf("%d pictures decoded of %d", len(pics), frames)
		}

		gw, gh := pics[0].Size()
		var luma int
		pics[frames-1].Read(func(y, uv []byte, ys, uvs int) { luma = int(y[10*ys+10]) })
		for _, p := range pics {
			p.Release()
		}

		if gw != w || gh != h || luma < 95 || luma > 105 {
			t.Fatalf("decoded %dx%d luma %d, want %dx%d luma ~100", gw, gh, luma, w, h)
		}

		t.Logf("%dx%d: encode %v/frame, decode %v/frame, %d keyframes, %.0f kbit/s at 30 fps",
			w, h, encodeTime/frames, decodeTime/frames, nKeys, float64(bytes*8)/frames*30/1000)
	}
}
