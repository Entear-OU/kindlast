package server_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
)

// The approve-from-email endpoint (ENT-249, §8).
//
// TWO PROPERTIES, AND THE FIRST IS THE REASON THIS FILE EXISTS.
//
// A GET must mutate nothing. Corporate mail gateways, link previewers and
// archiving proxies fetch every URL in a message before a human sees it, so
// under a GET the act of DELIVERING a finding notification would approve the
// finding: an approval in a customer's compliance record, with an audit row
// naming somebody who never opened the message. The tests below send exactly
// what those scanners send, headers and all, and assert that nothing behind the
// endpoint was called at all.
//
// And every unusable link answers identically. Expired, revoked, already
// redeemed, minted for a different finding, minted for somebody since removed,
// and never existed are one 404 with one body, because a caller presenting a
// credential has proved nothing that entitles them to the difference.
//
// PROVEN ABLE TO FAIL. Two deliberate breakages, both reverted:
//
//   - Registering the route as `/api/v1/approve` rather than
//     `POST /api/v1/approve` turns every mail-scanner case red at once, and
//     leaves the rest of the file green.
//   - Answering 400 rather than 404 for a request missing the token turns the
//     identical-answer test red on its own.

// approvalSpy answers however a test tells it to, and records every call.
//
// Recording is the assertion for the GET cases: the property is not "a GET
// returns an error", it is "a GET never reaches the thing that can approve".
type approvalSpy struct {
	calls    int
	orgSlug  string
	applied  bool
	failWith error
}

func (a *approvalSpy) ApproveFromEmail(_ context.Context, _, _ string) (string, bool, error) {
	a.calls++
	if a.failWith != nil {
		return "", false, a.failWith
	}
	return a.orgSlug, a.applied, nil
}

func handlerWith(t *testing.T, approvals server.Approvals) http.Handler {
	t.Helper()
	handler, err := server.New(server.Dependencies{Approvals: approvals})
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}
	return handler
}

// Every method a scanner, previewer or proxy uses, with the shapes they use.
//
// The last entry is the sharp one. Most scanners fetch the URL and nothing
// else, so a method-agnostic route would refuse them by accident when the JSON
// decode failed, and a test built only from those would report a safety that
// came from the body parser rather than from the routing. A gateway that
// replays a captured request keeps the body, so that case reaches the approval
// path the moment the method guard is gone.
var mailScannerRequests = []struct {
	name      string
	method    string
	userAgent string
	body      string
}{
	{"a corporate link scanner", http.MethodGet, "Mozilla/5.0 (compatible; Barracuda-Link-Protect)", ""},
	{"an Outlook Safe Links prefetch", http.MethodGet, "Microsoft Office Existence Discovery", ""},
	{"a Slack unfurl of a forwarded message", http.MethodGet, "Slackbot-LinkExpanding 1.0", ""},
	{"a previewer that only takes headers", http.MethodHead, "WhatsApp/2.0", ""},
	{"a browser preflight", http.MethodOptions, "Mozilla/5.0", ""},
	// Not a scanner: a person who bookmarked the link and came back to it in a
	// browser. Same shape, same answer, and worth listing because it is the
	// case somebody would be tempted to make convenient.
	{"a human revisiting the URL directly", http.MethodGet, "Mozilla/5.0 (Macintosh)", ""},
	{"a gateway replaying a captured request as a GET", http.MethodGet,
		"Mozilla/5.0 (compatible; MailGateway)", `{"token":"deleg-1","findingId":"f-1"}`},
}

func TestAMailScannerApprovesNothing(t *testing.T) {
	t.Parallel()

	for _, scanner := range mailScannerRequests {
		t.Run(scanner.name, func(t *testing.T) {
			t.Parallel()

			spy := &approvalSpy{orgSlug: "acme-gmbh", applied: true}
			recorder := httptest.NewRecorder()

			// A real link out of a real message: the finding and the credential
			// are both in the URL, which is what a scanner would fetch.
			var body io.Reader
			if scanner.body != "" {
				body = strings.NewReader(scanner.body)
			}
			request := httptest.NewRequestWithContext(t.Context(), scanner.method,
				"/api/v1/approve?findingId=f-1&token=deleg-1", body)
			request.Header.Set("User-Agent", scanner.userAgent)
			request.Header.Set("Accept", "text/html,application/xhtml+xml")

			handlerWith(t, spy).ServeHTTP(recorder, request)

			// The assertion that matters. Not the status code: whether anything
			// capable of approving a finding was reached at all.
			if spy.calls != 0 {
				t.Fatalf("%s reached the approval path %d times", scanner.method, spy.calls)
			}
			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s answered %d, want 405", scanner.method, recorder.Code)
			}
		})
	}
}

func TestOnlyThePostApproves(t *testing.T) {
	t.Parallel()

	spy := &approvalSpy{orgSlug: "acme-gmbh", applied: true}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/approve",
		strings.NewReader(`{"token":"deleg-1","findingId":"f-1"}`))
	request.Header.Set("Content-Type", "application/json")

	handlerWith(t, spy).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("the POST answered %d: %s", recorder.Code, recorder.Body.String())
	}
	if spy.calls != 1 {
		t.Fatalf("the approval path was reached %d times, want once", spy.calls)
	}
	// The slug comes back because the interstitial's next move is to send the
	// person into `/o/{slug}/`, and §8 requires that to be the organisation the
	// credential named rather than wherever their session pointed.
	if body := recorder.Body.String(); !strings.Contains(body, `"orgSlug":"acme-gmbh"`) {
		t.Fatalf("the answer does not name the organisation: %s", body)
	}
}

func TestEveryUnusableLinkAnswersIdentically(t *testing.T) {
	t.Parallel()

	// The four the schema refuses are indistinguishable to this handler by
	// construction: `resolve_act_delegation` returns the same nothing for all
	// of them and the store turns that into one error. What is asserted here is
	// that the handler does not reintroduce a difference of its own, which is
	// exactly what a well-meaning 400 for a malformed request would do.
	cases := []struct {
		name string
		body string
		spy  *approvalSpy
	}{
		{"expired, revoked, redeemed, wrong finding or never existed",
			`{"token":"deleg-1","findingId":"f-1"}`,
			&approvalSpy{failWith: errors.New("delegation: no usable delegation")}},
		{"no token at all", `{"findingId":"f-1"}`, &approvalSpy{}},
		{"no finding named", `{"token":"deleg-1"}`, &approvalSpy{}},
		{"neither", `{}`, &approvalSpy{}},
	}

	var answers []string
	for _, c := range cases {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(
			t.Context(), http.MethodPost, "/api/v1/approve", strings.NewReader(c.body))
		request.Header.Set("Content-Type", "application/json")
		handlerWith(t, c.spy).ServeHTTP(recorder, request)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s answered %d, want 404", c.name, recorder.Code)
		}
		answers = append(answers, recorder.Body.String())
	}

	for i, answer := range answers {
		if answer != answers[0] {
			t.Fatalf("%s answered %q where the first case answered %q; "+
				"the difference tells a caller which credentials are real",
				cases[i].name, answer, answers[0])
		}
	}
}

func TestAnUnwiredDeploymentRefusesRatherThanPanicking(t *testing.T) {
	t.Parallel()

	// Nil is a supported deployment: a stack with no database wired has no
	// approve path. 501 says so, where a panic would take the process down on a
	// request anybody can make without a credential.
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/approve",
		strings.NewReader(`{"token":"deleg-1","findingId":"f-1"}`))
	handlerWith(t, nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("an unwired deployment answered %d, want 501", recorder.Code)
	}
}

func TestAlreadyApprovedIsNotAnError(t *testing.T) {
	t.Parallel()

	// A second click, a retry, or a colleague who got there first. The finding
	// is approved either way, and answering 404 would tell somebody their link
	// was bad when what actually happened is that the thing they wanted is
	// done.
	spy := &approvalSpy{orgSlug: "acme-gmbh", applied: false}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/approve",
		strings.NewReader(`{"token":"deleg-1","findingId":"f-1"}`))
	handlerWith(t, spy).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("an already-approved finding answered %d", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"applied":false`) {
		t.Fatalf("the answer does not distinguish a repeat from a first approval: %s", body)
	}
}
