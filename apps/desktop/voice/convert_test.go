package voice

import "testing"

// A colour goes to NV12 and back about the same, scaled down or not.
func TestConvertRoundTrip(t *testing.T) {
	for _, c := range [][3]int{{255, 0, 0}, {0, 200, 40}, {20, 30, 220}, {128, 128, 128}, {0, 0, 0}, {255, 255, 255}} {
		sw, sh, dw, dh := 8, 6, 4, 2
		src := make([]byte, sw*sh*4)
		for i := 0; i < len(src); i += 4 {
			src[i], src[i+1], src[i+2], src[i+3] = byte(c[2]), byte(c[1]), byte(c[0]), 255
		}

		y, uv := make([]byte, dw*dh), make([]byte, dw*dh/2)
		bgraToNV12(src, sw, sh, y, uv, dw, dh)

		out := make([]byte, dw*dh*4)
		NV12ToRGBA(y, uv, dw, dw, dw, dh, out)
		for i := 0; i < len(out); i += 4 {
			for k := range 3 {
				if d := int(out[i+k]) - c[k]; d < -6 || d > 6 {
					t.Fatalf("%v came back as %v", c, out[i:i+4])
				}
			}
		}
	}
}
