package voice

// The noise gate (the web's lib/noise-gate.ts): it closes the microphone when what comes in is only the room. The
// threshold is learned, not set: the floor follows the quietest level down at once and up slowly (~30 s), and the
// gate opens well above it. Closed is not mute: a weak syllable taken for noise comes back quieter, not gone.
const (
	gateHold       = 12            // frames of 20 ms the gate stays open after the voice drops (the web's 90 blocks)
	gateOpenAbove  = 3.5           // how far above the floor the level must rise to open
	gateCloseAbove = 1.8           // and how far it may fall without closing
	gateFloorRise  = 0.00009 * 7.5 // per frame: the web's per-block rise over its 7.5 blocks of 128 samples
	quietestSpeech = 0.004         // ~-48 dBFS: no useful speech below this
	closedGain     = 0.05
)

type gate struct {
	floor float32
	open  bool
	hold  int
	gain  float32 // the gain the last frame ended at, so the next ramps from it
}

func newGate() gate { return gate{floor: quietestSpeech, gain: closedGain} }

// next moves the gate on one frame of the given level.
func (g *gate) next(level float32) {
	if level < g.floor {
		g.floor = level
	} else {
		g.floor += (level - g.floor) * gateFloorRise
	}

	switch {
	case g.open && level > max(g.floor*gateCloseAbove, quietestSpeech*0.6):
		g.hold = gateHold
	case g.open && g.hold > 0:
		g.hold--
	case g.open:
		g.open = false
	case level > max(g.floor*gateOpenAbove, quietestSpeech):
		g.open, g.hold = true, gateHold
	}
}

// apply runs the gate on a frame of level and scales it in place, ramping from the last frame's gain (no clicks).
func (g *gate) apply(pcm []int16, level float32) {
	g.next(level)
	target := float32(closedGain)
	if g.open {
		target = 1
	}

	from := g.gain
	for i, v := range pcm {
		k := from + (target-from)*float32(i+1)/float32(len(pcm))
		pcm[i] = int16(float32(v) * k)
	}

	g.gain = target
}
