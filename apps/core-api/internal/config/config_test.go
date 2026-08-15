package config_test

import (
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/config"
)

// The claim name Zitadel actually emits carries the project id:
//
//	urn:zitadel:iam:org:project:386092727885889542:roles
//
// compose shipped `urn:zitadel:iam:org:project:roles`, with no id, so core-api
// watched a claim that never arrives and every human token read as carrying no
// scopes. That is ENT-221's second cause, and it was invisible because an empty
// scope set and a wrong claim name look identical from inside: both produce a
// 403 that says the token lacks the scope.
//
// These tests pin the expansion rather than the discovery, because the
// discovery is recorded on the issue and what has to keep working is that
// `{audience}` becomes the audience.

func load(t *testing.T, env map[string]string) *config.Config {
	t.Helper()

	// The minimum Load() accepts, so the test is about scope claims and not
	// about which other settings happen to be required today.
	base := map[string]string{
		"KINDLAST_OIDC_ISSUER":   "http://localhost:8300",
		"KINDLAST_OIDC_AUDIENCE": "386092727885889542",
		"KINDLAST_DATABASE_URL":  "postgres://kindlast_app@localhost/kindlast",
		"KINDLAST_REDIS_ADDR":    "localhost:6379",
	}
	for k, v := range env {
		base[k] = v
	}
	for k, v := range base {
		t.Setenv(k, v)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	return cfg
}

func TestTheAudiencePlaceholderIsExpanded(t *testing.T) {
	cfg := load(t, map[string]string{
		"KINDLAST_OIDC_SCOPE_CLAIMS": "urn:zitadel:iam:org:project:{audience}:roles",
	})

	want := "urn:zitadel:iam:org:project:386092727885889542:roles"
	if len(cfg.OIDCScopeClaims) != 1 || cfg.OIDCScopeClaims[0] != want {
		t.Fatalf("got %v, want [%s]", cfg.OIDCScopeClaims, want)
	}
}

func TestAClaimWithNoPlaceholderIsUntouched(t *testing.T) {
	cfg := load(t, map[string]string{
		"KINDLAST_OIDC_SCOPE_CLAIMS": "roles,groups",
	})

	if len(cfg.OIDCScopeClaims) != 2 ||
		cfg.OIDCScopeClaims[0] != "roles" || cfg.OIDCScopeClaims[1] != "groups" {
		t.Fatalf("got %v, want [roles groups]", cfg.OIDCScopeClaims)
	}
}

func TestBothFormsSurviveTogether(t *testing.T) {
	// A deployment may assert roles in a vendor claim while emitting `scope`
	// for the basics, so the list is not all-or-nothing.
	cfg := load(t, map[string]string{
		"KINDLAST_OIDC_SCOPE_CLAIMS": "roles,urn:zitadel:iam:org:project:{audience}:roles",
	})

	if len(cfg.OIDCScopeClaims) != 2 {
		t.Fatalf("got %v, want two claims", cfg.OIDCScopeClaims)
	}
	if cfg.OIDCScopeClaims[1] != "urn:zitadel:iam:org:project:386092727885889542:roles" {
		t.Fatalf("second claim not expanded: %q", cfg.OIDCScopeClaims[1])
	}
}
