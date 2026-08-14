// Package scope reads the required OAuth scope that an RPC declares on its
// own method descriptor.
//
// It lives in the chassis rather than in core-api because it is pure protobuf
// plumbing: it knows nothing about findings, organisations or compliance, and
// the second Go service will need the same reader. It passes the §21.5 test,
// which is that it could be open-sourced without mentioning what this product
// does.
package scope

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	optionsv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/options/v1"
)

// ErrUndeclared is returned for a method carrying no required_scope option.
//
// Deliberately an error rather than an empty string: an RPC that forgot to
// declare a scope must fail closed, never default to reachable. The
// reflection test in core-api turns this into a build failure rather than a
// runtime surprise.
var ErrUndeclared = errors.New("no required_scope declared on method")

// Of returns the scope declared on a method descriptor.
func Of(method protoreflect.MethodDescriptor) (string, error) {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || opts == nil {
		return "", fmt.Errorf("%w: %s", ErrUndeclared, method.FullName())
	}
	if !proto.HasExtension(opts, optionsv1.E_RequiredScope) {
		return "", fmt.Errorf("%w: %s", ErrUndeclared, method.FullName())
	}
	declared, _ := proto.GetExtension(opts, optionsv1.E_RequiredScope).(string)
	if declared == "" {
		return "", fmt.Errorf("%w: %s", ErrUndeclared, method.FullName())
	}
	return declared, nil
}

// OfService returns every method in a service keyed by its full name, and the
// names of any that declare no scope.
//
// Both halves are returned rather than erroring on the first offender: a
// reflection test that reports one missing scope per run turns a five-minute
// fix into five runs.
func OfService(service protoreflect.ServiceDescriptor) (scopes map[string]string, undeclared []string) {
	scopes = make(map[string]string)
	methods := service.Methods()
	for i := 0; i < methods.Len(); i++ {
		method := methods.Get(i)
		declared, err := Of(method)
		if err != nil {
			undeclared = append(undeclared, string(method.FullName()))
			continue
		}
		scopes[string(method.FullName())] = declared
	}
	return scopes, undeclared
}
