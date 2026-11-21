package voice

import "testing"

// Scaling NV12 down picks the right luma and keeps chroma pairs together, with padded source rows.
func TestScaleNV12(t *testing.T) {
	sw, sh, stride := 8, 4, 10
	srcY, srcUV := make([]byte, stride*sh), make([]byte, stride*sh/2)
	for r := range sh {
		for c := range sw {
			srcY[r*stride+c] = byte(r*10 + c)
		}
	}

	for r := range sh / 2 {
		for c := 0; c < sw; c += 2 {
			srcUV[r*stride+c], srcUV[r*stride+c+1] = byte(100+r*10+c), byte(200+r*10+c)
		}
	}

	y, uv := make([]byte, 4*2), make([]byte, 4)
	scaleNV12(srcY, srcUV, stride, sw, sh, y, uv, 4, 2)

	if want := []byte{0, 2, 4, 6, 20, 22, 24, 26}; string(y) != string(want) {
		t.Fatalf("luma %v, want %v", y, want)
	}

	if want := []byte{100, 200, 104, 204}; string(uv) != string(want) {
		t.Fatalf("chroma %v, want %v", uv, want)
	}
}
