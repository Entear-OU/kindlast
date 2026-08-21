package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/workers/internal/config"
)

// The worker half's settings (ENT-256). Two properties: no engine address
// means no worker and no complaint, because a gateway-only deployment is a
// supported shape; an engine address with no way to call core-api fails at
// startup and names the setting, because that worker's every activity would
// fail and the operator should hear it before the first schedule does.

func TestNoEngineAddressMeansNoWorkerAndNoError(t *testing.T) {
	t.Setenv("KINDLAST_GATEWAY_TOKEN", secret)
	t.Setenv("KINDLAST_TEMPORAL_ADDR", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Temporal.Addr != "" {
		t.Errorf("Addr = %q, want empty", cfg.Temporal.Addr)
	}
	// The defaults are still there, so a deployment that turns the worker on
	// later gets the documented values rather than empty strings.
	if cfg.Temporal.TaskQueue != "core" || cfg.Temporal.Namespace != "default" {
		t.Errorf("defaults = %q/%q, want core/default", cfg.Temporal.TaskQueue, cfg.Temporal.Namespace)
	}
	if cfg.Temporal.SnoozeExpirySchedule != "10 * * * *" {
		t.Errorf("snooze expiry cron = %q, want hourly at ten past", cfg.Temporal.SnoozeExpirySchedule)
	}
}

func TestAnEngineAddressWithNoWayToCallCoreAPIIsRefused(t *testing.T) {
	t.Setenv("KINDLAST_GATEWAY_TOKEN", secret)
	t.Setenv("KINDLAST_TEMPORAL_ADDR", "temporal:7233")

	// Each missing piece, in the order Load checks them, and each error names
	// the setting that would fix it.
	for _, step := range []struct {
		name, wants string
		set        func()
	}{
		{"no core-api URL", "KINDLAST_CORE_API_URL", func() {}},
		{"no issuer", "KINDLAST_OIDC_ISSUER", func() {
			t.Setenv("KINDLAST_CORE_API_URL", "http://edge:80")
		}},
		{"no audience", "KINDLAST_OIDC_AUDIENCE", func() {
			t.Setenv("KINDLAST_OIDC_ISSUER", "http://localhost:8300")
		}},
		{"no client credential", "KINDLAST_INTERNAL_CLIENT_FILE", func() {
			t.Setenv("KINDLAST_OIDC_AUDIENCE", "386963854241824775")
		}},
	} {
		step.set()
		_, err := config.Load()
		if err == nil {
			t.Fatalf("%s: Load succeeded, want a refusal", step.name)
		}
		if !strings.Contains(err.Error(), step.wants) {
			t.Errorf("%s: the error does not name %s: %v", step.name, step.wants, err)
		}
	}
}

func TestTheClientCredentialAndAudienceAreReadFromTheSeedsFiles(t *testing.T) {
	t.Setenv("KINDLAST_GATEWAY_TOKEN", secret)
	t.Setenv("KINDLAST_TEMPORAL_ADDR", "temporal:7233")
	t.Setenv("KINDLAST_CORE_API_URL", "http://edge:80")
	t.Setenv("KINDLAST_OIDC_ISSUER", "http://localhost:8300")

	dir := t.TempDir()
	// The shapes the seed writes: the audience as a bare id, the credential as
	// Zitadel's own machine-key JSON, read as written rather than reshaped.
	audience := filepath.Join(dir, "core-api-audience.txt")
	if err := os.WriteFile(audience, []byte("386963854241824775\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := filepath.Join(dir, "core-api-client.json")
	if err := os.WriteFile(client, []byte(`{"clientId": "core-api-client", "clientSecret": "s3cret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KINDLAST_OIDC_AUDIENCE_FILE", audience)
	t.Setenv("KINDLAST_INTERNAL_CLIENT_FILE", client)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Temporal.OIDCAudience != "386963854241824775" {
		t.Errorf("audience = %q", cfg.Temporal.OIDCAudience)
	}
	if cfg.Temporal.ClientID != "core-api-client" || cfg.Temporal.ClientSecret != "s3cret" {
		t.Errorf("credential = %q/%q", cfg.Temporal.ClientID, cfg.Temporal.ClientSecret)
	}
}
