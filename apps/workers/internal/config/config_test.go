package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/workers/internal/config"
)

const secret = "a-development-only-gateway-secret"

// The two defaults in this package that would be catastrophic the other way
// round, asserted rather than left to the comments beside them.

func TestTheGatewayDoesNotStartWithoutASharedSecret(t *testing.T) {
	t.Setenv("KINDLAST_GATEWAY_TOKEN", "")

	_, err := config.Load()
	if err == nil {
		t.Fatal("the gateway loaded a configuration with no shared secret")
	}
	if !strings.Contains(err.Error(), "KINDLAST_GATEWAY_TOKEN") {
		t.Errorf("the error does not name the setting: %v", err)
	}
}

// A short secret is worse than none, because it reads as configured.
func TestAGuessableSecretIsRefused(t *testing.T) {
	t.Setenv("KINDLAST_GATEWAY_TOKEN", "letmein")

	if _, err := config.Load(); err == nil {
		t.Fatal("a seven-character shared secret was accepted")
	}
}

func TestPrivateDestinationsAreRefusedUnlessTurnedOn(t *testing.T) {
	t.Setenv("KINDLAST_GATEWAY_TOKEN", secret)

	// Unset.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AllowPrivateDestinations {
		t.Error("private destinations are permitted by default")
	}

	// A typo, which must not read as affirmative. This is the direction that
	// matters: a mistyped flag leaves the safer setting in place.
	t.Setenv("KINDLAST_GATEWAY_ALLOW_PRIVATE_DESTINATIONS", "ture")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AllowPrivateDestinations {
		t.Error("a mistyped flag read as affirmative")
	}

	t.Setenv("KINDLAST_GATEWAY_ALLOW_PRIVATE_DESTINATIONS", "true")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.AllowPrivateDestinations {
		t.Error("the flag does not turn on when it is set")
	}
}

// An unconfigured allow-list is a startable deployment that refuses every
// fetch, not a refusal to start.
//
// Refusing to start would take down a stack over a setting its operator may
// never use. The loud warning is main's job; what is asserted here is that
// Load does not turn it into a crash.
func TestAnEmptyAllowListIsNotAStartupFailure(t *testing.T) {
	t.Setenv("KINDLAST_GATEWAY_TOKEN", secret)
	t.Setenv("KINDLAST_GATEWAY_EGRESS_ALLOWLIST", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load refused to start over an empty allow-list: %v", err)
	}
	if cfg.EgressAllowList != "" {
		t.Errorf("EgressAllowList is %q", cfg.EgressAllowList)
	}
}

// The secret can be mounted as a file, because an operator should not have to
// put one in an environment variable that `docker inspect` prints.
func TestTheSecretCanBeMountedAsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway-token")
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	t.Setenv("KINDLAST_GATEWAY_TOKEN", "")
	t.Setenv("KINDLAST_GATEWAY_TOKEN_FILE", path)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Trailing newline trimmed. A secret read with the newline attached
	// compares unequal to the same secret from an environment variable, which
	// is a bug that only shows up between two deployment styles.
	if cfg.SharedSecret != secret {
		t.Errorf("SharedSecret is %q", cfg.SharedSecret)
	}
}

// A mistyped duration falls back rather than failing, unlike the secret, and
// the asymmetry is deliberate: a mistyped timeout produces a working service
// with a different bound, where a missing secret produces an open door.
func TestAMistypedDurationFallsBack(t *testing.T) {
	t.Setenv("KINDLAST_GATEWAY_TOKEN", secret)
	t.Setenv("KINDLAST_GATEWAY_OUTBOUND_TIMEOUT", "thirty seconds")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OutboundTimeout != 30*time.Second {
		t.Errorf("OutboundTimeout is %v, want the default", cfg.OutboundTimeout)
	}

	t.Setenv("KINDLAST_GATEWAY_OUTBOUND_TIMEOUT", "5s")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OutboundTimeout != 5*time.Second {
		t.Errorf("OutboundTimeout is %v, want 5s", cfg.OutboundTimeout)
	}
}

// NO THIRD-PARTY CREDENTIAL IS CONFIGURED HERE, AND THAT ABSENCE IS THE
// PROPERTY.
//
// The gateway is handed a customer's credential per call and holds none at
// rest. A settings field for one would mean a deployment-wide credential
// shared across tenants, which is the shape this whole design exists to avoid.
func TestNoThirdPartyCredentialIsPartOfTheGatewaysConfiguration(t *testing.T) {
	t.Setenv("KINDLAST_GATEWAY_TOKEN", secret)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Reflection rather than a field-by-field read, so a field added later is
	// caught by this test rather than by nobody.
	for _, field := range fieldNames(cfg) {
		lower := strings.ToLower(field)
		for _, forbidden := range []string{"oauth", "clientsecret", "apikey", "accesstoken", "refreshtoken"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("config.Config has a field %q; the gateway holds no third-party credential", field)
			}
		}
	}
}

// fieldNames lists the exported fields of a struct pointer.
func fieldNames(value any) []string {
	typ := reflect.TypeOf(value).Elem()
	names := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		names = append(names, typ.Field(i).Name)
	}
	return names
}
