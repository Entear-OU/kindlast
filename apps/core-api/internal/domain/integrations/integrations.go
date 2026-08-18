// Package integrations holds the rules about connecting a customer's systems
// that are decisions rather than invariants (ENT-231, §26.4).
//
// The split AGENTS.md draws applies cleanly here. What must hold no matter who
// writes is in 00025: one open grant per tool, a write flag the application
// cannot edit, a consent record nobody can amend. What decides is here: which
// tools a request may grant, what a connection is allowed to look like, and
// how a fetch turns into an observation.
package integrations

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Kind is what sort of system a connection reaches.
const KindMCP = "mcp"

// Status values.
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

// Fetch outcomes, matching the constraint on `integration_fetches.outcome`.
//
// # THREE, AND THE MIDDLE ONE IS THE POINT
//
// `refused` is not a kind of failure. It is what a working policy produces,
// and a vocabulary offering only success and failure would push refusals into
// one of them and lose the distinction a customer most needs: the difference
// between "we decided not to" and "it broke". The same argument
// `AGENT_RUN_OUTCOME_REFUSED` rests on in 00019.
const (
	OutcomeSucceeded = "succeeded"
	OutcomeRefused   = "refused"
	OutcomeFailed    = "failed"
)

// Tool is one tool a connection exposes.
type Tool struct {
	Name         string
	Description  string
	WriteCapable bool
	Granted      bool
}

// Connection is one customer system, as stored.
//
// There is no credential field, and its absence is deliberate rather than an
// oversight. Nothing that reads a connection for display should be able to
// carry a credential by accident, so the sealed value is fetched by the one
// query that needs it and never travels attached to the thing a console
// renders.
type Connection struct {
	ID          string
	Kind        string
	DisplayName string
	EndpointURL string
	Status      string
	CreatedAt   time.Time
	RevokedAt   *time.Time
	Tools       []Tool
	ConsentedAt *time.Time
	ConsentedBy string
}

// Fetch is one attempt, whatever became of it.
type Fetch struct {
	ID              string
	IntegrationID   string
	IntegrationName string
	Tool            string
	Outcome         string
	Detail          string
	RequestedAt     time.Time
	FinishedAt      time.Time
	EvidenceID      string
	Redactions      int32
	RequestedBy     string
}

var (
	// ErrInvalid is a caller's mistake: a bad name, a bad URL, a tool that was
	// never offered.
	ErrInvalid = errors.New("that connection cannot be recorded as described")

	// ErrRevoked is what every operation on a revoked connection returns.
	//
	// Separate from ErrInvalid because it is not a mistake in the request: the
	// request would have been fine yesterday, and the caller needs to be told
	// what changed rather than to look for a typo.
	ErrRevoked = errors.New("that connection has been revoked")
)

// ValidateEndpoint refuses a URL that is not one this product can store.
//
// # THIS IS NOT THE EGRESS CHECK AND MUST NOT BECOME IT
//
// Whether a host may be reached is the gateway's decision, made against the
// deployment's allow-list, on every call. Copying that list here would be a
// second answer to the same question, free to drift, and it would be the
// answer checked at insert rather than at use, so a host withdrawn from the
// allow-list would stay reachable for every connection already stored.
//
// What this checks is narrower and is genuinely core-api's: the string has to
// be a URL, with a scheme this product speaks and a host, so that a row cannot
// hold something that can only ever fail.
func ValidateEndpoint(endpoint string) error {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return fmt.Errorf("%w: it needs an address", ErrInvalid)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("%w: %q is not a URL", ErrInvalid, trimmed)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("%w: %q is not an address this product can reach", ErrInvalid, trimmed)
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("%w: %q names no host", ErrInvalid, trimmed)
	}
	return nil
}

// ValidateDisplayName refuses a name a console cannot show.
func ValidateDisplayName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("%w: it needs a name", ErrInvalid)
	}
	if len(trimmed) > 120 {
		return fmt.Errorf("%w: that name is longer than 120 characters", ErrInvalid)
	}
	return nil
}

// ResolveGrants decides which of the offered tools a grant request may name.
//
// # A GRANT FOR A TOOL THAT WAS NEVER OFFERED IS AN ERROR, NOT AN OMISSION
//
// The tempting implementation ignores unknown names, because it is one line
// shorter and never fails. It is also how a console that has drifted out of
// step with the endpoint silently grants nothing while showing a tick, and how
// a caller that misspells a tool name gets a connection that never works with
// no explanation available anywhere.
//
// So an unknown name is refused and the message says which, because the person
// who sees it is the one who can fix it.
func ResolveGrants(offered []Tool, granted []string) ([]Tool, error) {
	wanted := make(map[string]struct{}, len(granted))
	for _, name := range granted {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		wanted[trimmed] = struct{}{}
	}

	known := make(map[string]struct{}, len(offered))
	for _, tool := range offered {
		known[tool.Name] = struct{}{}
	}
	for name := range wanted {
		if _, ok := known[name]; !ok {
			return nil, fmt.Errorf("%w: that connection offers no tool called %q", ErrInvalid, name)
		}
	}

	out := make([]Tool, 0, len(offered))
	for _, tool := range offered {
		_, isGranted := wanted[tool.Name]
		// GRANTED IS ASSIGNED HERE AND NEVER INHERITED. A tool arriving from a
		// caller with `Granted: true` set does not become granted; only its
		// presence in the request's list does. That closes the obvious way to
		// grant a tool without ticking it, which is to send it back with the
		// flag already on.
		tool.Granted = isGranted
		out = append(out, tool)
	}
	return out, nil
}

// WriteGrants returns the names of the granted tools that write.
//
// The second list the gateway's policy carries. Derived here rather than sent
// by a client, so a caller cannot widen what it may write by sending a longer
// list than the one it was granted.
func WriteGrants(tools []Tool) []string {
	var names []string
	for _, tool := range tools {
		if tool.Granted && tool.WriteCapable {
			names = append(names, tool.Name)
		}
	}
	return names
}

// GrantedNames returns the names of every granted tool.
func GrantedNames(tools []Tool) []string {
	var names []string
	for _, tool := range tools {
		if tool.Granted {
			names = append(names, tool.Name)
		}
	}
	return names
}

// Find returns the named tool.
func Find(tools []Tool, name string) (Tool, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}
