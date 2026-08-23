package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// The loader gets its own token, so a fresh stack can load the corpus without
// a person pasting one (ENT-266).
//
// # WHY THIS IS THE TEST THAT MATTERS
//
// Until ENT-266 this command took a bearer token on the command line and had
// no other way in. That is fine for a curator at a terminal and impossible for
// a container: a job on the compose network has a client id and a secret on a
// volume, not a token, because a token lives ten minutes and the stack takes
// longer than that to come up on a cold laptop.
//
// So the job holds credentials and mints, which is the grant every other
// machine principal in this system already uses. These tests drive that path
// against a fake authorization server and a fake core-api rather than the
// stack, because the failure they exist to catch is a request that reaches the
// ingest endpoint carrying no authority: that one presents as
// `permission_denied` three services away and gets diagnosed as a missing
// grant.

// fixtureCorpus writes the smallest corpus that parses: a manifest naming one
// bibliography pack, and that pack.
//
// Small on purpose. What is under test is how the loader authenticates, not
// how it parses the law, and the real files are checked by the drift guard in
// internal/store/postgres.
func fixtureCorpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	write := func(name, contents string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	write("packs.json", `{
  "packs": [
    {
      "id": "test-guidelines",
      "kind": "guidelines",
      "file": "guidelines.json",
      "title": "Guidelines, for a test"
    }
  ]
}`)
	write("guidelines.json", `{
  "guidelines": [
    {
      "slug": "edpb-05-2020-consent",
      "publisher": "EDPB",
      "title": "Guidelines 05/2020 on consent",
      "adoptedDate": "2020-05-04",
      "version": "1.1",
      "sourceUrl": "https://edpb.europa.eu/example",
      "topicTags": ["consent"]
    }
  ]
}`)

	return dir
}

// fakeCoreAPI answers IngestCorpus and records the Authorization header it was
// given.
func fakeCoreAPI(t *testing.T, seen *string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/IngestCorpus") {
				http.NotFound(w, r)
				return
			}
			*seen = r.Header.Get("Authorization")
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"applied":true,"counts":{"guidelines":1}}`))
		}))
	t.Cleanup(server.Close)
	return server
}

// fakeAuthServer serves discovery and a token endpoint, and records the form
// the token was asked for with.
//
// `failDiscoveries` refuses that many discovery requests before answering, so
// a test can drive the wait the compose job needs: `auth` and this job start
// together and the first discovery routinely lands before Zitadel answers.
func fakeAuthServer(
	t *testing.T, token string, failDiscoveries int32, lastForm *map[string][]string,
) *httptest.Server {
	t.Helper()

	var remaining atomic.Int32
	remaining.Store(failDiscoveries)

	mux := http.NewServeMux()
	var server *httptest.Server

	mux.HandleFunc(oidc.DiscoveryPath, func(w http.ResponseWriter, r *http.Request) {
		if remaining.Add(-1) >= 0 {
			http.Error(w, "still starting", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 server.URL,
			"jwks_uri":               server.URL + "/oauth/v2/keys",
			"token_endpoint":         server.URL + "/oauth/v2/token",
			"authorization_endpoint": server.URL + "/oauth/v2/authorize",
		})
	})

	mux.HandleFunc("/oauth/v2/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if lastForm != nil {
			*lastForm = r.Form
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": token,
			"token_type":   "Bearer",
			"expires_in":   600,
		})
	})

	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestItMintsItsOwnTokenFromClientCredentials(t *testing.T) {
	var authorization string
	coreAPI := fakeCoreAPI(t, &authorization)

	var form map[string][]string
	auth := fakeAuthServer(t, "a-minted-token", 0, &form)

	err := run([]string{
		"-api", coreAPI.URL,
		"-dir", fixtureCorpus(t),
		"-oidc-discovery-url", auth.URL + oidc.DiscoveryPath,
		"-oidc-issuer", auth.URL,
		"-audience", "the-project-id",
		"-client-id", "core-api-client",
		"-client-secret", "a-secret",
	}, io.Discard)
	if err != nil {
		t.Fatalf("loading with client credentials: %v", err)
	}

	if authorization != "Bearer a-minted-token" {
		t.Errorf("ingest saw Authorization %q, want the minted token", authorization)
	}

	if got := form["grant_type"]; len(got) != 1 || got[0] != "client_credentials" {
		t.Errorf("grant_type was %v, want client_credentials", got)
	}

	// The two Zitadel scopes, both of them. The audience scope alone gets a
	// token that authenticates perfectly and carries no roles, which core-api
	// reports as permission denied and which sends somebody reading grants
	// that are already correct. The plural in `projects:roles` is not a typo.
	scope := strings.Join(form["scope"], " ")
	for _, want := range []string{
		"urn:zitadel:iam:org:projects:roles",
		"urn:zitadel:iam:org:project:id:the-project-id:aud",
	} {
		if !strings.Contains(scope, want) {
			t.Errorf("scope %q does not request %q", scope, want)
		}
	}
}

func TestItWaitsForAnAuthorizationServerThatIsStillStarting(t *testing.T) {
	var authorization string
	coreAPI := fakeCoreAPI(t, &authorization)
	auth := fakeAuthServer(t, "a-minted-token", 3, nil)

	err := run([]string{
		"-api", coreAPI.URL,
		"-dir", fixtureCorpus(t),
		"-oidc-discovery-url", auth.URL + oidc.DiscoveryPath,
		"-oidc-issuer", auth.URL,
		"-audience", "the-project-id",
		"-client-id", "core-api-client",
		"-client-secret", "a-secret",
		// Short, so the test does not spend the default wait on a server that
		// answers on the fourth attempt.
		"-retry-interval", "10ms",
		"-wait", "5s",
	}, io.Discard)
	if err != nil {
		t.Fatalf("loading against a slow authorization server: %v", err)
	}

	if authorization != "Bearer a-minted-token" {
		t.Errorf("ingest saw Authorization %q, want the minted token", authorization)
	}
}

func TestAGivenTokenIsUsedAsItIs(t *testing.T) {
	var authorization string
	coreAPI := fakeCoreAPI(t, &authorization)

	// Pointed at an address nothing is listening on. A run that contacts an
	// authorization server when it was handed a token would fail here, which
	// is the assertion: a curator's token is used, not exchanged.
	err := run([]string{
		"-api", coreAPI.URL,
		"-dir", fixtureCorpus(t),
		"-token", "a-curators-token",
		"-oidc-discovery-url", "http://127.0.0.1:1/.well-known/openid-configuration",
		"-oidc-issuer", "http://127.0.0.1:1",
		"-client-id", "core-api-client",
		"-client-secret", "a-secret",
	}, io.Discard)
	if err != nil {
		t.Fatalf("loading with a given token: %v", err)
	}

	if authorization != "Bearer a-curators-token" {
		t.Errorf("ingest saw Authorization %q, want the token it was given", authorization)
	}
}

func TestItRefusesWithNeitherATokenNorCredentials(t *testing.T) {
	err := run([]string{"-api", "http://127.0.0.1:1", "-dir", fixtureCorpus(t)}, io.Discard)
	if err == nil {
		t.Fatal("a run with no way to authenticate succeeded, and must not")
	}

	// Both ways in, named. The message is the whole of what an operator gets:
	// there is no log to correlate and no second attempt to learn from.
	for _, want := range []string{"-token", "internal:ingest", "-client-id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

func TestTheCredentialsFileIsReadAsZitadelWroteIt(t *testing.T) {
	var authorization string
	coreAPI := fakeCoreAPI(t, &authorization)

	var form map[string][]string
	auth := fakeAuthServer(t, "a-minted-token", 0, &form)

	dir := t.TempDir()
	credentials := filepath.Join(dir, "core-api-client.json")
	if err := os.WriteFile(credentials,
		[]byte(`{"clientId":"core-api-client","clientSecret":"a-secret"}`), 0o600); err != nil {
		t.Fatalf("writing the credentials file: %v", err)
	}
	audience := filepath.Join(dir, "core-api-audience.txt")
	if err := os.WriteFile(audience, []byte("the-project-id\n"), 0o600); err != nil {
		t.Fatalf("writing the audience file: %v", err)
	}

	err := run([]string{
		"-api", coreAPI.URL,
		"-dir", fixtureCorpus(t),
		"-oidc-discovery-url", auth.URL + oidc.DiscoveryPath,
		"-oidc-issuer", auth.URL,
		"-audience-file", audience,
		"-client-file", credentials,
	}, io.Discard)
	if err != nil {
		t.Fatalf("loading from the files the seed writes: %v", err)
	}

	if authorization != "Bearer a-minted-token" {
		t.Errorf("ingest saw Authorization %q, want the minted token", authorization)
	}

	// Trailing newline and all: the audience file is written by a shell
	// `printf` on a volume, and an audience with a newline in it produces a
	// scope Zitadel does not recognise and a token with no roles.
	if got := strings.Join(form["scope"], " "); !strings.Contains(
		got, "urn:zitadel:iam:org:project:id:the-project-id:aud") {
		t.Errorf("scope %q did not carry the audience read from the file", got)
	}

	if got := form["client_id"]; len(got) != 1 || got[0] != "core-api-client" {
		t.Errorf("client_id was %v, want the one in the file", got)
	}
}
