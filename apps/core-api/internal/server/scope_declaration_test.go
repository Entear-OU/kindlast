package server_test

import (
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
	"github.com/Entear-OU/kindlast/libs/chassis/scope"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
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

// The guard above is only worth having if it can fail, and "I checked once by
// hand" is not a guarantee that survives the next refactor. This strips the
// options off the real service descriptor and asserts the same walk reports
// the method, so the check's ability to fail is itself under test.
//
// Without this, a change that broke scope reading (a renamed extension, a
// swallowed error) would leave TestEveryRPCDeclaresARequiredScope passing
// happily while checking nothing at all.
func TestTheDeclarationCheckCanActuallyFail(t *testing.T) {
	for _, service := range server.Services() {
		if service.Methods().Len() == 0 {
			continue
		}

		stripped := withoutMethodOptions(t, service)
		_, undeclared := scope.OfService(stripped)

		if len(undeclared) != stripped.Methods().Len() {
			t.Fatalf("%s: stripped of options, got %d offenders for %d methods; the check is not looking at what it claims to",
				service.FullName(), len(undeclared), stripped.Methods().Len())
		}
	}
}

// withoutMethodOptions round-trips a real service descriptor through its
// proto form with every method option removed, which is exactly what the
// descriptor would look like had someone added an RPC and forgotten the
// annotation.
func withoutMethodOptions(t *testing.T, service protoreflect.ServiceDescriptor) protoreflect.ServiceDescriptor {
	t.Helper()

	file := protodesc.ToFileDescriptorProto(service.ParentFile())
	for _, svc := range file.Service {
		for _, method := range svc.Method {
			method.Options = nil
		}
	}

	rebuilt, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("rebuilding %s without method options: %v", service.FullName(), err)
	}
	return rebuilt.Services().ByName(service.Name())
}
