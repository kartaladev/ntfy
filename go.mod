module github.com/kartaladev/ntfy

go 1.26.0

// ntfy is a generic notification library. Its production code uses the standard
// library only; storage, WebSocket transport and cross-instance broadcasting are
// the separate modules beside it.

require (
	github.com/stretchr/testify v1.12.1 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.uber.org/mock v0.6.0 // indirect
)
