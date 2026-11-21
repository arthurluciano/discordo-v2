package voice

import "testing"

func TestSplitAnnexB(t *testing.T) {
	au := []byte{0, 0, 0, 1, 0x67, 1, 2, 0, 0, 1, 0x68, 3, 0, 0, 0, 1, 0x65, 4, 0, 5}
	nals := splitAnnexB(au)
	want := []string{"\x67\x01\x02", "\x68\x03", "\x65\x04\x00\x05"}
	if len(nals) != len(want) {
		t.Fatalf("%d NAL units: %x", len(nals), nals)
	}

	for i, n := range nals {
		if string(n) != want[i] {
			t.Errorf("NAL %d = %x, want %x", i, n, want[i])
		}
	}

	if len(splitAnnexB([]byte{1, 2, 3})) != 0 {
		t.Error("found NAL units without a start code")
	}
}
