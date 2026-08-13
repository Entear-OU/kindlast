package server_test

import (
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
	"github.com/Entear-OU/kindlast/libs/chassis/scope"
)

// The reflection test ENT-191 asks for: walk the service descriptors this
// binary serves and fail on any RPC that declares no required scope.
//
// Without it, adding an RPC and forgetting the option produces a method
// reachable by any valid token, with nothing to notice. That failure is
// silent in exactly the way tenant isolation was silent before ENT-192, so it
// gets the same treatment: a test that fails loudly rather than a convention.
func TestEveryRPCDeclaresARequiredScope(t *testing.T) {
	services := server.Services()
	if len(services) == 0 {
		t.Fatal("no services registered; the registry is the thing under test")
	}

	var undeclaredTotal []string
	for _, service := range services {
		scopes, undeclared := scope.OfService(service)
		undeclaredTotal = append(undeclaredTotal, undeclared...)

		if service.Methods().Len() > 0 && len(scopes) == 0 && len(undeclared) == 0 {
			t.Fatalf("%s has methods but produced neither scopes nor failures", service.FullName())
		}
	}

	if len(undeclaredTotal) > 0 {
		t.Fatalf("RPCs missing a required_scope option: %v", undeclaredTotal)
	}
}
