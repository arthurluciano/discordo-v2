package voice

import "testing"

// Room noise stays closed, speech over it opens, and the gate closes again after its hold.
func TestGate(t *testing.T) {
	g := newGate()
	for range 5000 { // 100 s of a fan: the floor learns it
		g.next(0.01)
	}

	if g.open {
		t.Fatal("the room alone opened the gate")
	}

	g.next(0.2)
	if !g.open {
		t.Fatal("speech did not open the gate")
	}

	for range gateHold {
		g.next(0.01)
	}

	if !g.open {
		t.Fatal("closed before its hold")
	}

	g.next(0.01)
	if g.open {
		t.Fatal("still open after the hold")
	}

	pcm := []int16{10000, 10000, 10000, 10000}
	g.apply(pcm, 0.01)
	if pcm[3] > 10000*closedGain+1 {
		t.Fatalf("closed frame not attenuated: %v", pcm)
	}
}
