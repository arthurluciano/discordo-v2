// Command desktop is Discordo's native app (macOS, Windows, Linux) on go-native-ui, laid out as the web app:
// the server rail, the channel or DM sidebar with the user panel, the chat, and the member list.
//
//	go run .                                  # the server on Openship
//	go run . -server http://localhost:8080   # a local one
package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"gonativeui/app"
	"gonativeui/img"
	"gonativeui/menu"
	"gonativeui/platform"
	"gonativeui/rexui"
	"gonativeui/theme"
	"gonativeui/theme/rex"
	"gonativeui/widget"
	"gonativeui/widget/base"
)

//go:embed assets/mascot.png
var mascot []byte

const cmdSignOut menu.ID = 1

type phase uint8

const (
	phaseLoading phase = iota // reading the saved token
	phaseSignedOut
	phaseSigningIn // waiting for the browser
	phaseOnline
)

// Friends tabs, as the web's.
const (
	tabOnline = iota
	tabAll
	tabPending
	tabBlocked
)

type Discordo struct {
	a    *app.App
	k    *widget.Kit
	imgs *img.Cache

	client Client
	st     *State
	phase  phase
	ready  bool   // READY arrived
	status string // the last error, shown until the next action
	gen    int    // bumps on sign-out: results of an older session are dropped
	stop   context.CancelFunc

	inbox chan func(*Discordo) // results from goroutines, applied on the UI thread

	// Where we are: a guild and its channel, or home ("") with a DM or the friends ("").
	guild, channel string
	last           map[string]string // guild → its last channel
	friendsTab     int

	loading map[string]bool // channel → a page is on its way

	composer *rexui.ComposerState
	sending  bool

	logScroll, sideScroll, railScroll, memberScroll, friendScroll base.ScrollState

	pin   int  // frames left to keep the log at its end
	atEnd bool // the log was at its end in the last frame

	pictures map[string]*picture // by URL or path

	ui uiCache
}

// picture is a downloaded avatar, icon or attachment.
type picture struct {
	data    []byte
	hash    uint64
	failed  bool
	pending bool
}

func main() {
	server := flag.String("server", envOr("DISCORDO_URL", "https://discordo.arluck.com.br"), "the Discordo server's URL")
	flag.Parse()

	if err := app.Run(config(), newDiscordo(*server)); err != nil {
		fmt.Fprintln(os.Stderr, "discordo:", err)
		os.Exit(1)
	}
}

func newDiscordo(server string) *Discordo {
	return &Discordo{
		client:   Client{Server: strings.TrimRight(server, "/")},
		st:       NewState(),
		inbox:    make(chan func(*Discordo), 256),
		last:     map[string]string{},
		loading:  map[string]bool{},
		pictures: map[string]*picture{},
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}

func config() app.Config {
	return app.Config{Title: "Discordo", Width: 1280, Height: 800, MinWidth: 940, MinHeight: 520, Clear: rgbaF(canvas)}
}

func (d *Discordo) Init(a *app.App) error {
	d.a = a

	th := rex.Theme()
	th.SetAccent(blurple, blurple)
	d.k = widget.NewKit(a.UI, a.Text, th)
	d.k.SetPreference(theme.ForceDark) // the web app is dark
	rexui.SetStrings(rexui.StringsPtBR)
	styleComponents()

	d.imgs = img.NewCache(img.Options{Renderer: a.R, GPUBytes: 96 << 20, CPUBytes: 32 << 20, Entries: 1024})
	d.k.SetImages(d.imgs)

	m, err := menu.NewModel(
		[]menu.Command{{ID: cmdSignOut, Title: "Sair da conta"}},
		[]menu.Menu{{Title: "Conta", Items: []menu.Item{{Kind: menu.ItemCommand, Command: cmdSignOut}}}},
		[]int{0},
	)
	if err != nil {
		return err
	}

	d.k.SetMenu(m)
	a.SetMenu(m)

	d.composer, err = rexui.NewComposer(a.Win, rexui.ComposerSetup{MaxLen: 4000})
	if err != nil {
		return err
	}

	a.UI.Reserve(8192)

	if d.phase != phaseLoading {
		return nil // a test that set the state itself
	}

	// The Keychain may ask the user: never on the UI thread.
	go func() {
		token := loadToken(d.client.Server)
		d.post(func(d *Discordo) {
			if token == "" {
				d.phase = phaseSignedOut
				return
			}

			d.connect(token)
		})
	}()

	return nil
}

// post hands a result to the UI thread and wakes it.
func (d *Discordo) post(f func(*Discordo)) {
	d.inbox <- f
	platform.Wake()
}

// connect keeps the gateway up (reconnecting with a growing pause) until sign-out.
func (d *Discordo) connect(token string) {
	d.client.Token = token
	d.phase, d.ready = phaseOnline, false

	ctx, cancel := context.WithCancel(context.Background())
	d.stop = cancel
	client, gen := d.client, d.gen

	go func() {
		pause := time.Second

		for {
			err := client.Gateway(ctx, func(e Event) {
				d.post(func(d *Discordo) {
					if d.gen == gen {
						d.apply(e)
					}
				})
			})

			if ctx.Err() != nil {
				return
			}

			if errors.Is(err, errUnauthorized) {
				d.post(func(d *Discordo) {
					if d.gen == gen {
						d.signOut("Sua sessão expirou. Entre de novo.")
					}
				})

				return
			}

			time.Sleep(pause)
			pause = min(pause*2, 30*time.Second)
		}
	}()
}

func (d *Discordo) apply(e Event) {
	if err := d.st.Apply(e); err != nil {
		d.status = "Evento inválido do servidor: " + err.Error()
		return
	}

	switch e.T {
	case "READY":
		d.ready, d.loading = true, map[string]bool{}

		// Somewhere that still exists: the guild may be gone after a reconnect.
		if d.guild != "" && d.st.Guild(d.guild) == nil {
			d.guild, d.channel = "", ""
		}

		d.open(d.guild, d.channel)

	case "GUILD_DELETE":
		if d.guild != "" && d.st.Guild(d.guild) == nil {
			d.open("", "")
		}

	case "MESSAGE_CREATE":
		if d.atEnd {
			d.pin = 2
		}
	}
}

func (d *Discordo) signIn() {
	d.phase, d.status = phaseSigningIn, ""
	server, gen := d.client.Server, d.gen

	go func() {
		token, err := Login(context.Background(), server)
		if err == nil {
			err = saveToken(server, token)
		}

		d.post(func(d *Discordo) {
			if d.gen != gen {
				return
			}

			if err != nil {
				d.phase, d.status = phaseSignedOut, "Não foi possível entrar: "+err.Error()
				return
			}

			d.connect(token)
		})
	}()
}

func (d *Discordo) signOut(reason string) {
	if d.stop != nil {
		d.stop()
	}

	client := d.client
	go func() {
		client.Logout(context.Background()) // the token is dropped here whatever the server says
		saveToken(client.Server, "")
	}()

	d.gen++
	d.client.Token = ""
	d.st = NewState()
	d.phase, d.ready, d.status = phaseSignedOut, false, reason
	d.guild, d.channel = "", ""
}

// open goes to a guild (its last or first text channel when channel is ""), or home with a DM or the friends.
func (d *Discordo) open(guildID, channelID string) {
	if g := d.st.Guild(guildID); g != nil && (channelID == "" || g.Channel(channelID) == nil) {
		channelID = d.last[guildID]

		if c := g.Channel(channelID); c == nil || c.Type != ChannelText {
			channelID = ""
			for _, c := range g.Channels {
				if c.Type == ChannelText {
					channelID = c.ID
					break
				}
			}
		}
	}

	if guildID != "" {
		d.last[guildID] = channelID
	}

	if channelID != d.channel && d.composer != nil {
		d.composer.SetText(nil)
	}

	d.guild, d.channel, d.status, d.pin = guildID, channelID, "", 2

	world := false
	if g := d.st.Guild(guildID); g != nil {
		c := g.Channel(channelID)
		world = c != nil && c.Type == ChannelWorld
	}

	// The world has no messages.
	if channelID != "" && !world && d.st.Logs[channelID] == nil {
		d.loadPage(channelID)
	}
}

// loadPage brings the newest page of a channel, or the page before its oldest message.
func (d *Discordo) loadPage(channelID string) {
	if d.loading[channelID] {
		return
	}

	before := ""
	if log := d.st.Logs[channelID]; log != nil {
		if log.Complete || len(log.List) == 0 {
			return
		}

		before = log.List[0].ID
	}

	d.loading[channelID] = true
	client, gen := d.client, d.gen

	go func() {
		msgs, err := client.Messages(context.Background(), channelID, before)

		d.post(func(d *Discordo) {
			if d.gen != gen {
				return
			}

			d.loading[channelID] = false

			if err != nil {
				d.status = "Não foi possível carregar as mensagens: " + err.Error()
				return
			}

			d.st.AddMessages(channelID, len(msgs) < pageSize, msgs...)
			if before == "" && channelID == d.channel {
				d.pin = 2
			}
		})
	}()
}

// send is composer.tsx's submit: the text and the uploaded files; with only files, the text may be empty.
func (d *Discordo) send() {
	text := strings.TrimSpace(string(d.composer.Text()))
	ids, _ := d.chatReady()
	if !d.chatCanSend() || d.channel == "" {
		return
	}

	if ids == nil {
		ids = []string{} // the server wants a list
	}

	d.composer.SetText(nil)
	d.sending = true
	d.chatSent() // the mention list and the typing clock start over
	client, gen, channelID := d.client, d.gen, d.channel

	go func() {
		var m Message
		err := client.do(context.Background(), "POST", "/channels/"+channelID+"/messages", map[string]any{"content": text, "attachments": ids}, &m)

		d.post(func(d *Discordo) {
			if d.gen != gen {
				return
			}

			d.sending = false
			if err != nil {
				d.status = "Mensagem não enviada: " + err.Error()
				d.composer.SetText([]byte(text)) // nothing typed is lost
				return
			}

			d.chatClearUploads(channelID, ids)
			d.st.AddMessages(channelID, false, m)
			d.pin = 2
		})
	}()
}

// toggleReaction adds my reaction or takes it back; the gateway's REACTION_* updates the message.
func (d *Discordo) toggleReaction(m *Message, emoji string) {
	mine := slices.ContainsFunc(m.Reactions, func(r Reaction) bool {
		return r.Emoji == emoji && slices.Contains(r.Users, d.st.Me.ID)
	})

	client, gen, channelID, messageID := d.client, d.gen, m.ChannelID, m.ID

	go func() {
		err := client.React(context.Background(), channelID, messageID, emoji, !mine)

		d.post(func(d *Discordo) {
			if d.gen == gen && err != nil {
				d.status = "Não foi possível reagir: " + err.Error()
			}
		})
	}()
}

// image returns the picture at ref (downloading it the first time), decoded to fit w×h pixels.
func (d *Discordo) image(ref string, w, h int32, cover bool) (img.Handle, img.State) {
	if ref == "" || d.imgs == nil {
		return 0, img.Failed
	}

	p := d.pictures[ref]
	if p == nil {
		p = &picture{pending: true, hash: img.HashString(ref)}
		d.pictures[ref] = p
		client := d.client

		go func() {
			data, err := client.Fetch(context.Background(), ref)

			d.post(func(d *Discordo) {
				p.pending, p.data, p.failed = false, data, err != nil
			})
		}()
	}

	if p.pending || p.failed {
		return 0, img.Failed
	}

	return d.request(p.hash, p.data, w, h, cover)
}

func (d *Discordo) request(hash uint64, data []byte, w, h int32, cover bool) (img.Handle, img.State) {
	key := img.Key{Hash: hash, W: w, H: h}
	if cover {
		key.Flags = img.Cover
	}

	return d.imgs.Request(key, img.Bytes(data))
}
