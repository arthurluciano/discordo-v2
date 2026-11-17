module discordo/desktop

go 1.27.3

require (
	github.com/coder/websocket v1.8.15
	github.com/ebitengine/purego v0.12.0-alpha.1.0.20261015130536-d456b1bb95e8
	github.com/kazzmir/opus-go v1.7.0
	github.com/pion/interceptor v0.1.51
	github.com/pion/rtcp v1.2.20
	github.com/pion/rtp v1.10.6
	github.com/pion/webrtc/v4 v4.2.26
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef
	golang.org/x/sys v0.41.0
	gonativeui v0.0.0
)

require (
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/pion/datachannel v1.6.3 // indirect
	github.com/pion/dtls/v3 v3.1.15 // indirect
	github.com/pion/ice/v4 v4.4.8 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/mdns/v2 v2.2.2 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/sctp v1.12.0 // indirect
	github.com/pion/sdp/v3 v3.0.20 // indirect
	github.com/pion/srtp/v3 v3.1.3 // indirect
	github.com/pion/stun/v4 v4.0.1 // indirect
	github.com/pion/transport/v5 v5.1.4 // indirect
	github.com/pion/turn/v5 v5.1.3 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/image v0.23.0 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/time v0.14.0 // indirect
)

// The framework is not a published module: it is the submodule beside this file.
replace gonativeui => ./third_party/go-native-ui

// Its own patched dependencies (replace directives of a dependency do not reach us).
replace github.com/go-text/typesetting => ./third_party/go-native-ui/third_party/typesetting

replace github.com/srwiley/oksvg => ./third_party/go-native-ui/third_party/oksvg
