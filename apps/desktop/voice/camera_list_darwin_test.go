package voice

import "testing"

// Listing cameras needs no grant: it must not crash, and names what it finds.
func TestCamerasList(t *testing.T) {
	cams, err := Cameras()
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range cams {
		if c.ID == "" {
			t.Fatalf("a camera without an ID: %+v", c)
		}

		t.Logf("camera %q", c.Name)
	}
}
