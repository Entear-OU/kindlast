package httprule_test

import (
	"errors"
	"testing"

	"github.com/Entear-OU/kindlast/libs/chassis/httprule"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Fixtures are built in code rather than imported from kindlast.core.v1,
// deliberately. The chassis must not depend on business protos even in its
// tests (§21.5): the moment it does, the "could this be open-sourced without
// mentioning compliance" test fails, and the CI boundary check would need an
// exception carved out for test files. Exceptions to boundary rules are how
// boundaries die.

// The point of the annotation is that a reflection test can read it back off a
// descriptor. If that stops working, every binding silently becomes "no REST
// alias", so it is asserted directly rather than only through the walker.
func TestOfReadsEachVerb(t *testing.T) {
	cases := []struct {
		name string
		rule *annotations.HttpRule
		want httprule.Binding
	}{
		{
			name: "get",
			rule: &annotations.HttpRule{Pattern: &annotations.HttpRule_Get{Get: "/api/v1/me"}},
			want: httprule.Binding{Method: "GET", Path: "/api/v1/me"},
		},
		{
			name: "post with a custom verb",
			rule: &annotations.HttpRule{Pattern: &annotations.HttpRule_Post{
				Post: "/api/v1/invitations/{token}:accept",
			}},
			want: httprule.Binding{Method: "POST", Path: "/api/v1/invitations/{token}:accept"},
		},
		{
			name: "patch",
			rule: &annotations.HttpRule{Pattern: &annotations.HttpRule_Patch{Patch: "/api/v1/things/{id}"}},
			want: httprule.Binding{Method: "PATCH", Path: "/api/v1/things/{id}"},
		},
		{
			name: "delete",
			rule: &annotations.HttpRule{Pattern: &annotations.HttpRule_Delete{Delete: "/api/v1/things/{id}"}},
			want: httprule.Binding{Method: "DELETE", Path: "/api/v1/things/{id}"},
		},
		{
			name: "put",
			rule: &annotations.HttpRule{Pattern: &annotations.HttpRule_Put{Put: "/api/v1/things/{id}"}},
			want: httprule.Binding{Method: "PUT", Path: "/api/v1/things/{id}"},
		},
		{
			// The escape hatch RFC-shaped APIs occasionally need, and the one
			// that would otherwise read as "no binding".
			name: "custom kind",
			rule: &annotations.HttpRule{Pattern: &annotations.HttpRule_Custom{
				Custom: &annotations.CustomHttpPattern{Kind: "HEAD", Path: "/api/v1/things"},
			}},
			want: httprule.Binding{Method: "HEAD", Path: "/api/v1/things"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := fixture(t, map[string]*annotations.HttpRule{"Method": testCase.rule})

			got, err := httprule.Of(service.Methods().ByName("Method"))
			if err != nil {
				t.Fatalf("reading the binding: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("binding = %s, want %s", got, testCase.want)
			}
		})
	}
}

// A method with no annotation must be an error, never a zero Binding a caller
// could mistake for "no REST alias needed".
func TestOfFailsClosedOnAnUnannotatedMethod(t *testing.T) {
	service := fixture(t, map[string]*annotations.HttpRule{"Method": nil})

	_, err := httprule.Of(service.Methods().ByName("Method"))
	if !errors.Is(err, httprule.ErrUndeclared) {
		t.Fatalf("error = %v, want ErrUndeclared", err)
	}
}

// The option present but empty is a different failure from the option being
// absent, and the more dangerous of the two: it looks annotated to anyone
// skimming the proto and binds nothing at all.
func TestOfFailsClosedOnAnOptionWithNoPattern(t *testing.T) {
	service := fixture(t, map[string]*annotations.HttpRule{"Method": {}})

	_, err := httprule.Of(service.Methods().ByName("Method"))
	if !errors.Is(err, httprule.ErrUndeclared) {
		t.Fatalf("error = %v, want ErrUndeclared", err)
	}
}

// An empty path with a real verb is the same trap wearing a different hat.
func TestOfFailsClosedOnAnEmptyPath(t *testing.T) {
	service := fixture(t, map[string]*annotations.HttpRule{
		"Method": {Pattern: &annotations.HttpRule_Get{Get: ""}},
	})

	_, err := httprule.Of(service.Methods().ByName("Method"))
	if !errors.Is(err, httprule.ErrUndeclared) {
		t.Fatalf("error = %v, want ErrUndeclared", err)
	}
}

// A method carrying options but not this one, which is the shape a real proto
// produces when someone writes only a required_scope. A distinct code path
// from "no options at all".
func TestOfFailsClosedWhenOtherOptionsArePresent(t *testing.T) {
	opts := &descriptorpb.MethodOptions{Deprecated: proto.Bool(true)}
	service := build(t, &descriptorpb.ServiceDescriptorProto{
		Name: proto.String("Fixture"),
		Method: []*descriptorpb.MethodDescriptorProto{{
			Name:       proto.String("Method"),
			InputType:  proto.String(".httprule.test.v1.Empty"),
			OutputType: proto.String(".httprule.test.v1.Empty"),
			Options:    opts,
		}},
	})

	_, err := httprule.Of(service.Methods().ByName("Method"))
	if !errors.Is(err, httprule.ErrUndeclared) {
		t.Fatalf("error = %v, want ErrUndeclared", err)
	}
}

// One offender per run turns a five-minute fix into five runs, so the walker
// reports all of them at once.
func TestOfServiceReportsEveryUnannotatedMethodAtOnce(t *testing.T) {
	service := fixture(t, map[string]*annotations.HttpRule{
		"Annotated":   {Pattern: &annotations.HttpRule_Get{Get: "/api/v1/thing"}},
		"Missing":     nil,
		"AlsoMissing": nil,
	})

	bindings, undeclared := httprule.OfService(service)

	if len(bindings) != 1 {
		t.Fatalf("bindings = %v, want exactly one", bindings)
	}
	if len(undeclared) != 2 {
		t.Fatalf("undeclared = %v, want both offenders reported", undeclared)
	}
}

// fixture builds a one-service file descriptor whose methods carry the rules
// given. A nil rule means the option is omitted entirely, which is what an
// unannotated RPC looks like to the reader.
func fixture(t *testing.T, methods map[string]*annotations.HttpRule) protoreflect.ServiceDescriptor {
	t.Helper()

	service := &descriptorpb.ServiceDescriptorProto{Name: proto.String("Fixture")}
	for name, rule := range methods {
		method := &descriptorpb.MethodDescriptorProto{
			Name:       proto.String(name),
			InputType:  proto.String(".httprule.test.v1.Empty"),
			OutputType: proto.String(".httprule.test.v1.Empty"),
		}
		if rule != nil {
			opts := &descriptorpb.MethodOptions{}
			proto.SetExtension(opts, annotations.E_Http, rule)
			method.Options = opts
		}
		service.Method = append(service.Method, method)
	}

	return build(t, service)
}

func build(t *testing.T, service *descriptorpb.ServiceDescriptorProto) protoreflect.ServiceDescriptor {
	t.Helper()

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("httprule/test/v1/fixture.proto"),
		Package: proto.String("httprule.test.v1"),
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
