package interceptor

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Entear-OU/kindlast/libs/chassis/scope"
)

// ErrUndeclaredMethod is returned by NewScope when a registered RPC carries no
// required_scope option.
var ErrUndeclaredMethod = errors.New("interceptor: RPC declares no required scope")

// Scope enforces the per-RPC scope declared in the proto.
//
// The table is read from the method descriptors once, at construction, rather
// than maintained by hand in this file. That is the whole point of putting the
// option in the contract (§1.3): a hand-maintained map in middleware drifts
// within a month, because someone adds an RPC, forgets the map, and the method
// becomes reachable with any valid token, with nothing to notice.
type Scope struct {
	required map[string]string
}

// NewScope builds the table and refuses to build it at all if any RPC is
// unannotated.
//
// Failing here means the binary does not start, which is the strongest
// available form of "fails closed": an unguarded RPC cannot be reached because
// the process serving it never came up. The runtime check in the interceptor
// is the second line, for a procedure that somehow reaches the chain without
// being in the table.
func NewScope(services []protoreflect.ServiceDescriptor) (*Scope, error) {
	required := make(map[string]string)
	var undeclared []string

	for _, service := range services {
		scopes, missing := scope.OfService(service)
		undeclared = append(undeclared, missing...)

		for fullName, declared := range scopes {
			required[procedureFor(fullName)] = declared
		}
	}

	if len(undeclared) > 0 {
		sort.Strings(undeclared)
		return nil, fmt.Errorf("%w: %v", ErrUndeclaredMethod, undeclared)
	}
	return &Scope{required: required}, nil
}

// Interceptor checks the token's scopes against the one the RPC declared.
func (s *Scope) Interceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			claims, ok := ClaimsFrom(ctx)
			if !ok {
				// Reachable only by wiring this interceptor before Auth, which
				// is a programming error rather than a request the caller
				// controls. Refuse rather than assume anything about identity.
				return nil, connect.NewError(connect.CodeInternal,
					errors.New("scope interceptor ran before authentication"))
			}

			procedure := req.Spec().Procedure
			declared, known := s.required[procedure]
			if !known {
				// An RPC absent from the table is unguarded, and an unguarded
				// RPC must be unreachable rather than open. Note the code: this
				// is not "you may not", it is "this server cannot say what
				// would be required", and answering 403 for it would suggest a
				// permission the caller could go and acquire.
				return nil, connect.NewError(connect.CodePermissionDenied,
					fmt.Errorf("%s declares no required scope", procedure))
			}

			if !claims.HasScope(declared) {
				return nil, connect.NewError(connect.CodePermissionDenied,
					fmt.Errorf("token does not carry the %q scope", declared))
			}

			return next(ctx, req)
		}
	}
}

// RequiredScope exposes the table for tests and for diagnostics.
func (s *Scope) RequiredScope(procedure string) (string, bool) {
	declared, ok := s.required[procedure]
	return declared, ok
}

// procedureFor converts a proto full method name into the Connect procedure
// path that arrives on the request.
//
// `kindlast.core.v1.SessionService.GetCurrentUser` becomes
// `/kindlast.core.v1.SessionService/GetCurrentUser`: the last dot becomes the
// slash separating service from method.
func procedureFor(fullName string) string {
	for i := len(fullName) - 1; i >= 0; i-- {
		if fullName[i] == '.' {
			return "/" + fullName[:i] + "/" + fullName[i+1:]
		}
	}
	return "/" + fullName
}
