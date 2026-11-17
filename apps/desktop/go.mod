module discordo/desktop

go 1.27.3

require (
	github.com/coder/websocket v1.8.15
	gonativeui v0.0.0
)

require (
	github.com/ebitengine/purego v0.12.0-alpha.1.0.20261015130536-d456b1bb95e8 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	golang.org/x/image v0.23.0 // indirect
)

// The framework is not a published module: it is the submodule beside this file.
replace gonativeui => ./third_party/go-native-ui

// Its own patched dependencies (replace directives of a dependency do not reach us).
replace github.com/go-text/typesetting => ./third_party/go-native-ui/third_party/typesetting

replace github.com/srwiley/oksvg => ./third_party/go-native-ui/third_party/oksvg
