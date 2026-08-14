package scope_test

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	optionsv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/options/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/scope"
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

// The option present but set to the empty string is a different failure from
// the option being absent, and it is the more dangerous of the two: it looks
// annotated to a reader skimming the proto. It must still fail closed.
func TestOfFailsClosedOnAnEmptyScope(t *testing.T) {
	service := fixtureWithEmptyOption(t)

	_, err := scope.Of(service.Methods().ByName("Read"))
	if !errors.Is(err, scope.ErrUndeclared) {
		t.Fatalf("error = %v, want ErrUndeclared", err)
	}
}

// A method that carries options but not this one, which is the shape a real
// proto produces when someone writes `option deprecated = true;` and no
// scope. It is a distinct code path from "no options at all", because
// protoreflect hands back a nil options message in that case and a populated
// one here.
func TestOfFailsClosedWhenOtherOptionsArePresent(t *testing.T) {
	opts := &descriptorpb.MethodOptions{Deprecated: proto.Bool(true)}
	service := build(t, &descriptorpb.ServiceDescriptorProto{
		Name: proto.String("Fixture"),
		Method: []*descriptorpb.MethodDescriptorProto{{
			Name:       proto.String("Read"),
			InputType:  proto.String(".scope.test.v1.Empty"),
			OutputType: proto.String(".scope.test.v1.Empty"),
			Options:    opts,
		}},
	})

	_, err := scope.Of(service.Methods().ByName("Read"))
	if !errors.Is(err, scope.ErrUndeclared) {
		t.Fatalf("error = %v, want ErrUndeclared", err)
	}
}

// A descriptor carrying no options at all, which is what a method declared
// without any `option (...)` block looks like once it has been through a
// round trip that drops the default instance.
func TestOfFailsClosedWhenOptionsAreAbsentEntirely(t *testing.T) {
	_, err := scope.Of(strippedOptions{fixture(t, map[string]string{"Read": "findings:read"}).Methods().ByName("Read")})
	if !errors.Is(err, scope.ErrUndeclared) {
		t.Fatalf("error = %v, want ErrUndeclared", err)
	}
}

// strippedOptions presents a real method descriptor reporting no options,
// exercising the branch that a normal descriptor never reaches because
// protoreflect hands back a default instance rather than nil.
type strippedOptions struct{ protoreflect.MethodDescriptor }

func (strippedOptions) Options() protoreflect.ProtoMessage { return nil }

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

	return build(t, service)
}

// fixtureWithEmptyOption sets the extension explicitly to "", which the
// map-based fixture above cannot express (an empty value there means "omit
// the option entirely").
func fixtureWithEmptyOption(t *testing.T) protoreflect.ServiceDescriptor {
	t.Helper()

	opts := &descriptorpb.MethodOptions{}
	proto.SetExtension(opts, optionsv1.E_RequiredScope, "")

	return build(t, &descriptorpb.ServiceDescriptorProto{
		Name: proto.String("Fixture"),
		Method: []*descriptorpb.MethodDescriptorProto{{
			Name:       proto.String("Read"),
			InputType:  proto.String(".scope.test.v1.Empty"),
			OutputType: proto.String(".scope.test.v1.Empty"),
			Options:    opts,
		}},
	})
}

func build(t *testing.T, service *descriptorpb.ServiceDescriptorProto) protoreflect.ServiceDescriptor {
	t.Helper()

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
