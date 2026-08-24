package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
)

// Every console-facing RPC is actually mounted on the mux (ENT-278).
//
// # THE OTHER HALF OF THE ENT-245 FAILURE
//
// `TestEveryKindlastServiceIsClassified` proves the registry names every
// service this binary could serve, because NarrativeService once shipped
// mounted and unlisted and the scope interceptor default-denied every call to
// it. The mirror image is a service that is listed, declares its scope, passes
// the binding test, and is never handed to `mux.Handle`. Every test in this
// package would stay green and every request would 404.
//
// That gap was closed by driving the live stack, which is the right thing to do
// and is not something the Go suite can do for a surface that needs a browser
// sign-in. This is what the Go suite CAN prove: that a request to the path
// reaches the interceptor chain rather than the mux's fallback.
//
// # WHY `Dependencies{}` IS EMPTY, AND WHY THE ASSERTION IS "NOT 404"
//
// A handler mounted with no dependencies still answers: the chain runs first,
// so an unauthenticated request is refused before anything reads a database.
// That is the whole point of the assertion. It cannot be "200", because there
// is no token here and there should not be; it cannot be "401" either, because
// which stage refuses is that stage's business and pinning it here would make
// this test fail every time the chain is reordered for a good reason.
//
// A 404 from this mux means one thing only, and it is the thing being tested.
func TestEveryCoreServiceIsMountedOnTheMux(t *testing.T) {
	// TWO CONSOLE SERVICES ARE CONDITIONAL, AND SUPPLYING THEM PROVES MORE.
	//
	// `IntegrationsService` is registered only when a gateway is configured,
	// and `ModelService` only when a model choice handler is, both by design
	// and both argued for where `Dependencies` declares them: a console page
	// whose every button errors is worse than one that is absent, because the
	// first looks broken and the second looks unconfigured.
	//
	// So an empty `Dependencies{}` leaves them nil and correctly unmounted,
	// and asserting they answer would fail against working code. The first
	// draft of this test did exactly that and reported ten RPCs missing on a
	// binary that serves them.
	//
	// Handing over the generated no-op handlers is the honest fix rather than
	// skipping the two, because it turns the assertion into the one worth
	// making: WHEN a deployment configures these, are they reachable. Skipping
	// them would leave the ENT-245 shape uncovered on the two services most
	// likely to be wired up wrong, since they are the two with a condition to
	// get wrong in the first place.
	handler, err := server.New(server.Dependencies{
		Integrations: corev1connect.UnimplementedIntegrationsServiceHandler{},
		ModelChoice:  corev1connect.UnimplementedModelServiceHandler{},
	})
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	var checked int
	for _, service := range server.Services() {
		// The console-facing half only. The platform surface is registered
		// conditionally on a deployment having an agent pool, an ingest pool or
		// a model, all of which are absent from `Dependencies{}` above, so a
		// 404 there is correct rather than a bug.
		if !strings.HasPrefix(string(service.FullName()), "kindlast.core.v1.") {
			continue
		}

		methods := service.Methods()
		for i := 0; i < methods.Len(); i++ {
			checked++
			assertMounted(t, handler, service, methods.Get(i))
		}
	}

	if checked == 0 {
		t.Fatal("no console-facing RPCs were checked; the registry is the input")
	}
}

func assertMounted(
	t *testing.T,
	handler http.Handler,
	service protoreflect.ServiceDescriptor,
	method protoreflect.MethodDescriptor,
) {
	t.Helper()

	path := "/" + string(service.FullName()) + "/" + string(method.Name())
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code == http.StatusNotFound {
		t.Errorf("%s answers 404: the contract declares it and nothing mounts it", path)
	}
}
