package server_test

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
)

// notServedByCoreAPI names every Kindlast proto service that this binary
// deliberately does not serve, with the reason.
//
// It is an allow-list rather than a skip, because the point of the test below
// is that a new service must be CLASSIFIED. Adding one and forgetting the
// registry is the failure this exists to catch, and an implicit "unknown means
// fine" rule would let exactly that through.
var notServedByCoreAPI = map[protoreflect.FullName]string{
	// Served by the Python service, not by core-api. core-api is its CLIENT:
	// it calls DraftNarrative outbound and never answers it. Listing it in the
	// registry would declare a scope for a method this binary does not route,
	// which is the mirror-image mistake.
	"kindlast.platform.v1.IntelligenceService": "served by apps/intelligence, core-api is the caller",
	// Served by apps/workers, not by core-api, and for the same shape of
	// reason: core-api is its CLIENT. It calls ListTools and CallTool outbound
	// and answers neither.
	//
	// The boundary is the point rather than an accident of who wrote it.
	// core-api holds the database credential and the key that seals customer
	// credentials; the gateway is the process that opens a connection to an
	// address a customer typed. Serving both in one binary would put
	// server-side request forgery where it has the most to reach.
	//
	// apps/workers has its own copy of the scope and binding declaration tests,
	// so these two RPCs are checked wherever they ARE served.
	"kindlast.platform.v1.GatewayService": "served by apps/workers, core-api is the caller",
}

// Every service this binary could serve is in the registry, or is explicitly
// excused.
//
// # WHY THIS EXISTS, AND WHAT IT CAUGHT
//
// TestEveryRPCDeclaresARequiredScope walks `server.Services()` and proves every
// RPC in that list declares a scope. It cannot prove the list is COMPLETE, and
// that gap is not theoretical: NarrativeService (ENT-245) shipped with its
// handler mounted on the mux and its file missing from the registry. The scope
// table is built from the registry, so the interceptor had no entry for the
// method and default-denied it. Every call returned permission_denied with
// "declares no required scope", and the feature was unreachable in every
// deployment while its own unit tests stayed green.
//
// The default-deny was correct. Denying a method nobody declared is exactly
// what should happen, and it is why this was a dead feature rather than an open
// door. But the failure was invisible: nothing in the Go suite calls a mounted
// handler through the interceptor chain, so only driving the live stack
// surfaced it.
//
// Walking the global registry is what makes this checkable. Both generated
// packages are linked into this binary, so every Kindlast service registers
// itself whether or not anyone remembered to list it.
func TestEveryKindlastServiceIsClassified(t *testing.T) {
	registered := map[protoreflect.FullName]bool{}
	for _, service := range server.Services() {
		registered[service.FullName()] = true
	}
	if len(registered) == 0 {
		t.Fatal("no services registered; the registry is the thing under test")
	}

	var missing []protoreflect.FullName

	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		services := file.Services()
		for i := 0; i < services.Len(); i++ {
			name := services.Get(i).FullName()

			// Only this product's own surface. The generated tree also carries
			// google.* descriptors, which are nobody's to serve.
			if !isKindlast(name) {
				continue
			}
			if registered[name] || notServedByCoreAPI[name] != "" {
				continue
			}
			missing = append(missing, name)
		}
		return true
	})

	if len(missing) > 0 {
		t.Fatalf("these services are neither in server.Services() nor excused in notServedByCoreAPI, "+
			"so any handler mounted for them is denied at runtime: %v", missing)
	}
}

// An excuse must name a service that exists, so the allow-list cannot rot into
// a list of typos that silently excuse nothing.
//
// Without this, renaming a service would leave its stale entry behind, the real
// service would fall through to `missing`, and the fix would look like adding a
// second excuse rather than correcting the first.
func TestEveryExcuseNamesARealService(t *testing.T) {
	for name, reason := range notServedByCoreAPI {
		if reason == "" {
			t.Errorf("%s is excused with no reason", name)
		}
		if _, err := protoregistry.GlobalFiles.FindDescriptorByName(name); err != nil {
			t.Errorf("%s is excused but no such service is linked in: %v", name, err)
		}
	}
}

// isKindlast reports whether a service belongs to this product's proto surface.
func isKindlast(name protoreflect.FullName) bool {
	const prefix = "kindlast."
	return len(name) > len(prefix) && name[:len(prefix)] == prefix
}
