package main

import (
	"slices"
	"testing"
)

func TestPickable(t *testing.T) {
	rels := []Relationship{{User: caio, Status: 1}, {User: beto, Status: 1}, {User: ana, Status: 2}}

	got := pickable(rels, nil)
	if len(got) != 2 || got[0].ID != beto.ID || got[1].ID != caio.ID {
		t.Fatalf("friends by name: %v", got)
	}

	if got := pickable(rels, []User{beto}); len(got) != 1 || got[0].ID != caio.ID {
		t.Fatalf("the group's people are left out: %v", got)
	}
}

func TestTogglePick(t *testing.T) {
	p := togglePick(nil, "a")
	p = togglePick(p, "b")
	p = togglePick(p, "a")

	if !slices.Equal(p, []string{"b"}) {
		t.Fatalf("togglePick = %v", p)
	}
}
