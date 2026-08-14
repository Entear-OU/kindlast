package interceptor_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
	"github.com/Entear-OU/kindlast/libs/chassis/subject"
)

// interceptorsFor assembles the production chain, in the production order, so
// every test that needs it gets the same one rather than its own arrangement.
func interceptorsFor(t *testing.T, a *authServer, live *stack, scopes *interceptor.Scope) []connect.Interceptor {
	t.Helper()

	return []connect.Interceptor{
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
		interceptor.Tenancy(tenantOpener{live.store}),
	}
}

// serveOrg starts OrgService behind the given interceptors.
func serveOrg(t *testing.T, interceptors ...connect.Interceptor) corev1connect.OrgServiceClient {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle(corev1connect.NewOrgServiceHandler(org.New(),
		connect.WithInterceptors(interceptors...)))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return corev1connect.NewOrgServiceClient(server.Client(), server.URL)
}

// requestWith builds a Connect request carrying headers.
func requestWith[T any](message *T, headers map[string]string) *connect.Request[T] {
	request := connect.NewRequest(message)
	for name, value := range headers {
		request.Header().Set(name, value)
	}
	return request
}

// mapClaims builds a token body that passes every stage, so a test varies only
// what it cares about.
//
// The scope is `openid` because that is what both shipped RPCs declare.
// Anything else and the request is refused at the scope stage, which would be
// a correct refusal and a confusing test failure.
func mapClaims(a *authServer, subjectClaim string, extra map[string]any) jwt.MapClaims {
	claims := jwt.MapClaims{
		"iss":            a.server.URL,
		"aud":            testAudience,
		"sub":            subjectClaim,
		"exp":            time.Now().Add(10 * time.Minute).Unix(),
		"iat":            time.Now().Unix(),
		"jti":            "jti-" + subjectClaim,
		"scope":          "openid profile",
		"email_verified": true,
	}
	for key, value := range extra {
		claims[key] = value
	}
	return claims
}

// chainMigratorPool opens a migrator pool behind the same skip-or-fail gate
// requireStack uses.
//
// Every test here reaches it only after requireStack, so today the gate is
// belt and braces. It exists because the equivalent helper in the store
// package did not have one, and a single test that called a fixture helper
// before the gate turned an absent database into a red build in the `go` CI
// job, where no stack runs at all.
// recordedEmailFor reads what user_identities holds for a subject, as the
// migrator, because that table is behind the same forced policies as
// everything else and this assertion wants the unfiltered truth.
func recordedEmailFor(t *testing.T, issuer, claim string) string {
	t.Helper()

	userID, err := subject.UUID(issuer, claim)
	if err != nil {
		t.Fatalf("deriving the user id: %v", err)
	}

	var email *string
	if err := chainMigratorPool(t).QueryRow(context.Background(),
		`select email from user_identities where user_id = $1`, userID).Scan(&email); err != nil {
		t.Fatalf("reading the recorded email: %v", err)
	}
	if email == nil {
		return ""
	}
	return *email
}

func chainMigratorPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), migratorDSNForChain())
	if err == nil {
		err = pool.Ping(context.Background())
	}
	if err != nil {
		if pool != nil {
			pool.Close()
		}
		unavailable(t, "migrator not reachable at %s (%v)", migratorDSNForChain(), err)
	}

	t.Cleanup(pool.Close)
	return pool
}

// insertInvitation writes one as the migrator, standing in for the owner-only
// invite endpoint that is build-order step 2.
func insertInvitation(t *testing.T, orgID, email, role, token string) {
	t.Helper()

	pool := chainMigratorPool(t)

	_, err := pool.Exec(context.Background(), `
		insert into invitations (org_id, email, role, token_hash, expires_at)
		values ($1, $2, $3, $4, now() + interval '1 hour')
	`, orgID, email, role, postgres.HashInvitationToken(token))
	if err != nil {
		t.Fatalf("creating the invitation: %v", err)
	}
}

func removeInvitation(t *testing.T, token string) {
	t.Helper()

	pool := chainMigratorPool(t)

	if _, err := pool.Exec(context.Background(),
		`delete from invitations where token_hash = $1`, postgres.HashInvitationToken(token)); err != nil {
		t.Fatalf("deleting the invitation: %v", err)
	}
}
