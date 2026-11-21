package voice

// Pictures to and from RGB where the system does not convert them (Windows).

// bgraToNV12 scales a sw×sh BGRA image to dw×dh (nearest pixel) in BT.601 video range: luma per pixel, chroma per
// 2×2 block from the block's average.
func bgraToNV12(src []byte, sw, sh int, y, uv []byte, dw, dh int) {
	for row := 0; row < dh; row += 2 {
		for col := 0; col < dw; col += 2 {
			var rs, gs, bs int
			for dy := range 2 {
				sy := (row + dy) * sh / dh
				for dx := range 2 {
					sx := (col + dx) * sw / dw
					i := (sy*sw + sx) * 4
					b, g, r := int(src[i]), int(src[i+1]), int(src[i+2])
					y[(row+dy)*dw+col+dx] = byte((66*r+129*g+25*b+128)>>8 + 16)
					rs, gs, bs = rs+r, gs+g, bs+b
				}
			}

			r, g, b := rs/4, gs/4, bs/4
			o := row/2*dw + col
			uv[o] = byte((-38*r-74*g+112*b+128)>>8 + 128)
			uv[o+1] = byte((112*r-94*g-18*b+128)>>8 + 128)
		}
	}
}

// NV12ToRGBA converts a w×h picture in BT.601 video range to RGBA (Windows draws pictures this way).
func NV12ToRGBA(y, uv []byte, ys, uvs, w, h int, out []byte) {
	for row := range h {
		for col := range w {
			c := 298 * (int(y[row*ys+col]) - 16)
			o := row/2*uvs + col&^1
			u, v := int(uv[o])-128, int(uv[o+1])-128
			i := (row*w + col) * 4
			out[i] = clamp8((c + 409*v + 128) >> 8)
			out[i+1] = clamp8((c - 100*u - 208*v + 128) >> 8)
			out[i+2] = clamp8((c + 516*u + 128) >> 8)
			out[i+3] = 255
		}
	}
}

func clamp8(v int) byte { return byte(min(255, max(0, v))) }
