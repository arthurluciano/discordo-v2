package voice

import "testing"

func TestFitSize(t *testing.T) {
	for _, c := range []struct{ sw, sh, mw, mh, w, h int }{
		{3024, 1964, 1920, 1080, 1662, 1080},
		{1280, 720, 1920, 1080, 1280, 720},
		{1001, 501, 1920, 1080, 1000, 500},
		{0, 0, 1280, 720, 1280, 720},
	} {
		if w, h := fitSize(c.sw, c.sh, c.mw, c.mh); w != c.w || h != c.h {
			t.Errorf("fitSize(%d, %d, %d, %d) = %dx%d, want %dx%d", c.sw, c.sh, c.mw, c.mh, w, h, c.w, c.h)
		}
	}
}
