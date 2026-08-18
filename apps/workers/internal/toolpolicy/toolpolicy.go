// Package toolpolicy decides whether one tool call is permitted on one
// connection (ENT-231; OWASP LLM03).
//
// # THE PROPERTY THIS PACKAGE EXISTS FOR
//
// A connection's write-capable tools are unreachable unless explicitly granted
// per connection. That sentence has three parts and each is a separate refusal
// below, because a single combined check is a check somebody eventually
// simplifies.
//
//	unreachable        a tool absent from the granted list is refused, whatever
//	                   the request says about it
//	write-capable      a tool the endpoint marks as writing, or that this
//	                   deployment cannot tell, is treated as writing
//	explicitly granted a write needs its name in the write-granted list, which
//	                   the console only fills from a tick a human made
//
// # WHY THE DECISION IS HERE AND NOT ONLY IN core-api
//
// core-api reads the grants from the database and would not send a call for a
// tool it has not granted, so this looks like a second copy of one check. It
// is a second copy, on purpose.
//
// The gateway is the last thing between this deployment and somebody else's
// system. It is reached over the internal network by a caller it cannot
// inspect, and the request carries the policy rather than a database handle.
// If the only enforcement were on the caller's side, then a bug in the caller,
// a replayed request, or a future second caller would each be enough to make a
// write happen. Checking what was actually sent, against what the request
// itself claims is permitted, is what makes the refusal a property of the
// gateway rather than a property of one caller's correctness.
//
// # AND WHY A DISAGREEMENT IS A REFUSAL
//
// The caller states whether it believes the tool writes; the gateway has its
// own reading from the endpoint's own annotation. When those disagree, nothing
// happens. Resolving the disagreement in either direction would mean one of
// the two parties silently overruling the other about whether an operation
// changes a customer's data, which is the moment to stop rather than to pick.
package toolpolicy

import (
	"errors"
	"fmt"
	"strings"
)

// ErrRefused is what every decision in this package returns.
//
// One sentinel, and the wrapped message carries which rule fired. That is the
// opposite of the choice `egress` makes, and the reason is who reads it: this
// message goes into a fetch record the CUSTOMER reads, and "the tool
// create_issue is not granted on this connection" is precisely what they need
// in order to fix it. The egress list is the operator's and telling a customer
// its shape teaches them nothing they can act on.
var ErrRefused = errors.New("this connection may not make that call")

// Policy is what a connection is permitted to do, as the caller sent it.
type Policy struct {
	// Granted is every tool this connection may call at all.
	Granted []string
	// WriteGranted is the subset a human ticked knowing it writes.
	//
	// A name here that is absent from Granted is meaningless rather than
	// permissive: Decide checks membership of Granted first, so a
	// write-granted tool that is not granted is refused like any other
	// ungranted tool.
	WriteGranted []string
}

// Decide refuses everything the policy does not permit.
//
// `callerSaysWrites` is what core-api believes, from the `write_capable`
// column recorded at discovery. `endpointSaysWrites` is what the gateway read
// from the endpoint just now, and is false only when the endpoint positively
// annotated the tool as read-only: an unannotated tool reads as writing, so an
// endpoint that says nothing cannot get a write through by silence.
func Decide(policy Policy, tool string, callerSaysWrites, endpointSaysWrites bool) error {
	name := strings.TrimSpace(tool)
	if name == "" {
		return fmt.Errorf("%w: no tool was named", ErrRefused)
	}

	if !contains(policy.Granted, name) {
		return fmt.Errorf("%w: the tool %q is not granted on this connection", ErrRefused, name)
	}

	// EITHER side saying it writes makes it a write. Not both, and not the
	// caller's alone.
	//
	// The failure this prevents is the interesting one. Suppose the endpoint
	// changes: a tool that was read-only at discovery starts writing, or a
	// server operator relabels it. core-api's stored flag is now stale, and a
	// rule of "the caller decides" would let the write through on the strength
	// of a months-old discovery. Taking the union means a tool that has
	// started writing is refused until a human looks at it again, which is the
	// right amount of friction for that event.
	writes := callerSaysWrites || endpointSaysWrites
	if !writes {
		return nil
	}

	if !contains(policy.WriteGranted, name) {
		return fmt.Errorf(
			"%w: %q can change data in that system and this connection has not granted it write access",
			ErrRefused, name)
	}
	return nil
}

// ReadsOnly reports whether a tool the endpoint described is read-only.
//
// # AN UNANNOTATED TOOL READS AS WRITING, WHICH IS THE WHOLE FUNCTION
//
// MCP servers may annotate a tool with `readOnlyHint`. Many do not. The
// tempting default for an unannotated tool is read-only, because most tools
// are reads and it produces a friendlier consent screen; it is also the answer
// that lets a `delete_everything` tool from a server that annotates nothing
// through the gate marked safe.
//
// So: read-only when and only when the endpoint said so. Everything else
// writes. The cost is that a customer connecting a server with no annotations
// has to tick every tool, which is one extra click on a screen that exists
// precisely to be read carefully.
func ReadsOnly(annotations map[string]any) bool {
	if annotations == nil {
		return false
	}
	hint, present := annotations["readOnlyHint"]
	if !present {
		return false
	}
	value, isBool := hint.(bool)
	return isBool && value
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
