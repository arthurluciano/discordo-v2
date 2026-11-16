package image

import "testing"

func TestValid(t *testing.T) {
	for url, want := range map[string]bool{
		"": true,
		"/images/01890a5d-ac96-774b-bcce-b302099a8057":      true,
		"/images/../etc/passwd":                             false,
		"https://example.com/a.png":                         false,
		"/images/":                                          false,
		"/attachments/01890a5d-ac96-774b-bcce-b302099a8057": false,
	} {
		if got := Valid(url); got != want {
			t.Errorf("Valid(%q) = %v, want %v", url, got, want)
		}
	}
}
