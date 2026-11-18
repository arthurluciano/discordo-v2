package world

// Skins: characters anyone can wear in place of the one made up from their ID. Each is a made-up person's
// colours plus a few rectangles of its own (a hat, a mask, a ponytail), drawn here: no picture is copied.

type Skin struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Zone string `json:"zone"` // the room it belongs to, to group them when choosing
	look look
}

// Skins in the order they are offered.
var Skins = []Skin{
	{ID: "jett", Name: "Jett", look: look{skin: rgb(0xffdcb8), hair: rgb(0xe9eff7), shirt: rgb(0x4f9fd9), pants: rgb(0x26324d),
		extra: func(r pen, at int) {
			white := rgb(0xf5f8fc)

			switch at {
			case down:
				r(11, -1, 3, 2, rgb(0xe9eff7))
				r(4, 10, 8, 1, white)
				r(3, 13, 1, 2, white)
				r(12, 13, 1, 2, white)
			case up:
				r(6, -2, 4, 3, rgb(0xe9eff7))
				r(4, 10, 8, 1, white)
			default:
				r(2, 0, 2, 6, rgb(0xe9eff7))
				r(5, 10, 6, 1, white)
			}
		}}},

	{ID: "phoenix", Name: "Phoenix", look: look{skin: rgb(0x7a4a2a), hair: rgb(0x1b1c26), shirt: rgb(0xf2f3f5), pants: rgb(0x1b1c26),
		extra: func(r pen, at int) {
			fire := rgb(0xff8a1f)

			if at == right {
				r(4, 0, 9, 1, fire)
				r(6, -1, 5, 1, rgb(0xffc24a))
				r(5, 13, 6, 1, fire)

				return
			}

			r(3, 0, 10, 1, fire)
			r(5, -1, 6, 1, rgb(0xffc24a))
			r(4, 13, 8, 1, fire)
			r(3, 10, 1, 5, ink)
			r(12, 10, 1, 5, ink)
		}}},

	{ID: "sage", Name: "Sage", look: look{skin: rgb(0xffe0c2), hair: rgb(0x1b1c26), shirt: rgb(0xe6f4f1), pants: rgb(0x2f8f86), style: 1,
		extra: func(r pen, at int) {
			teal := rgb(0x3fc5b5)

			switch at {
			case down:
				r(4, 12, 8, 1, teal)
				r(7, 10, 2, 1, teal)
			case up:
				r(4, 12, 8, 1, teal)
				r(7, 9, 2, 6, rgb(0x1b1c26))
			default:
				r(5, 12, 6, 1, teal)
			}
		}}},

	{ID: "reyna", Name: "Reyna", look: look{skin: rgb(0xc98a5e), hair: rgb(0x2c1a3d), shirt: rgb(0x6b2a8c), pants: rgb(0x241630), style: 1,
		eyes: rgb(0xff5ad1),
		extra: func(r pen, at int) {
			glow := rgb(0xff5ad1)

			switch at {
			case down:
				r(7, 11, 2, 2, glow)
				r(3, 10, 2, 1, glow)
				r(11, 10, 2, 1, glow)
			case up:
				r(3, 10, 10, 1, glow)
			default:
				r(5, 10, 6, 1, glow)
			}
		}}},

	{ID: "omen", Name: "Omen", look: look{skin: rgb(0x17131f), hair: rgb(0x2c2647), shirt: rgb(0x2c2647), pants: rgb(0x1b1828), style: 1,
		eyes: rgb(0x6fd6ff), plain: true,
		extra: func(r pen, at int) {
			cloth := rgb(0x5b4a9e)

			switch at {
			case down:
				r(7, 7, 2, 1, rgb(0x6fd6ff))
				r(3, 10, 10, 1, cloth)
				r(4, 14, 8, 1, cloth)
			case up:
				r(3, 10, 10, 1, cloth)
			default:
				r(11, 8, 1, 1, rgb(0x17131f))
				r(5, 10, 6, 1, cloth)
			}
		}}},

	{ID: "viper", Name: "Viper", look: look{skin: rgb(0xf1d3b8), hair: rgb(0x14161c), shirt: rgb(0x1d3a2c), pants: rgb(0x14161c),
		eyes: rgb(0x2fdc7a), plain: true,
		extra: func(r pen, at int) {
			green, suit := rgb(0x2fdc7a), rgb(0x1d3a2c)

			switch at {
			case down:
				r(2, 3, 2, 6, rgb(0x14161c))
				r(12, 3, 2, 6, rgb(0x14161c))
				r(4, 7, 8, 3, suit)
				r(6, 8, 4, 1, green)
				r(7, 11, 2, 3, green)
			case up:
				r(2, 3, 12, 7, rgb(0x14161c))
			default:
				r(3, 3, 3, 7, rgb(0x14161c))
				r(8, 7, 5, 3, suit)
				r(10, 8, 3, 1, green)
			}
		}}},

	{ID: "cypher", Name: "Cypher", look: look{skin: rgb(0x1b1c26), hair: rgb(0xe8e1d0), shirt: rgb(0xdcd6c8), pants: rgb(0x3b3f4e),
		eyes: rgb(0x4ad2ff), plain: true,
		extra: func(r pen, at int) {
			brim, band, dark := rgb(0xbfb7a4), rgb(0xff8a1f), rgb(0x1b1c26)

			switch at {
			case down:
				r(1, 3, 14, 1, brim)
				r(3, 2, 10, 1, band)
				r(4, 10, 8, 1, rgb(0x8a8fa3))
			case up:
				r(3, 4, 10, 5, dark)
				r(1, 3, 14, 1, brim)
				r(3, 2, 10, 1, band)
			default:
				r(4, 4, 3, 5, dark)
				r(11, 8, 1, 1, dark)
				r(2, 3, 13, 1, brim)
				r(4, 2, 9, 1, band)
			}
		}}},

	{ID: "killjoy", Name: "Killjoy", look: look{skin: rgb(0xffdcb8), hair: rgb(0x3a2a20), shirt: rgb(0xf7d038), pants: rgb(0x2b2e3a),
		extra: func(r pen, at int) {
			beanie := rgb(0x1f4d45)

			switch at {
			case down:
				r(3, 0, 10, 3, beanie)
				r(3, 2, 10, 1, rgb(0x2f6b60))
				r(5, 4, 3, 1, ink)
				r(8, 4, 3, 1, ink)
				r(7, 10, 2, 6, rgb(0x2b2e3a))
			case up:
				r(3, 0, 10, 4, beanie)
			default:
				r(4, 0, 9, 3, beanie)
				r(9, 4, 3, 1, ink)
			}
		}}},

	{ID: "raze", Name: "Raze", look: look{skin: rgb(0x8a5a3a), hair: rgb(0x2b1d14), shirt: rgb(0xff7a1f), pants: rgb(0x3b3f4e),
		extra: func(r pen, at int) {
			band, phones, belt := rgb(0xff7a1f), rgb(0x4ad2ff), rgb(0xf7d038)

			if at == right {
				r(4, 1, 9, 2, band)
				r(6, 4, 2, 3, phones)
				r(5, 14, 6, 1, belt)

				return
			}

			r(3, 1, 10, 2, band)
			r(2, 4, 1, 3, phones)
			r(13, 4, 1, 3, phones)
			r(4, 14, 8, 1, belt)
		}}},

	{ID: "sova", Name: "Sova", look: look{skin: rgb(0xffdcb8), hair: rgb(0xf0dc9a), shirt: rgb(0x3d5f9e), pants: rgb(0x2b3550), style: 1,
		extra: func(r pen, at int) {
			fur := rgb(0xeef2f7)

			switch at {
			case down:
				r(9, 5, 1, 2, rgb(0x4ad2ff))
				r(3, 10, 10, 2, fur)
			case up:
				r(3, 10, 10, 2, fur)
				r(4, 12, 8, 4, rgb(0x2b3f73))
			default:
				r(4, 10, 8, 2, fur)
			}
		}}},

	{ID: "brimstone", Name: "Brimstone", look: look{skin: rgb(0xe0b08a), hair: rgb(0x6b4a2f), shirt: rgb(0x4a5a3a), pants: rgb(0x2b2e3a),
		extra: func(r pen, at int) {
			beret, beard := rgb(0xd9541f), rgb(0x6b4a2f)

			switch at {
			case down:
				r(3, 0, 10, 3, beret)
				r(2, 2, 3, 1, beret)
				r(4, 8, 8, 2, beard)
				r(4, 12, 8, 1, beret)
			case up:
				r(3, 0, 10, 3, beret)
				r(4, 12, 8, 1, beret)
			default:
				r(4, 0, 9, 3, beret)
				r(9, 8, 4, 2, beard)
				r(5, 12, 6, 1, beret)
			}
		}}},

	{ID: "neon", Name: "Neon", look: look{skin: rgb(0xf1c9a5), hair: rgb(0x2f5fe6), shirt: rgb(0x25346b), pants: rgb(0x1b2140),
		extra: func(r pen, at int) {
			hair, bolt := rgb(0x2f5fe6), rgb(0xffe14a)

			if at == right {
				r(2, 2, 2, 8, hair)
				r(2, 9, 2, 1, bolt)
				r(5, 12, 6, 1, bolt)

				return
			}

			r(1, 2, 2, 7, hair)
			r(13, 2, 2, 7, hair)
			r(1, 8, 2, 1, bolt)
			r(13, 8, 2, 1, bolt)

			if at == down {
				r(7, 11, 2, 3, bolt)
			}
		}}},

	{ID: "astra", Name: "Astra", look: look{skin: rgb(0x5a3a26), hair: rgb(0x2a1a3a), shirt: rgb(0x5a2f8c), pants: rgb(0x2a1a3a), style: 1,
		extra: func(r pen, at int) {
			gold := rgb(0xf2c14e)

			if at == right {
				r(4, 0, 9, 1, gold)
				r(5, 10, 6, 1, gold)
				r(7, 11, 2, 4, gold)

				return
			}

			r(3, 0, 10, 1, gold)
			r(4, 10, 8, 1, gold)
			r(12, 10, 1, 5, gold)
		}}},

	{ID: "skye", Name: "Skye", look: look{skin: rgb(0xf1c9a5), hair: rgb(0xd9622b), shirt: rgb(0x3f8f57), pants: rgb(0x4a3a2a),
		extra: func(r pen, at int) {
			hair, leather := rgb(0xd9622b), rgb(0x8a5a3a)

			switch at {
			case down:
				r(11, 4, 2, 8, hair)
				r(3, 10, 2, 2, leather)
			case up:
				r(7, 9, 2, 6, hair)
			default:
				r(3, 4, 2, 9, hair)
				r(5, 10, 3, 2, leather)
			}
		}}},

	{ID: "yoru", Name: "Yoru", look: look{skin: rgb(0xf1c9a5), hair: rgb(0x2b4fd9), shirt: rgb(0x1f2a5a), pants: rgb(0x1b1c26), style: 2,
		extra: func(r pen, at int) {
			red := rgb(0xe23b4a)

			if at == right {
				r(8, -2, 2, 1, rgb(0x2b4fd9))
				r(5, 10, 1, 6, red)

				return
			}

			r(9, -2, 2, 1, rgb(0x2b4fd9))
			r(4, 10, 1, 6, red)
			r(11, 10, 1, 6, red)
		}}},

	{ID: "chamber", Name: "Chamber", look: look{skin: rgb(0xe6bf9a), hair: rgb(0x3a2a20), shirt: rgb(0xe8e1d0), pants: rgb(0x3b3526),
		extra: func(r pen, at int) {
			vest, gold := rgb(0x6b5a3a), rgb(0xc9a44a)

			switch at {
			case down:
				r(4, 10, 2, 6, vest)
				r(10, 10, 2, 6, vest)
				r(7, 10, 2, 4, gold)
				r(5, 4, 6, 1, gold)
			case up:
				r(4, 10, 8, 6, vest)
			default:
				r(5, 10, 2, 6, vest)
				r(9, 4, 4, 1, gold)
			}
		}}},
}

var skins = func() map[string]Skin {
	m := map[string]Skin{}
	for i := range Skins {
		Skins[i].Zone = "valorant"
		m[Skins[i].ID] = Skins[i]
	}

	return m
}()

// HasSkin tells a skin's ID from anything else.
func HasSkin(id string) bool {
	_, ok := skins[id]
	return ok
}
