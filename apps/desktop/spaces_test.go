package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gonativeui/app"
)

func TestParseInviteCode(t *testing.T) {
	for in, want := range map[string]string{
		"ABC123":                                "ABC123",
		"  ab-c_1 ":                             "ab-c_1",
		"https://discordo.example/invite/XyZ9":  "XyZ9",
		"https://discordo.example/invite/XyZ9/": "XyZ9",
		"http://x/invite/Q1?ref=a#b":            "Q1",
		"https://discordo.example/outra/XyZ9":   "",
		"dois codigos":                          "",
		"":                                      "",
	} {
		if got := parseInviteCode(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestOverwriteTriState(t *testing.T) {
	o := &Overwrite{Allow: permSend, Deny: permView}

	if bitState(nil, permSend) != triInherit || bitState(o, permSend) != triAllow || bitState(o, permView) != triDeny || bitState(o, permReact) != triInherit {
		t.Fatal("bitState")
	}

	// Send moves from allow to deny; view, denied, is kept.
	if allow, deny := setBitState(o, permSend, triDeny); allow != 0 || deny != permView|permSend {
		t.Fatalf("deny send: %b %b", allow, deny)
	}

	// Inheriting the last bit empties the overwrite (the dialog then deletes it).
	if allow, deny := setBitState(&Overwrite{Allow: permSend}, permSend, triInherit); allow|deny != 0 {
		t.Fatalf("inherit: %b %b", allow, deny)
	}

	if allow, deny := setBitState(nil, permReact, triAllow); allow != permReact || deny != 0 {
		t.Fatalf("from none: %b %b", allow, deny)
	}
}

func TestSettingsSections(t *testing.T) {
	keys := func(g *Guild, me string) (l []string) {
		for _, s := range settingsSections(g, me) {
			l = append(l, s.key)
		}

		return l
	}

	g := &Guild{ID: "g", OwnerID: "u1"}
	if l := keys(g, "u2"); len(l) != 0 {
		t.Fatalf("no permissions: %v", l)
	}

	g.Permissions = permKick
	if l := keys(g, "u2"); len(l) != 1 || l[0] != "members" {
		t.Fatalf("kick: %v", l)
	}

	g.Permissions = permAdmin
	if l := keys(g, "u1"); len(l) != 6 || l[5] != "delete" {
		t.Fatalf("admin owner: %v", l)
	}

	if l := keys(g, "u2"); len(l) != 5 {
		t.Fatalf("admin: %v", l)
	}
}

func TestMyTop(t *testing.T) {
	g := &Guild{OwnerID: "u1", Roles: []Role{{ID: "r1", Position: 3}, {ID: "r2", Position: 1}},
		Members: []Member{{User: User{ID: "u2"}, Roles: []string{"r1", "r2"}}, {User: User{ID: "u3"}}}}

	if myTop(g, "u2") != 3 || myTop(g, "u3") != 0 || myTop(g, "u1") <= 3 {
		t.Fatal("myTop")
	}
}

// TestSpacesRender draws the servers' dialogs offscreen; DISCORDO_SHOTS=dir saves them.
func TestSpacesRender(t *testing.T) {
	shots := []struct {
		name string
		set  func(d *Discordo)
	}{
		{"sidebar", func(*Discordo) {}},
		{"add-server", func(d *Discordo) { d.spaces().adding = true }},
		{"invite", func(d *Discordo) {
			s := d.spaces()
			s.inviting, s.inviteGuildID, s.link = true, "g1", d.inviteLink("AbC123")
		}},
		{"create-channel", func(d *Discordo) { d.openCreateChannel() }},
		{"leave", func(d *Discordo) { d.spaces().leaving = true }},
		{"channel-settings", func(d *Discordo) { g := d.st.Guild("g1"); d.openChannelSettings(g, g.Channel("c1")) }},
		{"channel-permissions", func(d *Discordo) {
			g := d.st.Guild("g1")
			d.openChannelSettings(g, g.Channel("c1"))
			d.spaces().channel.tab = 1
		}},
	}

	for i, sec := range settingsSections(&Guild{Permissions: permAdmin}, "") {
		shots = append(shots, struct {
			name string
			set  func(d *Discordo)
		}{"settings-" + sec.key, func(d *Discordo) {
			d.openServerSettings(d.st.Guild("g1"))
			d.spaces().server.tab = i
		}})
	}

	for _, s := range shots {
		t.Run(s.name, func(t *testing.T) {
			d := newDiscordo("http://localhost:0")
			d.st, d.phase, d.ready = ready(t), phaseOnline, true

			g := d.st.Guild("g1")
			g.Permissions = permAdmin
			g.BannerURL = "/images/none.png"
			g.Roles = append(g.Roles, Role{ID: "g1", Name: "@everyone"}, Role{ID: "r2", Name: "Moderação", Color: 0x5865f2, Position: 2})
			g.Members[1].Roles = append(g.Members[1].Roles, "r2")
			g.Channels[0].Permissions = permAdmin | permRoles | permManage | permSend | permView
			g.Channels[0].Overwrites = []Overwrite{{TargetID: "r1", Allow: permSend, Deny: permReact}}

			o, err := app.NewOffscreen(config(), 1280, 860, 1, d)
			if err != nil {
				t.Skip("no offscreen renderer here:", err)
			}
			defer o.Close()

			d.open("g1", "c1")
			s.set(d)

			img, _ := o.Frame()
			for range 8 {
				img, _ = o.Frame()
			}

			if dir := os.Getenv("DISCORDO_SHOTS"); dir != "" {
				f, err := os.Create(filepath.Join(dir, s.name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()

				png.Encode(f, img)
			}
		})
	}
}
