// Package server wires the Connect handlers the gateway serves.
//
// One service, one interceptor, and a health probe. It is deliberately the
// smallest surface in this repository: the gateway's job is to be the only
// thing that dials outward, and every additional endpoint here is an
// additional way to ask it to.
package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
	"github.com/Entear-OU/kindlast/libs/chassis/scope"
)

// Services returns every proto service this binary exposes.
//
// The same shape as core-api's registry, and it exists for the same reason:
// the scope-declaration and binding tests have one place to enumerate rather
// than a list in a test file somebody forgets to extend. An RPC added to
// gateway.proto without a declared scope fails a test here, not a review.
func Services() []protoreflect.ServiceDescriptor {
	files := []protoreflect.FileDescriptor{
		platformv1.File_kindlast_platform_v1_gateway_proto,
	}

	var services []protoreflect.ServiceDescriptor
	for _, file := range files {
		descriptors := file.Services()
		for i := 0; i < descriptors.Len(); i++ {
			services = append(services, descriptors.Get(i))
		}
	}
	return services
}

// Dependencies is everything the mux needs, supplied by main.
type Dependencies struct {
	// Gateway is the handler. An interface would buy nothing here: there is
	// one implementation and it is in this module.
	Gateway platformv1connect.GatewayServiceHandler

	// SharedSecret is what a caller must present. Empty is refused at
	// construction rather than at request time, because a service that starts
	// with no secret and refuses every call looks identical to one that is
	// simply broken.
	SharedSecret string

	// Ready reports whether the gateway is usable. Nil means always ready.
	Ready func(context.Context) error
}

// ErrNoSecret is returned when the mux is built without one.
var ErrNoSecret = errors.New("server: a shared secret is required")

// New builds the HTTP handler the gateway serves.
func New(deps Dependencies) (http.Handler, error) {
	if strings.TrimSpace(deps.SharedSecret) == "" {
		return nil, ErrNoSecret
	}

	required, err := requiredScopes()
	if err != nil {
		// The binary does not start. An RPC with no declared scope would
		// otherwise be reachable by anybody holding the shared secret, and a
		// process that refuses to come up is the loudest form of failing
		// closed.
		return nil, err
	}

	chain := connect.WithInterceptors(authorise(deps.SharedSecret, required))

	mux := http.NewServeMux()
	mux.Handle(platformv1connect.NewGatewayServiceHandler(deps.Gateway, chain))

	// Unauthenticated by design and bound to the internal listener only.
	// Requiring a credential on a health probe is a common reflex that breaks
	// orchestrator probes for no security gain (§1.7).
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if deps.Ready != nil {
			if err := deps.Ready(r.Context()); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprintf(w, "not ready: %v\n", err)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})

	return mux, nil
}

// requiredScopes reads the declared scope off every method this binary serves.
func requiredScopes() (map[string]string, error) {
	required := map[string]string{}
	var undeclared []string

	for _, service := range Services() {
		scopes, missing := scope.OfService(service)
		undeclared = append(undeclared, missing...)
		for fullName, declared := range scopes {
			required[procedureFor(fullName)] = declared
		}
	}
	if len(undeclared) > 0 {
		return nil, fmt.Errorf("server: RPCs missing a required_scope option: %v", undeclared)
	}
	return required, nil
}

// procedureFor turns `pkg.Service.Method` into the `/pkg.Service/Method` that
// Connect reports as the procedure.
//
// The same three lines core-api's scope interceptor carries. Duplicated rather
// than lifted into the chassis, because the chassis is protobuf plumbing and
// this is a fact about Connect's routing.
func procedureFor(fullName string) string {
	for i := len(fullName) - 1; i >= 0; i-- {
		if fullName[i] == '.' {
			return "/" + fullName[:i] + "/" + fullName[i+1:]
		}
	}
	return "/" + fullName
}

// grantedScopes is what a caller presenting the shared secret holds.
//
// # WHY A CONSTANT SET AND NOT A TOKEN'S CLAIMS
//
// The gateway's caller is core-api, on the internal network, acting as itself.
// It has no person, no organisation and no consent to narrow, so a token's
// subject, audience and tenancy would each mean nothing here, and acquiring
// one would mean core-api holding an OAuth client it uses for this hop alone.
//
// What the scope option still buys, and why this is a set rather than a
// bypass: the declared scope on each method is read off the descriptor and
// checked against this set, so an RPC declaring something else fails at the
// interceptor rather than being reachable because the secret was right. The
// day a second caller exists with less authority, this function becomes a
// token reader and nothing else moves.
var grantedScopes = map[string]struct{}{
	"internal:gateway": {},
}

// authorise checks the shared secret and then the declared scope.
func authorise(secret string, required map[string]string) connect.UnaryInterceptorFunc {
	expected := []byte(secret)

	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
			presented := bearer(request.Header().Get("Authorization"))

			// Constant time, because a byte-at-a-time comparison of a secret
			// against an attacker-supplied string is a timing oracle, and the
			// attacker here is anybody who has got onto the internal network,
			// which is precisely the case this secret exists for.
			if subtle.ConstantTimeCompare([]byte(presented), expected) != 1 {
				return nil, connect.NewError(connect.CodeUnauthenticated,
					errors.New("this gateway is not open to you"))
			}

			declared, known := required[request.Spec().Procedure]
			if !known {
				// A procedure served by the mux and absent from the table
				// means the registry and the mux disagree. Refusing is the
				// only safe reading: the alternative is running a method whose
				// required authority nobody could state.
				return nil, connect.NewError(connect.CodePermissionDenied,
					fmt.Errorf("%s declares no required scope", request.Spec().Procedure))
			}
			if _, held := grantedScopes[declared]; !held {
				return nil, connect.NewError(connect.CodePermissionDenied,
					fmt.Errorf("%s requires %s", request.Spec().Procedure, declared))
			}

			return next(ctx, request)
		}
	}
}

// bearer pulls the token out of an Authorization header.
//
// Case-insensitive on the scheme, because clients write it every way there is,
// and returning an empty string for anything else, which the constant-time
// comparison then refuses like any other wrong value.
func bearer(header string) string {
	const scheme = "bearer "
	if len(header) < len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(header[len(scheme):])
}
