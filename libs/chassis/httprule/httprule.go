// Package httprule reads the REST binding an RPC declares on its own method
// descriptor.
//
// The sibling of libs/chassis/scope, and it exists for the same reason. The
// binding belongs in the contract rather than in a table somewhere in a
// gateway, because a hand-maintained table drifts: someone adds an RPC,
// forgets the table, and the method has no REST alias while the OpenAPI
// document still claims a complete surface.
//
// It lives in the chassis because it is pure protobuf plumbing. It knows
// nothing about findings, organisations or compliance, and it passes the §21.5
// test: it could be open-sourced without mentioning what this product does.
//
// Why this is worth having at all, when the annotations already generate an
// OpenAPI document: the document is only complete if every method is
// annotated, and nothing about generating it fails when one is not. The gap
// shows up as an endpoint missing from a customer's client library, months
// later. Reading the binding back at test time is what turns that into a build
// failure (§23.2).
package httprule

import (
	"errors"
	"fmt"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// ErrUndeclared is returned for a method carrying no google.api.http option.
//
// An error rather than a zero Binding, for the same reason scope.Of returns
// one: an empty value a caller might read as "no REST alias needed" is exactly
// the ambiguity that lets an unannotated method through.
var ErrUndeclared = errors.New("no google.api.http binding declared on method")

// Binding is the REST alias for one RPC.
type Binding struct {
	// Method is the HTTP verb, upper case.
	Method string
	// Path is the template, with {field} segments naming request fields.
	Path string
}

func (b Binding) String() string { return b.Method + " " + b.Path }

// Of returns the binding declared on a method descriptor.
func Of(method protoreflect.MethodDescriptor) (Binding, error) {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || opts == nil {
		return Binding{}, fmt.Errorf("%w: %s", ErrUndeclared, method.FullName())
	}
	if !proto.HasExtension(opts, annotations.E_Http) {
		return Binding{}, fmt.Errorf("%w: %s", ErrUndeclared, method.FullName())
	}

	rule, _ := proto.GetExtension(opts, annotations.E_Http).(*annotations.HttpRule)
	if rule == nil {
		return Binding{}, fmt.Errorf("%w: %s", ErrUndeclared, method.FullName())
	}

	binding, ok := bindingOf(rule)
	if !ok {
		// The option is present but names no pattern, which looks annotated to
		// anyone skimming the proto and binds nothing. The more dangerous of
		// the two failures, so it fails the same way.
		return Binding{}, fmt.Errorf("%w: %s declares an http option with no pattern",
			ErrUndeclared, method.FullName())
	}
	return binding, nil
}

func bindingOf(rule *annotations.HttpRule) (Binding, bool) {
	switch pattern := rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return Binding{Method: "GET", Path: pattern.Get}, pattern.Get != ""
	case *annotations.HttpRule_Put:
		return Binding{Method: "PUT", Path: pattern.Put}, pattern.Put != ""
	case *annotations.HttpRule_Post:
		return Binding{Method: "POST", Path: pattern.Post}, pattern.Post != ""
	case *annotations.HttpRule_Delete:
		return Binding{Method: "DELETE", Path: pattern.Delete}, pattern.Delete != ""
	case *annotations.HttpRule_Patch:
		return Binding{Method: "PATCH", Path: pattern.Patch}, pattern.Patch != ""
	case *annotations.HttpRule_Custom:
		custom := pattern.Custom
		if custom == nil || custom.GetKind() == "" || custom.GetPath() == "" {
			return Binding{}, false
		}
		return Binding{Method: custom.GetKind(), Path: custom.GetPath()}, true
	default:
		return Binding{}, false
	}
}

// OfService returns every method's binding keyed by full name, and the names
// of any that declare none.
//
// Both halves, rather than erroring on the first offender: a test that reports
// one missing annotation per run turns a five-minute fix into five runs. Same
// shape as scope.OfService, deliberately, so the two checks read alike.
func OfService(service protoreflect.ServiceDescriptor) (bindings map[string]Binding, undeclared []string) {
	bindings = make(map[string]Binding)

	methods := service.Methods()
	for i := 0; i < methods.Len(); i++ {
		method := methods.Get(i)

		binding, err := Of(method)
		if err != nil {
			undeclared = append(undeclared, string(method.FullName()))
			continue
		}
		bindings[string(method.FullName())] = binding
	}
	return bindings, undeclared
}
