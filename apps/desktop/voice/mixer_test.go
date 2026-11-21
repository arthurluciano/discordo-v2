package voice

import (
	"math"
	"testing"

	"github.com/kazzmir/opus-go/opus"
)

// packets encodes n frames of a 440 Hz tone at amplitude amp.
func packets(t *testing.T, n int, amp float64) [][]byte {
	t.Helper()

	enc, err := opus.NewEncoder(Rate, 1, opus.ApplicationVoIP)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()

	var out [][]byte
	pcm := make([]int16, Frame)
	for f := range n {
		for i := range pcm {
			pcm[i] = int16(amp * math.Sin(2*math.Pi*440*float64(f*Frame+i)/Rate))
		}

		buf := make([]byte, 1500)
		k, err := enc.Encode(pcm, Frame, buf)
		if err != nil {
			t.Fatal(err)
		}

		out = append(out, buf[:k])
	}

	return out
}

func TestMixerBuffersThenPlaysAndConceals(t *testing.T) {
	m := NewMixer()
	s, err := m.Add("ana")
	if err != nil {
		t.Fatal(err)
	}

	pk := packets(t, 20, 8000)
	out := make([]int16, Frame)

	// Two packets are not enough to start: silence.
	m.Push(s, 65534, pk[0]) // starts near the wrap of the sequence numbers
	m.Push(s, 65535, pk[1])
	m.Mix(out)
	if Level(out) != 0 {
		t.Fatalf("played before %d packets were buffered", startDepth)
	}

	// The third (out of order: 1 then 0 is the wrap) starts it.
	m.Push(s, 1, pk[3])
	m.Push(s, 0, pk[2])
	for range 4 {
		m.Mix(out)
	}

	// Packet 2 (seq 0) played, seq 1 too; now seq 2 is lost but 3 is queued: concealment, not silence.
	m.Push(s, 3, pk[5])
	m.Mix(out)
	if Level(out) < 0.05 {
		t.Fatalf("a lost packet was not concealed: level %v", Level(out))
	}

	speaking := map[string]bool{}
	m.Speaking(speaking)
	if !speaking["ana"] {
		t.Fatal("not speaking while playing a tone")
	}

	// A packet older than what was played is dropped.
	m.Push(s, 65000, pk[6])
	if _, ok := s.pkts[65000]; ok {
		t.Fatal("kept a late packet")
	}

	// Deafened: silence, yet the buffer still drains.
	m.SetDeafened(true)
	m.Mix(out)
	if Level(out) != 0 || len(s.pkts) != 0 {
		t.Fatalf("deafened: level %v, %d packets left", Level(out), len(s.pkts))
	}

	// Ran dry: it stops and buffers again.
	m.SetDeafened(false)
	for range 3 {
		m.Mix(out)
	}
	if s.playing {
		t.Fatal("still playing with nothing buffered")
	}

	m.Remove(s)
}

func TestMixerCatchesUpAndScalesVolume(t *testing.T) {
	m := NewMixer()
	s, _ := m.Add("bia")
	pk := packets(t, 20, 4000)
	for i := range 20 {
		m.Push(s, uint16(100+i), pk[i])
	}

	out := make([]int16, Frame)
	m.Mix(out) // 20 queued > maxDepth: skips ahead to the newest startDepth
	if s.next != 100+20-startDepth+1 {
		t.Fatalf("next = %d after catching up", s.next)
	}

	loud := Level(out)
	m.SetVolume("bia", 0)
	m.Mix(out)
	if loud == 0 || Level(out) != 0 {
		t.Fatalf("volume: %v at 1, %v at 0", loud, Level(out))
	}
}
