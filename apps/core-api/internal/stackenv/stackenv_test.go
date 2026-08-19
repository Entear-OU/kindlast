package stackenv

import "testing"

// The default is the property worth protecting. Every instruction in
// README.md, docs/self-hosting.md and the Postman collection names 5433 and
// 6379, and ENT-250 is only safe to land because a single checkout still gets
// exactly those. A test that only proved the override works would let the
// default drift and nothing would notice until somebody followed the docs.
func TestDefaultsAreTodaysComposePorts(t *testing.T) {
	clearStackEnv(t)

	if got, want := DSN("app"), "postgres://kindlast_app:app-dev-password@127.0.0.1:5433/kindlast"; got != want {
		t.Errorf("DSN(app) = %q, want %q", got, want)
	}
	if got, want := DSN("migrator"), "postgres://kindlast_migrator:migrator-dev-password@127.0.0.1:5433/kindlast"; got != want {
		t.Errorf("DSN(migrator) = %q, want %q", got, want)
	}
	if got, want := RedisAddr(), "127.0.0.1:6379"; got != want {
		t.Errorf("RedisAddr() = %q, want %q", got, want)
	}
}

// Setting only the variable compose itself reads has to be enough. A worktree
// that pointed compose at another port and left the suites on 5433 is the
// exact failure this package exists to prevent, and it would look like a
// passing run against somebody else's database rather than like an error.
func TestComposePortAloneRedirectsEverySuite(t *testing.T) {
	clearStackEnv(t)
	t.Setenv("KINDLAST_PG_APP_PORT", "26608")
	t.Setenv("KINDLAST_REDIS_PORT", "26611")

	if got, want := DSN("agent"), "postgres://kindlast_agent:agent-dev-password@127.0.0.1:26608/kindlast"; got != want {
		t.Errorf("DSN(agent) = %q, want %q", got, want)
	}
	if got, want := RedisAddr(), "127.0.0.1:26611"; got != want {
		t.Errorf("RedisAddr() = %q, want %q", got, want)
	}
}

// CI sets the whole DSN, and a whole DSN outranks a port. Roles differ, so an
// override for one must not answer for another.
func TestFullDSNOverridesPerRole(t *testing.T) {
	clearStackEnv(t)
	t.Setenv("KINDLAST_PG_APP_PORT", "26608")
	t.Setenv("PG_APP_URL", "postgres://elsewhere/db")

	if got, want := DSN("app"), "postgres://elsewhere/db"; got != want {
		t.Errorf("DSN(app) = %q, want %q", got, want)
	}
	if got, want := DSN("ingest"), "postgres://kindlast_ingest:ingest-dev-password@127.0.0.1:26608/kindlast"; got != want {
		t.Errorf("DSN(ingest) = %q, want %q", got, want)
	}
}

// PG_HOST and PG_PORT sit between the two, for a stack that is standard apart
// from where it lives.
func TestHostAndPortOverrideTheComposePort(t *testing.T) {
	clearStackEnv(t)
	t.Setenv("KINDLAST_PG_APP_PORT", "26608")
	t.Setenv("PG_HOST", "db.internal")
	t.Setenv("PG_PORT", "5432")

	if got, want := DSN("app"), "postgres://kindlast_app:app-dev-password@db.internal:5432/kindlast"; got != want {
		t.Errorf("DSN(app) = %q, want %q", got, want)
	}
}

// The developer running this has very likely sourced scripts/stack-env.sh, so
// the process starts with every one of these set. Without clearing them the
// default case would assert whatever that shell happens to hold, which is a
// test that reports on the environment rather than on the code.
func clearStackEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"PG_HOST", "PG_PORT", "KINDLAST_PG_APP_PORT", "KINDLAST_REDIS_PORT",
		"REDIS_ADDR", "PG_SUPER_URL", "PG_MIGRATOR_URL", "PG_APP_URL",
		"PG_AGENT_URL", "PG_BILLING_URL", "PG_INGEST_URL",
	} {
		t.Setenv(name, "")
	}
}
