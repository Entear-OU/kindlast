package schedule

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc"
)

// A Temporal frontend that answers, and that does not have the namespace yet.
//
// # WHY THIS IS A FAKE SERVER AND NOT THE SDK'S TEST ENVIRONMENT
//
// The SDK's test environment gives you a working namespace, which is the one
// state this cannot be in to test anything. What is under test is the window
// between "the server answers" and "the namespace exists", so the fake has to
// be able to be in exactly that state and then leave it.
//
// It implements the two calls a dial and a namespace check make and nothing
// else: `GetSystemInfo`, which `DialContext` uses to negotiate capabilities,
// and `DescribeNamespace`. Everything else stays unimplemented, which is a
// feature: if `Connect` ever starts making a third call, this fails rather
// than silently tolerating it.
type bootingFrontend struct {
	workflowservice.UnimplementedWorkflowServiceServer

	// How many DescribeNamespace calls still answer NamespaceNotFound before
	// the namespace appears. Mirrors a real boot, where auto-setup registers it
	// some seconds after the frontend starts listening.
	missingFor atomic.Int32
	describes  atomic.Int32
}

func (f *bootingFrontend) GetSystemInfo(
	context.Context, *workflowservice.GetSystemInfoRequest,
) (*workflowservice.GetSystemInfoResponse, error) {
	return &workflowservice.GetSystemInfoResponse{}, nil
}

func (f *bootingFrontend) DescribeNamespace(
	_ context.Context, req *workflowservice.DescribeNamespaceRequest,
) (*workflowservice.DescribeNamespaceResponse, error) {
	f.describes.Add(1)
	if f.missingFor.Add(-1) >= 0 {
		return nil, serviceerror.NewNamespaceNotFound(req.GetNamespace())
	}
	return &workflowservice.DescribeNamespaceResponse{}, nil
}

// serve starts the fake on a loopback port and returns its address.
func serve(t *testing.T, f *bootingFrontend) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(temporalErrors))
	workflowservice.RegisterWorkflowServiceServer(server, f)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	return listener.Addr().String()
}

// temporalErrors makes the fake fail the way the real frontend fails.
//
// Without it a `NamespaceNotFound` crosses the wire as a bare gRPC status and
// arrives at the client as `*errors.errorString`, because the typed error is
// reconstructed from details the real server attaches with this interceptor
// and a plain `grpc.NewServer` does not. A fake that gets this wrong would
// have made `Connect` look broken while it was correct, or worse, would have
// pushed a string match into production code to satisfy a test artefact.
func temporalErrors(
	ctx context.Context, req any,
	_ *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
) (any, error) {
	resp, err := handler(ctx, req)
	if err != nil {
		return resp, serviceerror.ToStatus(err).Err()
	}
	return resp, nil
}

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// THE BUG THIS EXISTS FOR (ENT-275).
//
// `Connect` used to return as soon as `DialContext` succeeded. A dial succeeds
// against a server that is answering, and the namespace is registered later in
// the same boot, so it could hand back a client for a namespace that did not
// exist. The failure then surfaced in `Start` as a schedule that could not be
// created, which reads as a bug in the schedules rather than as a dependency
// that was not ready, and on a loaded machine it took whole stacks down.
func TestConnectWaitsForTheNamespaceAndNotOnlyForTheServer(t *testing.T) {
	frontend := &bootingFrontend{}
	frontend.missingFor.Store(2)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	c, err := Connect(ctx, Options{
		Addr:      serve(t, frontend),
		Namespace: "default",
		Logger:    quiet(),
	})
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer c.Close()

	// Three calls: two refused, one that found it. If `Connect` returned on the
	// dial alone this is 1, and if it never asked at all it is 0. The count is
	// the assertion, because a client came back in every one of those cases.
	if got := frontend.describes.Load(); got != 3 {
		t.Errorf("asked about the namespace %d times, want 3", got)
	}
}

func TestConnectGivesUpWhenTheNamespaceNeverArrives(t *testing.T) {
	// A namespace that is never registered is a misconfiguration rather than a
	// slow boot, and the operator has to be told which of the two it is. The
	// message names the namespace for exactly that reason.
	frontend := &bootingFrontend{}
	frontend.missingFor.Store(1 << 30)

	// Cancelled rather than left to run thirty attempts at two seconds each.
	// What is under test is that it keeps waiting, and the context ending is
	// the cheapest proof it was still waiting when time ran out.
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	c, err := Connect(ctx, Options{
		Addr:      serve(t, frontend),
		Namespace: "default",
		Logger:    quiet(),
	})
	if err == nil {
		c.Close()
		t.Fatal("connected to a namespace that does not exist")
	}
	if frontend.describes.Load() < 2 {
		t.Errorf("gave up after %d attempts, so it is not retrying",
			frontend.describes.Load())
	}
}

func TestConnectStopsOnAnErrorThatIsNotAMissingNamespace(t *testing.T) {
	// A server refusing the question for any other reason is a real error and
	// retrying it thirty times wastes a minute before saying so. `Unimplemented`
	// stands in for that here, since the embedded base returns it for anything
	// the fake does not override.
	blank := &bootingFrontend{}
	blank.missingFor.Store(0)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// A namespace the fake will answer for, so the failure below can only come
	// from the call this test is about.
	c, err := Connect(ctx, Options{Addr: serve(t, blank), Namespace: "default", Logger: quiet()})
	if err != nil {
		t.Fatalf("the fake should answer for this namespace: %v", err)
	}
	c.Close()

	// And now one that refuses with something else entirely.
	refusing := &refusingFrontend{}
	started := time.Now()
	_, err = Connect(ctx, Options{Addr: serve2(t, refusing), Namespace: "default", Logger: quiet()})
	if err == nil {
		t.Fatal("a refused question was treated as a namespace that will appear")
	}
	if !strings.Contains(err.Error(), "refused a question about namespace") {
		t.Errorf("error does not say what happened: %v", err)
	}
	// Two seconds is one retry interval. Anything at or above it means the
	// error was queued behind a sleep rather than returned.
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Errorf("took %s to report an error it should not have retried", elapsed)
	}
}

type refusingFrontend struct {
	workflowservice.UnimplementedWorkflowServiceServer
}

func (f *refusingFrontend) GetSystemInfo(
	context.Context, *workflowservice.GetSystemInfoRequest,
) (*workflowservice.GetSystemInfoResponse, error) {
	return &workflowservice.GetSystemInfoResponse{}, nil
}

func serve2(t *testing.T, f *refusingFrontend) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(temporalErrors))
	workflowservice.RegisterWorkflowServiceServer(server, f)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}
