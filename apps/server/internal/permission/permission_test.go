package permission

import "testing"

func TestCompute(t *testing.T) {
	const everyone, user, mod = "everyone", "user", "mod"
	base := ViewChannel | SendMessages

	tests := []struct {
		name  string
		base  Set
		roles []string
		ows   []Overwrite
		want  Set
	}{
		{"no overwrites", base, nil, nil, base},
		{"administrator ignores overwrites", base | Administrator, nil,
			[]Overwrite{{TargetID: everyone, Deny: ViewChannel}}, All},
		{"everyone deny hides channel", base, nil,
			[]Overwrite{{TargetID: everyone, Deny: ViewChannel}}, SendMessages},
		{"role allow beats everyone deny", base, []string{mod},
			[]Overwrite{{TargetID: mod, Allow: ViewChannel}, {TargetID: everyone, Deny: ViewChannel}}, base},
		{"role of someone else is ignored", base, nil,
			[]Overwrite{{TargetID: mod, Deny: SendMessages}}, base},
		{"member deny beats role allow", base, []string{mod},
			[]Overwrite{{TargetID: mod, Allow: Stream}, {TargetID: user, IsMember: true, Deny: Stream | SendMessages}}, ViewChannel},
		{"other member's overwrite is ignored", base, nil,
			[]Overwrite{{TargetID: "other", IsMember: true, Deny: ViewChannel}}, base},
	}
	for _, tt := range tests {
		if got := Compute(tt.base, everyone, user, tt.roles, tt.ows); got != tt.want {
			t.Errorf("%s: got %b, want %b", tt.name, got, tt.want)
		}
	}
}
