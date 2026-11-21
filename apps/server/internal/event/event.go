// Package event is the contract between domain services and the realtime gateway.
package event

type Kind int

const (
	ToGuild   Kind = iota // every online member of GuildID
	ToChannel             // members of GuildID who can view ChannelID
	ToUsers               // UserIDs
	Sync                  // every online member of GuildID gets a fresh snapshot of it (Type/Data unused)
	Joined                // UserIDs became members of GuildID: subscribe them and send the snapshot
	Left                  // UserIDs are no longer members of GuildID: unsubscribe and tell them
)

type Event struct {
	Kind      Kind
	Type      string
	Data      any
	GuildID   string
	ChannelID string
	UserIDs   []string
}

type Publisher interface {
	Publish(Event)
}
