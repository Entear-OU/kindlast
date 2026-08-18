package integrations_test

import (
	"errors"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/integrations"
)

func offered() []integrations.Tool {
	return []integrations.Tool{
		{Name: "search_tickets", WriteCapable: false},
		{Name: "close_ticket", WriteCapable: true},
		{Name: "delete_queue", WriteCapable: true},
	}
}

// A tool becomes granted because it was named in the request, and for no other
// reason.
//
// THE CASE THAT MATTERS IS THE SECOND ONE. A caller sending the discovered
// tools back with `Granted: true` already set is the obvious way to grant a
// write without ticking it, and it is what a lazily written console would do
// by round-tripping the discovery response.
func TestAToolIsGrantedOnlyByBeingNamed(t *testing.T) {
	resolved, err := integrations.ResolveGrants(offered(), []string{"search_tickets"})
	if err != nil {
		t.Fatalf("ResolveGrants: %v", err)
	}
	if names := integrations.GrantedNames(resolved); len(names) != 1 || names[0] != "search_tickets" {
		t.Fatalf("granted %v, want only search_tickets", names)
	}

	// Every tool arrives already flagged, and none is named.
	preFlagged := offered()
	for i := range preFlagged {
		preFlagged[i].Granted = true
	}
	resolved, err = integrations.ResolveGrants(preFlagged, nil)
	if err != nil {
		t.Fatalf("ResolveGrants: %v", err)
	}
	if names := integrations.GrantedNames(resolved); len(names) != 0 {
		t.Fatalf("granted %v from a request that named nothing", names)
	}
}

// A grant naming a tool the connection does not offer is an error rather than
// a silent omission.
//
// Ignoring unknown names is one line shorter and never fails, and it is how a
// console that has drifted out of step with an endpoint silently grants
// nothing while showing a tick.
func TestGrantingAToolThatWasNeverOfferedIsAnError(t *testing.T) {
	_, err := integrations.ResolveGrants(offered(), []string{"search_tickets", "drop_database"})
	if !errors.Is(err, integrations.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

// The write-granted list is DERIVED from the granted tools rather than sent,
// so a caller cannot widen what it may write by sending a longer list.
func TestTheWriteGrantsAreDerivedFromWhatWasGranted(t *testing.T) {
	resolved, err := integrations.ResolveGrants(offered(),
		[]string{"search_tickets", "close_ticket"})
	if err != nil {
		t.Fatalf("ResolveGrants: %v", err)
	}

	writes := integrations.WriteGrants(resolved)
	if len(writes) != 1 || writes[0] != "close_ticket" {
		t.Fatalf("write grants are %v, want only close_ticket", writes)
	}

	// `delete_queue` writes and was not granted, so it appears in neither
	// list. A write grant for a tool that is not granted at all would be a
	// permission with nothing under it.
	if _, found := findName(integrations.GrantedNames(resolved), "delete_queue"); found {
		t.Error("an ungranted tool appears in the granted list")
	}
	if _, found := findName(writes, "delete_queue"); found {
		t.Error("an ungranted tool appears in the write-granted list")
	}
}

// The endpoint check refuses what cannot be stored, and deliberately does not
// duplicate the egress allow-list.
func TestTheEndpointCheckRefusesWhatCannotBeStored(t *testing.T) {
	for name, endpoint := range map[string]string{
		"empty":       "",
		"no scheme":   "tools.example.com/mcp",
		"file scheme": "file:///etc/passwd",
		"no host":     "https:///mcp",
		"not a URL":   "http://[::1",
	} {
		if err := integrations.ValidateEndpoint(endpoint); !errors.Is(err, integrations.ErrInvalid) {
			t.Errorf("%s (%q): got %v, want a refusal", name, endpoint, err)
		}
	}

	// A host nobody's allow-list would permit is still VALID here, and that is
	// the point rather than a gap: whether an address may be reached is the
	// gateway's decision, made against the deployment's allow-list on every
	// call. Deciding it here as well would be a second answer to the same
	// question, checked at insert rather than at use, so a host withdrawn from
	// the allow-list would stay reachable for every connection already stored.
	if err := integrations.ValidateEndpoint("http://169.254.169.254/mcp"); err != nil {
		t.Errorf("the endpoint check is duplicating the egress allow-list: %v", err)
	}
}

func TestTheNameCheckRefusesWhatAConsoleCannotShow(t *testing.T) {
	if err := integrations.ValidateDisplayName("   "); !errors.Is(err, integrations.ErrInvalid) {
		t.Errorf("a blank name was accepted: %v", err)
	}
	if err := integrations.ValidateDisplayName(longName(121)); !errors.Is(err, integrations.ErrInvalid) {
		t.Errorf("a 121-character name was accepted: %v", err)
	}
	if err := integrations.ValidateDisplayName("Helpdesk"); err != nil {
		t.Errorf("an ordinary name was refused: %v", err)
	}
}

func findName(list []string, want string) (int, bool) {
	for i, item := range list {
		if item == want {
			return i, true
		}
	}
	return 0, false
}

func longName(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = 'x'
	}
	return string(out)
}
