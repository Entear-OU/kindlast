// The policy gateway: the only process that dials an address a customer
// supplied (core-api-surface §21.4, §26.4).
//
// A module of its own rather than a package in core-api, so the two cannot
// share a dependency set and a careless import cannot cross the boundary. CI
// checks the second half directly: apps/workers must not import apps/core-api.
//
// `go 1.25.0` to match go.work and the two sibling modules. A module that
// declares a newer toolchain than the workspace is one that silently downloads
// a second Go to build, which is a poor footing for a linter whose findings
// depend on the language version it parses with.
module github.com/Entear-OU/kindlast/apps/workers

go 1.25.0

require (
	connectrpc.com/connect v1.20.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)
