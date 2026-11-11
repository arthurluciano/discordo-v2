// Package media is the contract with whatever carries voice and video (the built-in SFU, or LiveKit).
package media

import (
	"context"
	"encoding/json"
)

// Track names a participant may publish. The name is the WebRTC track ID.
const (
	Mic         = "mic"
	Camera      = "camera"
	Screen      = "screen"
	ScreenAudio = "screen-audio"
)

type Grants struct {
	Speak  bool // mic
	Stream bool // camera, screen and its audio
}

// Connection tells the client how to reach the media plane.
type Connection struct {
	Kind  string `json:"kind"`            // "pion": signal over the gateway; "livekit": connect to URL with Token
	URL   string `json:"url,omitempty"`   // livekit only
	Token string `json:"token,omitempty"` // livekit only
}

// Sender delivers a signaling message to one participant. It must not block or call back into the provider.
type Sender func(typ string, data any)

type Provider interface {
	// Join puts the user in the room, replacing any previous connection of theirs to it.
	Join(ctx context.Context, roomID, userID string, grants Grants, send Sender) (Connection, error)
	// Signal handles a provider-specific signaling message from the participant.
	Signal(ctx context.Context, roomID, userID, typ string, data json.RawMessage) error
	Leave(roomID, userID string)
	// OnTracks registers the callback told which tracks a participant is publishing, whenever that changes.
	OnTracks(func(roomID, userID string, tracks []string))
}
