package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// The listener serves HTTP/1.1 and unencrypted HTTP/2 on one port (ENT-216).
//
// # WHY THIS IS A TEST AND NOT A REVIEW COMMENT
//
// Replacing `h2c.NewHandler` with `http.Server.Protocols` changes protocol
// negotiation, and both ways of getting it wrong compile, pass every unit test
// in this repository, and only fail against a real client:
//
//   - without HTTP/1.1, gRPC works and the console does not
//   - without unencrypted HTTP/2, the console works and gRPC does not
//
// So the assertion has to be a request over a socket, with the protocol the
// server actually negotiated read back off the response. Verified against a
// deliberately broken server (each bit cleared in turn) before being trusted;
// each direction fails the matching subtest and no other.
func TestListenerServesBothProtocols(t *testing.T) {
	// Echoes the protocol it was reached over, so the assertion is what the
	// server negotiated rather than what the client asked for.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Proto)
	})

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()

	server := newHTTPServer(addr, handler)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	t.Run("HTTP/1.1, which the console and the health probe use", func(t *testing.T) {
		// Protocols left at the default, so this transport will not attempt h2c.
		client := &http.Client{Transport: &http.Transport{}}
		if got := proto(t, client, addr); got != "HTTP/1.1" {
			t.Fatalf("negotiated %q, want HTTP/1.1", got)
		}
	})

	t.Run("unencrypted HTTP/2, which gRPC needs", func(t *testing.T) {
		// h2c from the standard library rather than golang.org/x/net: dropping
		// that dependency is half the point of ENT-216, and reaching for it here
		// would put it straight back into go.mod as a test dependency.
		var protocols http.Protocols
		protocols.SetUnencryptedHTTP2(true)
		client := &http.Client{Transport: &http.Transport{Protocols: &protocols}}

		if got := proto(t, client, addr); got != "HTTP/2.0" {
			t.Fatalf("negotiated %q, want HTTP/2.0 (plaintext gRPC would not work)", got)
		}
	})
}

func proto(t *testing.T, client *http.Client, addr string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return string(body)
}
