// Package stackenv answers one question for the test suites: which compose
// stack is this process supposed to talk to?
//
// ONE STACK PER WORKTREE (ENT-250). `deploy/compose.yaml` used to pin
// `name: kindlast`, so every checkout on a machine addressed one Postgres and
// one Redis. That is fine with one checkout. With several it is how a
// migration from an unmerged branch reaches a sibling branch's test run, and
// on 2026-08-18 it did: three people separately investigated the same corpus
// test failing on branches that had never touched the corpus, and main went
// red over a column that existed only because of a branch that had not merged.
//
// `scripts/stack-env.sh` derives a project name and a block of host ports from
// the worktree path and exports both the `KINDLAST_*_PORT` names compose reads
// and the `PG_*_URL` names these suites read:
//
//	eval "$(./scripts/stack-env.sh)"
//	cd apps/core-api && go test -p 1 ./...
//
// This package is the reason a suite only has to be told once. Every DSN in
// the Go suites used to be its own string literal ending in `127.0.0.1:5433`,
// which meant a worktree pointing its stack somewhere else had to be believed
// by a dozen separate files.
//
// It is test support that lives in a non-test file so both
// `internal/store/postgres` and `internal/server/interceptor` can use it. It
// is never linked into the binary: nothing outside a _test.go file imports it.
package stackenv

import (
	"fmt"
	"os"
	"strings"
)

// Defaults for a single checkout, which are the values every instruction in
// README.md, docs/ and the Postman collection names. Nothing about this
// package changes them; it changes only how far an override reaches.
const (
	defaultHost      = "127.0.0.1"
	defaultPGPort    = "5433"
	defaultRedisPort = "6379"
)

// DSN is the development connection string for one database role, on whichever
// stack this process is pointed at.
//
// Precedence, widest override first:
//
//	PG_<ROLE>_URL            the whole DSN, which is what CI and
//	                         scripts/stack-env.sh set
//	PG_HOST, PG_PORT         host and port, for a stack that is otherwise
//	                         standard
//	KINDLAST_PG_APP_PORT     the port compose itself publishes on, so setting
//	                         only the compose variable still lands here
//
// The passwords are the compose defaults from
// deploy/postgres/init/01-roles.sh, and are development-only by construction:
// a deployment sets `KINDLAST_*_PASSWORD` and never sees these.
func DSN(role string) string {
	if dsn := os.Getenv("PG_" + strings.ToUpper(role) + "_URL"); dsn != "" {
		return dsn
	}
	return fmt.Sprintf(
		"postgres://kindlast_%s:%s-dev-password@%s:%s/kindlast",
		role, role, host(), pgPort(),
	)
}

// RedisAddr is the host address of this stack's Redis.
func RedisAddr() string {
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		return addr
	}
	return host() + ":" + valueOr("KINDLAST_REDIS_PORT", defaultRedisPort)
}

func host() string { return valueOr("PG_HOST", defaultHost) }

func pgPort() string {
	if port := os.Getenv("PG_PORT"); port != "" {
		return port
	}
	return valueOr("KINDLAST_PG_APP_PORT", defaultPGPort)
}

func valueOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
