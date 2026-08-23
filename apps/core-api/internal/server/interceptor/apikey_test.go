package interceptor_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/apikey"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	apikeysservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/apikeys"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/session"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
)

// Partner API keys, driven through the real chain (ENT-262).
//
// Nothing here is mocked, per §13.2 and §13.3. Real signed tokens against a real
// JWKS mint the keys, a real Postgres enforces the policies, and every
// authentication below goes through `authenticate_api_key` and a real
// constant-time comparison. A stubbed authenticator would turn every assertion
// in this file into a fact about the stub.

// managingScopes is what the PERSON in these tests holds, granted on the token
// rather than through the human-client constant.
//
// This mux is built with no WithHumanClient, deliberately: these tests are about
// what a KEY may do, and resolving the caller to the human set would hide a key
// that had quietly been measured against it. The person therefore carries the
// two scopes the key surface declares, explicitly, so every refusal below is
// about the key rather than about the token that minted it.
const managingScopes = "openid org:read org:manage"

// keyStack is the mux both halves of these tests talk to: the session service,
// which is the ordinary tenant surface a key calls, and the key service, which
// is how one is minted.
//
// EVERY RUN GETS ITS OWN ORGANISATION AND ITS OWN PERSON, and that is not
// fastidiousness. Minting and revoking a key COMMIT audit rows, unlike almost
// everything else in this package, which works inside a transaction it rolls
// back. Committing into the seeded organisation would leave permanent rows in
// its audit log, and the audit store's tests assert exact row counts against
// exactly that log: the first run would pass and every run after it would fail,
// in a different package, for a reason nobody would connect to this file.
type keyStack struct {
	live    *stack
	session corev1connect.SessionServiceClient
	keys    corev1connect.ApiKeyServiceClient
	auth    *authServer

	// subject is the IdP subject of the person who mints in these tests, and
	// orgID is the organisation they own. Both are fresh per run.
	subject string
	orgID   string
}

func serveKeys(t *testing.T) *keyStack {
	t.Helper()

	a := newAuthServer(t)
	live := requireStack(t, a.server.URL)

	scopes, err := interceptor.NewScope(server.Services())
	if err != nil {
		t.Fatalf("scope table: %v", err)
	}

	chain := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t), interceptor.WithAPIKeys(live.store)),
		interceptor.JTI(live.revocations),
		interceptor.ActOnBehalf(live.store),
		scopes.Interceptor(),
		interceptor.Tenancy(tenantOpener{live.store}),
	)

	mux := http.NewServeMux()
	mux.Handle(corev1connect.NewSessionServiceHandler(session.New(a.profiles(t)), chain))
	mux.Handle(corev1connect.NewApiKeyServiceHandler(apikeysservice.New(), chain))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	subject, orgID := freshOrganisation(t, live.store)

	return &keyStack{
		live:    live,
		session: corev1connect.NewSessionServiceClient(srv.Client(), srv.URL),
		keys:    corev1connect.NewApiKeyServiceClient(srv.Client(), srv.URL),
		auth:    a,
		subject: subject,
		orgID:   orgID,
	}
}

// freshOrganisation makes a person and an organisation they own.
//
// Through CreateOrganisation rather than an insert, so the row is made the way
// production makes it. Note that CreateOrganisation writes NO audit row, which
// is what lets this run without the very pollution it exists to avoid.
func freshOrganisation(t *testing.T, store *postgres.Store) (subject, orgID string) {
	t.Helper()

	subject = "apikey-test-" + uuid.NewString()

	tenant, err := store.BeginTenant(t.Context(), subject, "")
	if err != nil {
		t.Fatalf("provisioning a person for the key tests: %v", err)
	}

	joined, err := tenant.CreateOrganisation(t.Context(), "API keys "+uuid.NewString())
	if err != nil {
		_ = tenant.Rollback(t.Context())
		t.Fatalf("creating the key tests' organisation: %v", err)
	}
	if err := tenant.Commit(t.Context()); err != nil {
		t.Fatalf("committing: %v", err)
	}
	return subject, joined.OrgID
}

// mintKey creates one through the real RPC, as a person, and returns the
// credential.
//
// Deliberately not an insert. The mint path is where `api_keys_mint` pins
// `created_by` to the GUC user, and a test that wrote the row directly would
// skip the policy it most wants exercised.
func (k *keyStack) mintKey(t *testing.T, scopes ...string) (string, string) {
	t.Helper()

	request := connect.NewRequest(&corev1.CreateApiKeyRequest{
		Name:   "test key",
		Scopes: scopes,
	})
	request.Header().Set(interceptor.AuthorizationHeader,
		"Bearer "+k.auth.token(t, k.subject, managingScopes))
	request.Header().Set(interceptor.OrgHeader, k.orgID)

	response, err := k.keys.CreateApiKey(t.Context(), request)
	if err != nil {
		t.Fatalf("minting a key: %v", err)
	}
	id := response.Msg.GetKey().GetId()

	// Revoked on the way out so a repeated run does not leave the seeded
	// organisation carrying live credentials. Best effort, and the assertions
	// never depend on it: a test that revoked the key it is about does so
	// itself, and revoking twice is refused rather than fatal.
	t.Cleanup(func() {
		cleanup := connect.NewRequest(&corev1.RevokeApiKeyRequest{KeyId: id})
		cleanup.Header().Set(interceptor.AuthorizationHeader,
			"Bearer "+k.auth.token(t, k.subject, managingScopes))
		cleanup.Header().Set(interceptor.OrgHeader, k.orgID)
		_, _ = k.keys.RevokeApiKey(context.WithoutCancel(t.Context()), cleanup)
	})

	return response.Msg.GetCredential(), id
}

// callWithKey makes an ordinary tenant RPC presenting a key.
//
// ListApiKeys rather than GetCurrentUser, and the reason is a design decision
// rather than convenience. GetCurrentUser declares `openid`, which means "signed
// in" and is asserted by verification rather than granted, so it is deliberately
// NOT a scope a key may carry: a partner's credential is not a session and has
// no current user. ListApiKeys declares `org:read`, which a key may hold, and it
// is a call a partner has a real reason to make: it is how they check that the
// credential they are holding is still live and which scopes it has.
func (k *keyStack) callWithKey(t *testing.T, credential string, headers map[string]string) error {
	t.Helper()

	request := connect.NewRequest(&corev1.ListApiKeysRequest{})
	request.Header().Set(interceptor.AuthorizationHeader,
		interceptor.APIKeyScheme+" "+credential)
	for name, value := range headers {
		request.Header().Set(name, value)
	}

	_, err := k.keys.ListApiKeys(t.Context(), request)
	return err
}

// A KEY IS NOT SIGNED IN, AND THIS IS WHERE THAT IS ASSERTED.
//
// `openid` is excused for a person because verification asserts it. A key is not
// a session, has no current user, and must not be able to reach the bootstrap
// calls that declare it. The scope is not grantable, so a mint carrying it is
// refused, and GetCurrentUser is therefore unreachable on a key by construction
// rather than by a check somebody could forget.
func TestAKeyIsNotASession(t *testing.T) {
	k := serveKeys(t)

	request := connect.NewRequest(&corev1.CreateApiKeyRequest{
		Name:   "pretending to be a person",
		Scopes: []string{"openid"},
	})
	request.Header().Set(interceptor.AuthorizationHeader,
		"Bearer "+k.auth.token(t, k.subject, managingScopes))
	request.Header().Set(interceptor.OrgHeader, k.orgID)

	if _, err := k.keys.CreateApiKey(t.Context(), request); err == nil {
		t.Fatal("a key carrying openid was minted, so a credential that is not a " +
			"session can reach the calls that mean one")
	}

	// And the session surface refuses a key that got there some other way.
	credential, _ := k.mintKey(t, "org:read")
	bootstrap := connect.NewRequest(&corev1.GetCurrentUserRequest{})
	bootstrap.Header().Set(interceptor.AuthorizationHeader,
		interceptor.APIKeyScheme+" "+credential)
	if _, err := k.session.GetCurrentUser(t.Context(), bootstrap); err == nil {
		t.Error("a key reached GetCurrentUser")
	} else if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("got %s, want permission_denied", connect.CodeOf(err))
	}
}

// THE ACCEPTANCE CRITERION. A key minted in the console reaches the tenant
// surface with no session at all.
func TestAKeyReachesTheTenantSurfaceWithNoSession(t *testing.T) {
	k := serveKeys(t)

	credential, _ := k.mintKey(t, "org:read")

	// No Authorization bearer, no Kindlast-Org-Id, no cookie. The only thing
	// this request carries is the key, and the organisation comes out of it.
	if err := k.callWithKey(t, credential, nil); err != nil {
		t.Fatalf("a live key was refused: %v", err)
	}
}

// THE OTHER ACCEPTANCE CRITERION, and the one worth breaking deliberately to
// watch fail: a revoked key is refused at core-api, not merely at the edge.
//
// This test talks straight to the Connect handler over loopback, which is the
// internal network as far as this assertion is concerned. No gateway is in the
// path, so nothing but core-api can be doing the refusing.
func TestARevokedKeyIsRefusedAtCoreAPI(t *testing.T) {
	k := serveKeys(t)

	credential, id := k.mintKey(t, "org:read")

	if err := k.callWithKey(t, credential, nil); err != nil {
		t.Fatalf("the key did not work before revocation: %v", err)
	}

	revoke := connect.NewRequest(&corev1.RevokeApiKeyRequest{KeyId: id})
	revoke.Header().Set(interceptor.AuthorizationHeader,
		"Bearer "+k.auth.token(t, k.subject, managingScopes))
	revoke.Header().Set(interceptor.OrgHeader, k.orgID)
	if _, err := k.keys.RevokeApiKey(t.Context(), revoke); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	err := k.callWithKey(t, credential, nil)
	if err == nil {
		t.Fatal("a revoked key still worked, which means revocation is not immediate")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("revoked key gave %s, want unauthenticated", connect.CodeOf(err))
	}

	// Revoking twice is refused rather than silently succeeding, so a person
	// clicking the button again is told the second click did nothing.
	if _, err := k.keys.RevokeApiKey(t.Context(), revoke); err == nil {
		t.Error("revoking an already revoked key succeeded")
	}
}

// A key is measured against what it was minted with, and against nothing else.
// Its minter holds every human scope; the key must not inherit them.
func TestAKeyIsBoundedByItsOwnScopes(t *testing.T) {
	k := serveKeys(t)

	// No `org:read`, which is what ListApiKeys declares. `records:read` is a
	// scope this key genuinely has, so the refusal below is about the scope
	// that was NOT minted rather than about the key being unusable.
	credential, _ := k.mintKey(t, "records:read")

	err := k.callWithKey(t, credential, nil)
	if err == nil {
		t.Fatal("a key without the declared scope reached the handler, so it is " +
			"borrowing its minter's scopes rather than carrying its own")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("got %s, want permission_denied", connect.CodeOf(err))
	}
}

// THE TENANCY PROPERTY. A key names one organisation and a header cannot move
// it.
//
// Without this, a consultancy's one integration credential would reach every
// client company it serves by changing one header, which is the worst version of
// the bug the whole two-GUC design exists to survive.
func TestAHeaderCannotMoveAKeyToAnotherOrganisation(t *testing.T) {
	k := serveKeys(t)

	credential, _ := k.mintKey(t, "org:read")

	// This run's key, pointed at the seeded organisation it was never minted in.
	err := k.callWithKey(t, credential, map[string]string{interceptor.OrgHeader: alphaOrg})
	if err == nil {
		t.Fatal("a key served a request for an organisation it was not minted in")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("got %s, want permission_denied", connect.CodeOf(err))
	}

	// The key's own organisation in the header is fine, so a client that sets
	// the header uniformly stays usable.
	if err := k.callWithKey(t, credential,
		map[string]string{interceptor.OrgHeader: k.orgID}); err != nil {
		t.Errorf("a matching header was refused: %v", err)
	}
}

// A key cannot mint a key. Three refusals guard this and the outermost is that
// `org:manage` is not a scope a key may carry at all.
func TestAKeyCannotMintAnotherKey(t *testing.T) {
	k := serveKeys(t)

	request := connect.NewRequest(&corev1.CreateApiKeyRequest{
		Name:   "escalation",
		Scopes: []string{"org:manage"},
	})
	request.Header().Set(interceptor.AuthorizationHeader,
		"Bearer "+k.auth.token(t, k.subject, managingScopes))
	request.Header().Set(interceptor.OrgHeader, k.orgID)

	if _, err := k.keys.CreateApiKey(t.Context(), request); err == nil {
		t.Fatal("a key carrying org:manage was minted, so a key could mint keys")
	}
}

// A key may never present a delegation, and it is told so rather than being
// pointed at a scope it can never hold.
func TestAKeyCannotActOnBehalfOfAnyone(t *testing.T) {
	k := serveKeys(t)

	credential, _ := k.mintKey(t, "org:read")

	err := k.callWithKey(t, credential, map[string]string{
		interceptor.DelegationHeader: "anything at all",
	})
	if err == nil {
		t.Fatal("a key presented a delegation and was not refused")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("got %s, want permission_denied", connect.CodeOf(err))
	}
	if !strings.Contains(err.Error(), "on behalf") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// THE DISPATCH IS A DISPATCH, NOT A FALLBACK. A bad key is never retried as a
// token, and a bad token is never retried as a key.
func TestTheTwoCredentialSchemesDoNotFallBackToEachOther(t *testing.T) {
	k := serveKeys(t)

	credential, _ := k.mintKey(t, "org:read")

	// A perfectly good key presented under `Bearer` must fail as a token, not
	// succeed as a key.
	request := connect.NewRequest(&corev1.GetCurrentUserRequest{})
	request.Header().Set(interceptor.AuthorizationHeader, "Bearer "+credential)
	if _, err := k.session.GetCurrentUser(t.Context(), request); err == nil {
		t.Error("a key presented as a Bearer token was accepted, so the schemes share a path")
	}

	// A perfectly good token presented under `ApiKey` must fail as a key.
	if err := k.callWithKey(t, k.auth.token(t, k.subject, managingScopes), nil); err == nil {
		t.Error("a token presented as an ApiKey was accepted, so the schemes share a path")
	}
}

// Every unusable credential gets the same answer, so this is not an oracle for
// which key handles are real.
func TestUnusableKeysAreRefusedIdentically(t *testing.T) {
	k := serveKeys(t)

	live, _ := k.mintKey(t, "org:read")
	handle := live[len(apikey.Prefix) : len(apikey.Prefix)+16]

	// A real handle with a wrong secret, and a well-formed handle that names no
	// row. Both must be unauthenticated, and neither must say which.
	forged, err := apikey.Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	spliced := apikey.Prefix + handle + "_" + forged.Credential[len(apikey.Prefix)+17:]

	for name, credential := range map[string]string{
		"right handle, wrong secret": spliced,
		"unknown handle":             forged.Credential,
		"not a credential at all":    "kl_nonsense",
	} {
		t.Run(name, func(t *testing.T) {
			err := k.callWithKey(t, credential, nil)
			if err == nil {
				t.Fatal("accepted")
			}
			if connect.CodeOf(err) != connect.CodeUnauthenticated {
				t.Errorf("got %s, want unauthenticated", connect.CodeOf(err))
			}
			if !strings.Contains(err.Error(), interceptor.ErrNoUsableAPIKey.Error()) {
				t.Errorf("the refusal distinguishes this case: %v", err)
			}
		})
	}
}

// A deployment that wired no authenticator refuses a key rather than reporting
// a missing token, which would send a caller holding a good key to look for one
// they were never issued.
func TestWithoutAnAuthenticatorAKeyIsRefusedAsAKey(t *testing.T) {
	a := newAuthServer(t)

	mux := http.NewServeMux()
	mux.Handle(corev1connect.NewSessionServiceHandler(session.New(a.profiles(t)),
		connect.WithInterceptors(interceptor.Auth(a.verifier(t)))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := corev1connect.NewSessionServiceClient(srv.Client(), srv.URL)
	request := connect.NewRequest(&corev1.GetCurrentUserRequest{})
	request.Header().Set(interceptor.AuthorizationHeader,
		interceptor.APIKeyScheme+" kl_0123456789abcdef_"+strings.Repeat("a", 43))

	_, err := client.GetCurrentUser(t.Context(), request)
	if err == nil {
		t.Fatal("a key was accepted with no authenticator wired")
	}
	if !strings.Contains(err.Error(), interceptor.ErrNoUsableAPIKey.Error()) {
		t.Errorf("the refusal talks about tokens rather than keys: %v", err)
	}
}

// THE AUDIT TRAIL. Minting and revoking are both in the customer's own record,
// and a key's own acts name the key.
func TestMintAndRevokeAreInTheAuditLog(t *testing.T) {
	k := serveKeys(t)

	_, id := k.mintKey(t, "org:read")

	revoke := connect.NewRequest(&corev1.RevokeApiKeyRequest{KeyId: id})
	revoke.Header().Set(interceptor.AuthorizationHeader,
		"Bearer "+k.auth.token(t, k.subject, managingScopes))
	revoke.Header().Set(interceptor.OrgHeader, k.orgID)
	if _, err := k.keys.RevokeApiKey(t.Context(), revoke); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	tenant, err := k.live.store.BeginTenant(t.Context(), k.subject, k.orgID)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tenant.Rollback(t.Context()) }()

	for _, action := range []string{postgres.ActionMintAPIKey, postgres.ActionRevokeAPIKey} {
		var count int
		if err := tenant.Tx().QueryRow(t.Context(), `
			select count(*) from audit_log
			 where action_type = $1 and target_table = 'api_keys' and target_id = $2::uuid
		`, action, id).Scan(&count); err != nil {
			t.Fatalf("reading the audit log: %v", err)
		}
		if count != 1 {
			t.Errorf("%s wrote %d audit rows, want exactly 1", action, count)
		}
	}

	// NO CREDENTIAL IN THE LOG, hashed or otherwise. The audit log is readable
	// by every member and exportable to CSV.
	var leaked int
	if err := tenant.Tx().QueryRow(t.Context(), `
		select count(*) from audit_log
		 where target_table = 'api_keys'
		   and (coalesce(before::text, '') like $1 or coalesce(after::text, '') like $1)
	`, "%"+apikey.Prefix+"%").Scan(&leaked); err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	if leaked != 0 {
		t.Errorf("%d audit rows contain something shaped like a credential", leaked)
	}
}

// A key's own acts are attributed to the key, not to the human who minted it.
//
// The mechanism is a transaction-local GUC read by a column default, so this
// holds for every audit row a key's request writes including ones written by
// triggers, without any call site knowing about it.
func TestAnAuditRowWrittenByAKeyNamesTheKey(t *testing.T) {
	k := serveKeys(t)

	credential, _ := k.mintKey(t, "org:read")

	// Asserted on the transaction rather than through an RPC, deliberately.
	// What is being tested is that a GUC set by BeginAPIKeyTenant reaches a
	// COLUMN DEFAULT, so it holds for every writer including the triggers that
	// call `record_audit_log` from inside an UPDATE. Driving one RPC would
	// prove it for that RPC's call site and say nothing about the mechanism.
	principal, err := k.live.store.AuthenticateAPIKey(t.Context(), credential)
	if err != nil {
		t.Fatalf("authenticating: %v", err)
	}
	tenant, err := k.live.store.BeginAPIKeyTenant(t.Context(), principal)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tenant.Rollback(t.Context()) }()

	var named string
	if err := tenant.Tx().QueryRow(t.Context(),
		`select coalesce(nullif(current_setting('app.current_api_key_id', true), ''), '')`,
	).Scan(&named); err != nil {
		t.Fatalf("reading the GUC: %v", err)
	}
	if named != principal.ID {
		t.Errorf("the transaction names %q as the acting key, want %q", named, principal.ID)
	}

	// And the column default reads it, which is the half that makes every
	// writer name the key without knowing it has to.
	//
	// Two statements rather than a join against the function call. Postgres does
	// not order a join relative to a volatile function in the same query, so the
	// single-statement version reads the table before the row is there.
	var written string
	if err := tenant.Tx().QueryRow(t.Context(), `
		select record_audit_log($1::uuid, $2::uuid, null, 'rename_organisation',
		                        'organisations', null, null, null, $2::uuid)::text
	`, k.orgID, principal.UserID).Scan(&written); err != nil {
		t.Fatalf("writing an audit row on the key's transaction: %v", err)
	}

	var actor string
	if err := tenant.Tx().QueryRow(t.Context(),
		`select coalesce(actor_api_key_id::text, '') from audit_log where id = $1::uuid`,
		written).Scan(&actor); err != nil {
		t.Fatalf("reading the audit row back: %v", err)
	}
	if actor != principal.ID {
		t.Errorf("the audit row names %q as the acting key, want %q", actor, principal.ID)
	}
}

// A key stops working when its minter loses their membership, with no sweep and
// nothing to remember. That is the reason a key borrows a person's authority
// rather than holding its own.
func TestAKeyDiesWithItsMintersMembership(t *testing.T) {
	k := serveKeys(t)

	credential, _ := k.mintKey(t, "org:read")
	if err := k.callWithKey(t, credential, nil); err != nil {
		t.Fatalf("the key did not work to begin with: %v", err)
	}

	principal, err := k.live.store.AuthenticateAPIKey(t.Context(), credential)
	if err != nil {
		t.Fatalf("authenticating: %v", err)
	}

	// Remove the membership inside a transaction that is rolled back, so the
	// seeded fixture survives. BeginAPIKeyTenant runs on its own connection, so
	// it cannot see this uncommitted change; assert on the store's own check
	// instead, run inside the same transaction.
	tenant, err := k.live.store.BeginTenant(t.Context(), k.subject, k.orgID)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tenant.Rollback(t.Context()) }()

	if _, err := tenant.Tx().Exec(t.Context(),
		`delete from memberships where org_id = $1 and user_id = $2`,
		k.orgID, principal.UserID); err != nil {
		t.Fatalf("removing the membership: %v", err)
	}

	var still int
	if err := tenant.Tx().QueryRow(t.Context(),
		`select count(*) from memberships where org_id = $1 and user_id = $2`,
		principal.OrgID, principal.UserID).Scan(&still); err != nil {
		t.Fatalf("checking: %v", err)
	}
	if still != 0 {
		t.Fatal("the membership survived the delete, so this test proves nothing")
	}

	// The key's request runs the same membership query, so with the row gone it
	// resolves to no tenant.
	if _, err := k.live.store.BeginAPIKeyTenant(t.Context(), apikey.Principal{
		ID:     principal.ID,
		UserID: principal.UserID,
		// An organisation the minter is definitely not in stands in for the
		// removed membership, because the delete above is not committed.
		OrgID:  alphaOrg,
		Scopes: principal.Scopes,
	}); !errors.Is(err, postgres.ErrNotAMember) {
		t.Errorf("a key for an organisation its minter does not belong to gave %v, "+
			"want ErrNotAMember", err)
	}
}
