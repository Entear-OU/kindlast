// Package config loads the gateway's settings from the environment and fails
// closed on anything missing.
//
// The same shape as core-api's config package, and for the same reason (§18.7):
// every value here is either an address this service must not guess at or a
// security parameter, and a default for one of those is how a control quietly
// stops being one.
//
// Two settings deserve their defaults being read carefully rather than
// skimmed, and both are commented below: the egress allow-list defaults to
// empty, which permits nothing, and private destinations default to refused.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config is everything the gateway needs to start.
type Config struct {
	// ListenAddr is the internal listener. There is no public one: the gateway
	// publishes no port and is reachable only on the compose network, which is
	// what stops anybody outside it presenting the shared secret at all.
	ListenAddr string

	// SharedSecret is what core-api presents as a bearer token.
	//
	// Required, and the service does not start without it. A gateway serving
	// with no secret would accept whatever arrived, and what arrives is a
	// request to reach a customer's system with a customer's credential.
	//
	// Read through fileOrValue, because a shared secret is exactly the sort of
	// value an operator mounts as a file rather than putting in an environment
	// variable that `docker inspect` prints.
	SharedSecret string

	// EgressAllowList is the comma-separated set of hosts this deployment may
	// dial, with a leading dot for "and every subdomain".
	//
	// EMPTY PERMITS NOTHING, AND THAT IS THE POINT. An operator who has not
	// thought about which hosts their gateway may reach gets a gateway that
	// reaches none, which is a support question. The other default is a
	// server-side request forgery available to anybody with an account, which
	// is an incident.
	EgressAllowList string

	// AllowPrivateDestinations lets a permitted host name resolve into a
	// private or loopback address.
	//
	// False by default, which is right for a hosted deployment: without it,
	// anybody who can type a URL can point the gateway at the instance
	// metadata service, and the allow-list is the only thing between them and
	// a set of cloud credentials.
	//
	// True is the ordinary self-hosted setting and the bundled compose stack
	// sets it, because a self-hoster's MCP server is on their own network by
	// definition and every compose service name resolves into 172.16/12.
	AllowPrivateDestinations bool

	// OutboundTimeout bounds one call to a customer's endpoint.
	//
	// A bound rather than a tuning knob: without it a slow server holds a
	// goroutine, and enough slow servers hold all of them. This is what
	// Temporal's schedule-to-close timeout becomes at step 8.
	OutboundTimeout time.Duration

	// RateLimitBurst and RateLimitWindow are the per-organisation budget: how
	// many gateway calls at once, refilling to full over the window.
	RateLimitBurst  int
	RateLimitWindow time.Duration
}

// Load reads the environment.
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:               valueOr("KINDLAST_GATEWAY_LISTEN", ":8100"),
		SharedSecret:             fileOrValue("KINDLAST_GATEWAY_TOKEN"),
		EgressAllowList:          strings.TrimSpace(os.Getenv("KINDLAST_GATEWAY_EGRESS_ALLOWLIST")),
		AllowPrivateDestinations: truthy(os.Getenv("KINDLAST_GATEWAY_ALLOW_PRIVATE_DESTINATIONS")),
		OutboundTimeout:          durationOr("KINDLAST_GATEWAY_OUTBOUND_TIMEOUT", 30*time.Second),
		RateLimitBurst:           intOr("KINDLAST_GATEWAY_RATE_BURST", 30),
		RateLimitWindow:          durationOr("KINDLAST_GATEWAY_RATE_WINDOW", time.Minute),
	}

	if strings.TrimSpace(cfg.SharedSecret) == "" {
		return nil, errors.New(
			"config: KINDLAST_GATEWAY_TOKEN (or KINDLAST_GATEWAY_TOKEN_FILE) must be set; " +
				"a gateway with no shared secret accepts whatever arrives")
	}

	// A short secret is worse than none, because it reads as configured. The
	// bound is deliberately low enough that a development value passes and
	// high enough that a guessable one does not.
	if len(cfg.SharedSecret) < 16 {
		return nil, fmt.Errorf(
			"config: KINDLAST_GATEWAY_TOKEN is %d characters; it must be at least 16",
			len(cfg.SharedSecret))
	}

	// NOT AN ERROR. A gateway with an empty allow-list starts and refuses
	// every fetch, which is a working deployment of a feature nobody has
	// configured yet. Refusing to start would take down a stack over a setting
	// its operator may not use.
	//
	// The caller logs it loudly at boot; see main.
	return cfg, nil
}

// fileOrValue reads NAME, falling back to the contents of the file named by
// NAME_FILE.
//
// The same shape core-api uses for the billing webhook's signing secret, and
// for the same reason: the values an operator most wants out of the process
// environment are exactly the ones a compose file cannot carry.
func fileOrValue(name string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	path := strings.TrimSpace(os.Getenv(name + "_FILE"))
	if path == "" {
		return ""
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		// Not fatal here, and reported by Load as a missing value, which is
		// the same message an operator gets for any other unset setting rather
		// than a stack trace about a file they may not know is involved.
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

// durationOr parses a Go duration, falling back on anything unparseable.
//
// A fallback rather than an error, unlike the secret above, because the
// failure mode is different in kind: a mistyped timeout produces a working
// service with a different bound, where a missing secret produces an open
// door. Logged by the caller so a typo is visible rather than silent.
func durationOr(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func intOr(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(raw, "%d", &parsed); err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

// truthy reads a boolean environment variable.
//
// Only the four affirmative spellings count, and anything else including an
// unset variable is false. A typo therefore leaves private destinations
// REFUSED rather than permitted, which is the safe direction for this
// particular flag.
func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
