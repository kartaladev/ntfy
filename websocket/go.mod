module github.com/kartaladev/ntfy/websocket

go 1.26.0

// The dependency on github.com/kartaladev/ntfy is supplied by the repository's
// go.work during development and is written in here, with a real version, when
// ntfy is tagged.

require (
	github.com/coder/websocket v1.8.15 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	go.uber.org/goleak v1.3.0 // indirect
)
