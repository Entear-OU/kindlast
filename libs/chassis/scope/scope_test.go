package scope_test

import (
	"errors"
	"testing"

	optionsv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/options/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/scope"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// The fixtures here are built in code rather than imported from
// kindlast.core.v1, deliberately. The chassis must not depend on business
// protos even in its tests (core-api-surface §21.5): the moment it does, the
// "could this be open-sourced without mentioning compliance" test fails, and
// the CI boundary check that enforces it would need an exception carved out
// for test files.

// The point of the option is that middleware can read it off a descriptor at
// runtime. If that ever stops working, every scope check silently becomes
// "no scope required", so it is asserted directly rather than only through an
// interceptor.
func TestOfReadsTheDeclaredScope(t *testing.T) {
	service := fixture(t, map[string]string{"Read": "findings:read"})

	got, err := scope.Of(service.Methods().ByName("Read"))
	if err != nil {
		t.Fatalf("reading the declared scope: %v", err)
	}
	if want := "findings:read"; got != want {
		t.Fatalf("declared scope = %q, want %q", got, want)
	}
}

// A method with no declared scope must be an error, never an empty string a
// caller could mistake for "no scope needed".
func TestOfFailsClosedOnAnUndeclaredMethod(t *testing.T) {
	service := fixture(t, map[string]string{"Read": ""})

	_, err := scope.Of(service.Methods().ByName("Read"))
	if !errors.Is(err, scope.ErrUndeclared) {
		t.Fatalf("error = %v, want ErrUndeclared", err)
	}
}

// One offender per run turns a five-minute fix into five runs, so the walker
// reports all of them at once.
func TestOfServiceReportsEveryUndeclaredMethodAtOnce(t *testing.T) {
	service := fixture(t, map[string]string{
		"Declared":    "findings:read",
		"Missing":     "",
		"AlsoMissing": "",
	})

	scopes, undeclared := scope.OfService(service)

	if len(scopes) != 1 {
		t.Fatalf("declared scopes = %v, want exactly one", scopes)
	}
	if len(undeclared) != 2 {
		t.Fatalf("undeclared = %v, want both offenders reported", undeclared)
	}
}

// fixture builds a one-service file descriptor whose methods carry the scopes
// given. An empty scope means the option is omitted entirely, which is what an
// unannotated RPC looks like to the reader.
func fixture(t *testing.T, methods map[string]string) protoreflect.ServiceDescriptor {
	t.Helper()

	service := &descriptorpb.ServiceDescriptorProto{Name: proto.String("Fixture")}
	for name, declared := range methods {
		method := &descriptorpb.MethodDescriptorProto{
			Name:       proto.String(name),
			InputType:  proto.String(".scope.test.v1.Empty"),
			OutputType: proto.String(".scope.test.v1.Empty"),
		}
		if declared != "" {
			opts := &descriptorpb.MethodOptions{}
			proto.SetExtension(opts, optionsv1.E_RequiredScope, declared)
			method.Options = opts
		}
		service.Method = append(service.Method, method)
	}

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("scope/test/v1/fixture.proto"),
		Package: proto.String("scope.test.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Empty")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{service},
	}, nil)
	if err != nil {
		t.Fatalf("building the fixture descriptor: %v", err)
	}
	return file.Services().Get(0)
}
