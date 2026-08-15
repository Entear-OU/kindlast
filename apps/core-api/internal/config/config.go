// Package config loads core-api's settings from the environment and fails
// closed on anything missing.
//
// Failing closed matters more here than the usual argument about typos
// (§18.7). Every value below is either an address of something this service
// must not guess at, or a security parameter. A default for the audience would
// be the worst of them: a resource server that falls back to accepting some
// convenient audience accepts tokens minted for another service, which is the
// replay §1.4 spends a paragraph on.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Config is everything core-api needs to start.
type Config struct {
	// ListenAddr is the internal listener. There is no public one: core-api
	// binds no published port and is reachable only on the compose network,
	// which is what stops a leaked access token being replayed against it from
	// the internet (§0.4, §1.2).
	ListenAddr string

	// OIDCIssuer is the only thing this service is told about the
	// authorization server. Everything else, including the JWKS endpoint, is
	// discovered, so a self-hoster can point at their own IdP (§18.2).
	OIDCIssuer string

	// OIDCDiscoveryURL is where to fetch the discovery document, when that is
	// not the issuer's own address.
	//
	// Optional, and empty is the ordinary case. It exists because the bundled
	// Zitadel advertises `http://localhost:8300`, which is where a browser
	// reaches it, and core-api is on the compose network where no such address
	// exists. The document must still declare OIDCIssuer, so this changes
	// where configuration is fetched from and never what is trusted.
	OIDCDiscoveryURL string

	// OIDCHostHeader overrides the Host header sent to the authorization
	// server. Zitadel routes by Host, so reaching it at `auth:8080` while it
	// believes it is `localhost:8300` needs this or the request lands on the
	// wrong virtual server.
	OIDCHostHeader string

	// OIDCAudience is the audience this resource server accepts, and only this
	// one.
	//
	// Configured rather than a constant because it is deployment-specific in
	// practice. Against the bundled Zitadel it is the project id, a snowflake
	// integer, not the friendly `kindlast-core-api` the design names: Zitadel
	// puts the project id in `aud` when a token is requested with the
	// project's reserved audience scope. Measured on the running stack, not
	// assumed.
	OIDCAudience string

	// OIDCScopeClaims names any non-standard claims that carry the caller's
	// granted scopes, on top of `scope` and `scp`.
	//
	// Empty by default, which is the RFC 9068 behaviour. It is configurable
	// because the bundled Zitadel does not populate the standard claims at
	// all; see the note on oidc.WithScopeClaims.
	OIDCScopeClaims []string

	// DatabaseURL must name kindlast_app. Not the migrator, not the
	// superuser: both bypass RLS, and the bypass is silent (§14.1).
	DatabaseURL string

	// RedisAddr is the shared instance holding the revocation deny-list.
	RedisAddr string

	// BillingEnabled turns plan gating on. Off by default, and the default is
	// the important half (§18.1).
	//
	// A self-hoster runs the whole product on their own hardware and has no
	// subscription, no provider and no intention of acquiring either. Gating
	// them out of the Executor because they are "on the free plan" would make
	// the self-hosted build a demo, which is not what it is. So gating is
	// something a deployment opts into rather than something it must remember
	// to switch off.
	//
	// An explicit flag rather than inferring it from data. The obvious
	// inference, "no subscription row means self-hosted", is wrong in the
	// direction that costs money: a hosted customer between provisioning and
	// their first subscription row would be indistinguishable from a
	// self-hoster and would get the paid feature free. Deployment shape is
	// deployment configuration, not something to reconstruct from a table.
	BillingEnabled bool
}

// Load reads the environment.
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:       valueOr("KINDLAST_CORE_API_LISTEN", ":8080"),
		OIDCIssuer:       os.Getenv("KINDLAST_OIDC_ISSUER"),
		OIDCDiscoveryURL: os.Getenv("KINDLAST_OIDC_DISCOVERY_URL"),
		OIDCHostHeader:   os.Getenv("KINDLAST_OIDC_HOST_HEADER"),
		OIDCAudience:     audience(),
		OIDCScopeClaims:  splitList(os.Getenv("KINDLAST_OIDC_SCOPE_CLAIMS")),
		DatabaseURL:      os.Getenv("KINDLAST_DATABASE_URL"),
		RedisAddr:        os.Getenv("KINDLAST_REDIS_ADDR"),
		BillingEnabled:   truthy(os.Getenv("KINDLAST_BILLING_ENABLED")),
	}

	var missing []string
	for name, value := range map[string]string{
		"KINDLAST_OIDC_ISSUER":   cfg.OIDCIssuer,
		"KINDLAST_OIDC_AUDIENCE": cfg.OIDCAudience,
		"KINDLAST_DATABASE_URL":  cfg.DatabaseURL,
		"KINDLAST_REDIS_ADDR":    cfg.RedisAddr,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		// All of them at once. Reporting one per start turns a two-minute fix
		// into four restarts.
		return nil, fmt.Errorf("config: these must be set: %s", strings.Join(sorted(missing), ", "))
	}

	if strings.Contains(cfg.DatabaseURL, "kindlast_migrator") ||
		strings.Contains(cfg.DatabaseURL, "postgres:") && strings.Contains(cfg.DatabaseURL, "@") &&
			strings.HasPrefix(cfg.DatabaseURL, "postgres://postgres:") {
		// A guard rather than a comment, because the failure it prevents is
		// invisible: connecting as the migrator or the superuser leaves every
		// policy in place and every one of them a no-op, with no error and no
		// warning, and tenant isolation simply gone (§14.1).
		return nil, errors.New("config: KINDLAST_DATABASE_URL must connect as kindlast_app; " +
			"the migrator and the superuser both bypass row level security")
	}

	return cfg, nil
}

// audience reads the accepted audience from the environment, or from a file
// named by KINDLAST_OIDC_AUDIENCE_FILE.
//
// The file exists for the bundled stack, where the audience is Zitadel's
// project id and therefore not known until the seed job has run. The seed
// writes it to the shared volume the same way it already writes the web
// client's credentials, so compose needs no value baked in and no manual step
// between `up` and a working service. The environment wins where both are
// present, so a real deployment never depends on the file.
func audience() string {
	if value := strings.TrimSpace(os.Getenv("KINDLAST_OIDC_AUDIENCE")); value != "" {
		return value
	}

	path := strings.TrimSpace(os.Getenv("KINDLAST_OIDC_AUDIENCE_FILE"))
	if path == "" {
		return ""
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		// Deliberately not fatal here: Load reports it as a missing value,
		// which is the same message an operator would get for any other
		// unset setting rather than a stack trace about a file they may not
		// know is involved.
		return ""
	}
	return strings.TrimSpace(string(contents))
}

func valueOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	list := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			list = append(list, trimmed)
		}
	}
	return list
}

// truthy reads a boolean environment variable.
//
// Only the four affirmative spellings count, and anything else including an
// unset variable is false. A typo therefore leaves plan gating OFF rather than
// on, which is the safe direction for a flag whose failure mode is refusing a
// paying customer their feature.
func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func sorted(values []string) []string {
	out := append([]string(nil), values...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
