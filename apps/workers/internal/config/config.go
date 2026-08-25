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
	"encoding/json"
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

	// Temporal is the worker half of this binary (ENT-256): the schedules that
	// used to be pg_cron jobs and Vercel cron routes, run as workflows whose
	// activities call core-api.
	//
	// EMPTY MEANS NO WORKER, AND THAT IS A SUPPORTED CONFIGURATION rather than
	// a degraded one, the same way core-api serves no SweepService without an
	// agent pool: a deployment that runs only the gateway half leaves this
	// unset and nothing here tries to reach an engine that is not there. The
	// bundled compose stack sets it.
	Temporal Temporal
}

// Temporal is how the worker reaches the engine and core-api.
type Temporal struct {
	// Addr is the frontend, host:port. Empty disables the worker.
	Addr string
	// Namespace the schedules and workflows live in. The default namespace is
	// the one auto-setup creates with the configured retention.
	Namespace string
	// TaskQueue this process polls. `core` is the Go queue of the design's
	// two (§16.4); the Python service polls `intelligence`.
	TaskQueue string

	// SnoozeExpirySchedule is the cron expression for bringing deferred
	// findings back. Hourly at ten past by default: pg_cron ran the same
	// function once a day at 06:10, which meant a finding deferred "for seven
	// days" came back up to a day late. An hour is the granularity a person
	// notices, and the pass is one cheap UPDATE.
	SnoozeExpirySchedule string

	// OutboxRelayInterval is how often the relay asks core-api what mail is
	// pending and starts a delivery workflow for each row (ENT-256, part
	// three). Fifteen seconds by default: an invitation is expected in the
	// recipient's inbox within a minute or so, not within a second, and each
	// tick is one indexed query against a partial index sized to the backlog,
	// which on an idle deployment is empty. It is the one interval here rather
	// than a cron, because a cron's finest grain is a minute.
	OutboxRelayInterval time.Duration

	// OutboxReclaimSchedule is the cron expression for clearing personal data
	// out of delivered and abandoned messages (ENT-242). Hourly at forty past
	// by default: every window the pass applies is measured in days, so more
	// often buys an accuracy nobody can perceive, and daily would leave a
	// spent token in the clear for most of a day after its invitation was
	// accepted. Offset from the snooze expiry so the two passes do not share a
	// minute on the producer pool.
	OutboxReclaimSchedule string

	// SweepRelayInterval is how often the relay looks for sweeps somebody
	// asked for, which today means an onboarding somebody just confirmed
	// (ENT-256, part four). Fifteen seconds, like the outbox relay, because
	// the person is looking at an empty feed.
	SweepRelayInterval time.Duration

	// SweepSchedule is the cron expression for sweeping every organisation
	// with a profile: the Watcher and then the Analyst, one organisation at a
	// time. Daily at 06:00 UTC by default, which is what pg_cron's
	// `watcher-daily` ran; the Analyst no longer needs its own 06:05 because
	// it is the next step in the same workflow.
	SweepSchedule string

	// ExecutorRelayInterval is how often the relay looks for approvals whose
	// record has not been created yet (ENT-271). Fifteen seconds, like the
	// other two relays: somebody clicked approve and is looking for what
	// they approved.
	ExecutorRelayInterval time.Duration

	// FetchRelayInterval is how often the relay asks core-api which
	// connections have evidence that has gone stale (ENT-279).
	//
	// NOT HOW OFTEN A CUSTOMER IS DIALLED. That is core-api's staleness
	// constant, which is deliberately not a request field: a caller able to
	// say "everything is stale" would be a caller able to dial every
	// customer's systems at once. This only sets how often the question is
	// asked.
	//
	// Hourly rather than the fifteen seconds the other three relays use,
	// because nothing here is a person waiting: the other relays exist so that
	// somebody staring at a screen sees their sweep or their record. This one
	// collects evidence for a sweep that runs tomorrow morning. Hourly is what
	// gets a newly granted tool its first observation within the hour instead
	// of at six tomorrow, and what spreads an estate's fetches across the day
	// instead of dialling every customer at once.
	FetchRelayInterval time.Duration

	// CoreAPIURL is where the activities call. Through the edge on the bundled
	// stack, the same door Intelligence uses, so there is no
	// development-only shortcut.
	CoreAPIURL string

	// The credential the activities present to core-api. The same OIDC client
	// file Intelligence and core-api read (`core-api-client.json`, written by
	// the seed), because both halves are the same service principal and a
	// second client would be a second thing to grant `internal:ingest` to, to
	// rotate, and to forget. Required when Addr is set: a worker that cannot
	// authenticate is not a degraded worker, it is one whose every activity
	// fails.
	OIDCIssuer       string
	OIDCDiscoveryURL string
	OIDCHostHeader   string
	OIDCAudience     string
	ClientID         string
	ClientSecret     string
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
		Temporal: Temporal{
			Addr:                  strings.TrimSpace(os.Getenv("KINDLAST_TEMPORAL_ADDR")),
			Namespace:             valueOr("KINDLAST_TEMPORAL_NAMESPACE", "default"),
			TaskQueue:             valueOr("KINDLAST_TEMPORAL_TASK_QUEUE", "core"),
			SnoozeExpirySchedule:  valueOr("KINDLAST_SNOOZE_EXPIRY_SCHEDULE", "10 * * * *"),
			OutboxRelayInterval:   durationOr("KINDLAST_OUTBOX_RELAY_INTERVAL", 15*time.Second),
			OutboxReclaimSchedule: valueOr("KINDLAST_OUTBOX_RECLAIM_SCHEDULE", "40 * * * *"),
			SweepRelayInterval:    durationOr("KINDLAST_SWEEP_RELAY_INTERVAL", 15*time.Second),
			SweepSchedule:         valueOr("KINDLAST_SWEEP_SCHEDULE", "0 6 * * *"),
			ExecutorRelayInterval: durationOr("KINDLAST_EXECUTOR_RELAY_INTERVAL", 15*time.Second),
			FetchRelayInterval:    durationOr("KINDLAST_FETCH_RELAY_INTERVAL", time.Hour),
			CoreAPIURL:            strings.TrimSpace(os.Getenv("KINDLAST_CORE_API_URL")),
			OIDCIssuer:            strings.TrimSpace(os.Getenv("KINDLAST_OIDC_ISSUER")),
			OIDCDiscoveryURL:      strings.TrimSpace(os.Getenv("KINDLAST_OIDC_DISCOVERY_URL")),
			OIDCHostHeader:        strings.TrimSpace(os.Getenv("KINDLAST_OIDC_HOST_HEADER")),
			OIDCAudience:          audience(),
		},
	}
	cfg.Temporal.ClientID, cfg.Temporal.ClientSecret = internalClient()

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

	// The worker half fails closed on what it cannot do without. An engine
	// address with no way to call core-api is a worker whose every activity
	// would fail, and a misconfiguration somebody meant to get right should be
	// heard about at startup rather than at the first schedule.
	if t := cfg.Temporal; t.Addr != "" {
		switch {
		case t.CoreAPIURL == "":
			return nil, errors.New("config: KINDLAST_TEMPORAL_ADDR is set but KINDLAST_CORE_API_URL is not; " +
				"the worker's activities call core-api and need to know where")
		case t.OIDCIssuer == "":
			return nil, errors.New("config: KINDLAST_TEMPORAL_ADDR is set but KINDLAST_OIDC_ISSUER is not; " +
				"the worker mints a token to call core-api and needs the issuer")
		case t.OIDCAudience == "":
			return nil, errors.New("config: KINDLAST_TEMPORAL_ADDR is set but no audience is configured " +
				"(KINDLAST_OIDC_AUDIENCE or KINDLAST_OIDC_AUDIENCE_FILE); the token core-api accepts is scoped to it")
		case t.ClientID == "" || t.ClientSecret == "":
			return nil, errors.New("config: KINDLAST_TEMPORAL_ADDR is set but no client credential is configured " +
				"(KINDLAST_INTERNAL_CLIENT_FILE, or KINDLAST_INTERNAL_CLIENT_ID and _SECRET); " +
				"the worker presents it to core-api")
		}
	}
	return cfg, nil
}

// audience reads KINDLAST_OIDC_AUDIENCE, or the file KINDLAST_OIDC_AUDIENCE_FILE
// names. The same two spellings core-api and Intelligence accept, because the
// value is Zitadel's project id, generated by the seed and written to the
// shared volume rather than known in advance.
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
		return ""
	}
	return strings.TrimSpace(string(contents))
}

// internalClient reads the OIDC client credential, from the two environment
// variables or from the JSON file Zitadel's machine-key output produces.
//
// The field names are the authorization server's, not ours: this is
// `core-api-client.json` read as the seed wrote it, the same file and the same
// shape core-api's config reads, so the two cannot drift.
func internalClient() (id, secret string) {
	id = strings.TrimSpace(os.Getenv("KINDLAST_INTERNAL_CLIENT_ID"))
	secret = strings.TrimSpace(os.Getenv("KINDLAST_INTERNAL_CLIENT_SECRET"))
	if id != "" && secret != "" {
		return id, secret
	}
	path := strings.TrimSpace(os.Getenv("KINDLAST_INTERNAL_CLIENT_FILE"))
	if path == "" {
		return id, secret
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return id, secret
	}
	var credential struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(contents, &credential); err != nil {
		return id, secret
	}
	if id == "" {
		id = strings.TrimSpace(credential.ClientID)
	}
	if secret == "" {
		secret = strings.TrimSpace(credential.ClientSecret)
	}
	return id, secret
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
