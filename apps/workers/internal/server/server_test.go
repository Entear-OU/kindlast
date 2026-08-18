package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/Entear-OU/kindlast/apps/workers/internal/server"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
	"github.com/Entear-OU/kindlast/libs/chassis/httprule"
	"github.com/Entear-OU/kindlast/libs/chassis/scope"
)

const secret = "a-development-only-gateway-secret"

// ---------------------------------------------------------------------------
// The declaration checks, in the same shape core-api carries them.
//
// This binary serves proto RPCs, so it needs its own copies: the checks in
// core-api walk core-api's registry and would never see gateway.proto. An RPC
// added here without a scope or a binding must fail a test wherever it is
// served.
// ---------------------------------------------------------------------------

func TestEveryRPCDeclaresARequiredScope(t *testing.T) {
	services := server.Services()
	if len(services) == 0 {
		t.Fatal("no services registered; the registry is the thing under test")
	}

	var undeclared []string
	for _, service := range services {
		scopes, missing := scope.OfService(service)
		undeclared = append(undeclared, missing...)
		if service.Methods().Len() > 0 && len(scopes) == 0 && len(missing) == 0 {
			t.Fatalf("%s has methods but produced neither scopes nor failures", service.FullName())
		}
	}
	if len(undeclared) > 0 {
		t.Fatalf("RPCs missing a required_scope option: %v", undeclared)
	}
}

// The guard is only worth having if it can fail. Stripping the options off the
// real descriptor must make the same walk report every method.
func TestTheDeclarationChecksCanActuallyFail(t *testing.T) {
	for _, service := range server.Services() {
		if service.Methods().Len() == 0 {
			continue
		}
		stripped := withoutMethodOptions(t, service)

		if _, undeclared := scope.OfService(stripped); len(undeclared) != stripped.Methods().Len() {
			t.Fatalf("%s: stripped of options, the scope walk found %d offenders for %d methods",
				service.FullName(), len(undeclared), stripped.Methods().Len())
		}
		if _, undeclared := httprule.OfService(stripped); len(undeclared) != stripped.Methods().Len() {
			t.Fatalf("%s: stripped of options, the binding walk found %d offenders for %d methods",
				service.FullName(), len(undeclared), stripped.Methods().Len())
		}
	}
}

// The bindings this contract promises, pinned by value.
//
// The reviewable choice is the prefix. `/internal/v1/gateway/...` is not
// reachable through the edge's public routes and is not served to a browser,
// which matters more here than anywhere else in this repository: these are the
// two methods that make an outbound connection on somebody else's behalf.
//
// And the colon verbs rather than `/tools` and `/tools/{name}`, because
// neither is a resource. There is nothing at `/internal/v1/gateway/tools` to
// GET: listing is an action performed against a third party, and calling one
// is an action rather than the creation of anything.
func TestTheDeclaredBindingsAreTheOnesTheContractPromises(t *testing.T) {
	want := map[string]httprule.Binding{
		"kindlast.platform.v1.GatewayService.ListTools": {
			Method: "POST", Path: "/internal/v1/gateway/tools:list",
		},
		"kindlast.platform.v1.GatewayService.CallTool": {
			Method: "POST", Path: "/internal/v1/gateway/tools:call",
		},
	}

	got := map[string]httprule.Binding{}
	for _, service := range server.Services() {
		bindings, _ := httprule.OfService(service)
		for name, binding := range bindings {
			got[name] = binding
		}
	}

	for name, expected := range want {
		actual, ok := got[name]
		if !ok {
			t.Errorf("%s declares no binding", name)
			continue
		}
		if actual != expected {
			t.Errorf("%s binds %s, want %s", name, actual, expected)
		}
	}
	for name := range got {
		if _, expected := want[name]; !expected {
			t.Errorf("%s declares a binding this test does not pin; add it, "+
				"so the path is reviewed rather than merely generated", name)
		}
	}
}

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

// ---------------------------------------------------------------------------
// The shared secret, driven through a real HTTP round trip.
// ---------------------------------------------------------------------------

// reachedHandler answers successfully and records that it was reached, which
// is what makes "the request was refused" mean something stronger than "an
// error came back".
type reachedHandler struct {
	platformv1connect.UnimplementedGatewayServiceHandler
	reached bool
}

func (h *reachedHandler) ListTools(
	context.Context, *connect.Request[platformv1.ListToolsRequest],
) (*connect.Response[platformv1.ListToolsResponse], error) {
	h.reached = true
	return connect.NewResponse(&platformv1.ListToolsResponse{}), nil
}

func serve(t *testing.T, handler *reachedHandler) *httptest.Server {
	t.Helper()
	mux, err := server.New(server.Dependencies{Gateway: handler, SharedSecret: secret})
	if err != nil {
		t.Fatalf("building the mux: %v", err)
	}
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestAGatewayCallWithoutTheSharedSecretDoesNotReachTheHandler(t *testing.T) {
	handler := &reachedHandler{}
	s := serve(t, handler)

	for name, token := range map[string]string{
		"nothing at all": "",
		"the wrong one":  "not-the-gateway-secret-at-all",
		"a prefix of it": secret[:len(secret)-1],
	} {
		client := platformv1connect.NewGatewayServiceClient(s.Client(), s.URL,
			connect.WithInterceptors(withToken(token)))

		_, err := client.ListTools(t.Context(), connect.NewRequest(&platformv1.ListToolsRequest{}))
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("%s: got %v (%v), want Unauthenticated", name, err, connect.CodeOf(err))
		}
	}

	if handler.reached {
		t.Fatal("the handler ran for a caller that presented no valid secret")
	}
}

// The guard is only worth having if it can fail. The right secret must reach
// the handler.
func TestTheSecretCheckCanActuallyFail(t *testing.T) {
	handler := &reachedHandler{}
	s := serve(t, handler)

	client := platformv1connect.NewGatewayServiceClient(s.Client(), s.URL,
		connect.WithInterceptors(withToken(secret)))

	if _, err := client.ListTools(t.Context(), connect.NewRequest(&platformv1.ListToolsRequest{})); err != nil {
		t.Fatalf("a caller presenting the right secret was refused: %v", err)
	}
	if !handler.reached {
		t.Fatal("the handler never ran, so the refusals above prove nothing")
	}
}

// A mux built with no secret does not exist, rather than existing and
// refusing everything.
//
// The two are indistinguishable from outside and very different to debug: one
// is a deployment that has not been configured, the other looks like a broken
// gateway.
func TestAMuxWithNoSecretIsNotBuilt(t *testing.T) {
	_, err := server.New(server.Dependencies{Gateway: &reachedHandler{}})
	if err == nil {
		t.Fatal("a mux was built with no shared secret")
	}
	if !strings.Contains(err.Error(), "shared secret") {
		t.Errorf("the error does not say what is missing: %v", err)
	}
}

// The health probe is deliberately unauthenticated, because requiring a
// credential there breaks orchestrator probes for no security gain (§1.7).
func TestTheHealthProbesNeedNoCredential(t *testing.T) {
	s := serve(t, &reachedHandler{})

	for _, path := range []string{"/healthz", "/readyz"} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL+path, nil)
		if err != nil {
			t.Fatalf("building the request: %v", err)
		}
		response, err := s.Client().Do(request)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Errorf("%s answered %s", path, response.Status)
		}
	}
}

func withToken(token string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
			if token != "" {
				request.Header().Set("Authorization", "Bearer "+token)
			}
			return next(ctx, request)
		}
	}
}
