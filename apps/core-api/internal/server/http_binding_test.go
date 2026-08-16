package server_test

import (
	"testing"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
	"github.com/Entear-OU/kindlast/libs/chassis/httprule"
)

// The REST binding half of ENT-199, built to the same shape as the scope
// declaration test next to it, because the failure it guards is the same
// shape: an RPC added without an annotation, noticed by nobody.
//
// What makes this worth having when the annotations already generate an
// OpenAPI document: generation does not fail on a missing annotation. It
// quietly emits a document describing a smaller API than the one that exists,
// and the gap surfaces as an endpoint absent from a customer's generated
// client, long after the commit that caused it.

func TestEveryRPCDeclaresAnHTTPBinding(t *testing.T) {
	services := server.Services()
	if len(services) == 0 {
		t.Fatal("no services registered; the registry is the thing under test")
	}

	var undeclaredTotal []string
	for _, service := range services {
		bindings, undeclared := httprule.OfService(service)
		undeclaredTotal = append(undeclaredTotal, undeclared...)

		if service.Methods().Len() > 0 && len(bindings) == 0 && len(undeclared) == 0 {
			t.Fatalf("%s has methods but produced neither bindings nor failures", service.FullName())
		}
	}

	if len(undeclaredTotal) > 0 {
		t.Fatalf("RPCs missing a google.api.http annotation: %v", undeclaredTotal)
	}
}

// The other half, and the one the presence check cannot give.
//
// A reader hard-wired to return one binding passes "is something declared"
// forever. So this asserts the actual values the shipped contract carries: if
// the reader stops reading and starts guessing, these go red, and if a path is
// changed by accident the diff has to say so here too.
//
// The paths are the ones §12 and §0.3 settle: `/api/v1/...`, and `:verb` for a
// custom action rather than a subresource that does not exist.
func TestTheDeclaredBindingsAreTheOnesTheContractPromises(t *testing.T) {
	want := map[string]httprule.Binding{
		"kindlast.core.v1.SessionService.GetCurrentUser": {
			Method: "GET", Path: "/api/v1/me",
		},
		"kindlast.core.v1.OrgService.AcceptInvitation": {
			Method: "POST", Path: "/api/v1/invitations/{token}:accept",
		},

		// ENT-202. Read these as a group, because the thing worth reviewing is
		// what they all lack: not one carries an `{org_id}` segment.
		//
		// Conventional REST would write /organisations/{org_id}/members/{user_id}
		// and it would be wrong here. The organisation travels in the
		// Kindlast-Org-Id header so that membership has exactly one source of
		// truth; a path parameter would give the same fact a second source, and
		// a request naming one organisation in the header and another in the
		// path would have to be either an error nobody wrote or a silent
		// winner. Stripe and Slack scope the same way.
		//
		// `{user_id}` is fine and is a different thing: it names the person
		// being acted on, and it is meaningless without the header.
		"kindlast.core.v1.OrgService.CreateOrganisation": {
			Method: "POST", Path: "/api/v1/organisations",
		},
		// Singular, because it addresses the organisation the header names
		// rather than a member of a collection.
		"kindlast.core.v1.OrgService.UpdateOrganisation": {
			Method: "PATCH", Path: "/api/v1/organisation",
		},
		"kindlast.core.v1.OrgService.ListMembers": {
			Method: "GET", Path: "/api/v1/members",
		},
		"kindlast.core.v1.OrgService.UpdateMemberRole": {
			Method: "PATCH", Path: "/api/v1/members/{user_id}",
		},
		"kindlast.core.v1.OrgService.RemoveMember": {
			Method: "DELETE", Path: "/api/v1/members/{user_id}",
		},
		"kindlast.core.v1.OrgService.InviteMember": {
			Method: "POST", Path: "/api/v1/invitations",
		},

		// ENT-203. Same absence to review as the group above: no `{org_id}`
		// anywhere. `{finding_id}` names the thing being acted on and is
		// meaningless without the header, exactly as `{user_id}` is.
		//
		// The three act paths use `:approve`, `:reject` and `:snooze` rather
		// than `/approve`, per AIP-136 and §12: a custom verb on a resource,
		// not a subresource that does not exist. `AcceptInvitation` above set
		// that precedent.
		"kindlast.core.v1.FindingsService.ListFindings": {
			Method: "GET", Path: "/api/v1/findings",
		},
		"kindlast.core.v1.FindingsService.GetFinding": {
			Method: "GET", Path: "/api/v1/findings/{finding_id}",
		},
		"kindlast.core.v1.FindingsService.ApproveFinding": {
			Method: "POST", Path: "/api/v1/findings/{finding_id}:approve",
		},
		"kindlast.core.v1.FindingsService.RejectFinding": {
			Method: "POST", Path: "/api/v1/findings/{finding_id}:reject",
		},
		"kindlast.core.v1.FindingsService.SnoozeFinding": {
			Method: "POST", Path: "/api/v1/findings/{finding_id}:snooze",
		},
		// Singular, and for the same reason UpdateOrganisation is: it
		// addresses the dashboard of the organisation the header names, not a
		// member of a collection of dashboards.
		"kindlast.core.v1.DashboardService.GetDashboard": {
			Method: "GET", Path: "/api/v1/dashboard",
		},

		// ENT-203. The only binding outside /api/v1, and the prefix is the
		// reviewable part: /internal/v1 is not reachable through the edge's
		// public routes and is not served to a browser client. The proto
		// package is `platform` rather than `internal` only because Go gives
		// any path segment called `internal` a visibility rule that would make
		// the generated code unimportable; the route keeps the design's name.
		//
		// Still no {org_id}: a sweep names its organisation in the header like
		// everything else.
		"kindlast.platform.v1.SweepService.RunSweep": {
			Method: "POST", Path: "/internal/v1/sweep",
		},
	}

	got := map[string]httprule.Binding{}
	for _, service := range server.Services() {
		bindings, _ := httprule.OfService(service)
		for name, binding := range bindings {
			got[name] = binding
		}
	}

	for name, expected := range want {
		actual, ok := got[name]
		if !ok {
			t.Errorf("%s declares no binding", name)
			continue
		}
		if actual != expected {
			t.Errorf("%s binds %s, want %s", name, actual, expected)
		}
	}

	// Every served method must appear above. A new RPC that nobody added here
	// is a new REST path nobody reviewed, which is the whole point of pinning
	// the contract before strangers depend on it.
	for name := range got {
		if _, expected := want[name]; !expected {
			t.Errorf("%s declares a binding this test does not pin; add it, "+
				"so the path is reviewed rather than merely generated", name)
		}
	}
}

// The guard is only worth having if it can fail. This strips the options off
// the real service descriptors and asserts the same walk reports every method,
// so the check's ability to fail is itself under test rather than resting on
// someone having tried it once by hand.
func TestTheBindingCheckCanActuallyFail(t *testing.T) {
	for _, service := range server.Services() {
		if service.Methods().Len() == 0 {
			continue
		}

		stripped := withoutMethodOptionsForBindings(t, service)
		_, undeclared := httprule.OfService(stripped)

		if len(undeclared) != stripped.Methods().Len() {
			t.Fatalf("%s: stripped of options, got %d offenders for %d methods; "+
				"the check is not looking at what it claims to",
				service.FullName(), len(undeclared), stripped.Methods().Len())
		}
	}
}

func withoutMethodOptionsForBindings(t *testing.T, service protoreflect.ServiceDescriptor) protoreflect.ServiceDescriptor {
	t.Helper()

	file := protodesc.ToFileDescriptorProto(service.ParentFile())
	for _, svc := range file.Service {
		for _, method := range svc.Method {
			method.Options = nil
		}
	}

	// The real file imports google/api/annotations.proto, so rebuilding it
	// needs a resolver that can find those dependencies. GlobalFiles has them,
	// because the generated code registered them on import.
	rebuilt, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("rebuilding %s without method options: %v", service.FullName(), err)
	}

	return rebuilt.Services().ByName(service.Name())
}
