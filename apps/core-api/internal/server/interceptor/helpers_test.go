package interceptor_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// insertInvitation writes one as the migrator, standing in for the owner-only
// invite endpoint that is build-order step 2.
func insertInvitation(t *testing.T, orgID, email, role, token string) {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), migratorDSNForChain())
	if err != nil {
		t.Fatalf("connecting as the migrator: %v", err)
	}
	defer pool.Close()

	_, err = pool.Exec(context.Background(), `
		insert into invitations (org_id, email, role, token_hash, expires_at)
		values ($1, $2, $3, $4, now() + interval '1 hour')
	`, orgID, email, role, postgres.HashInvitationToken(token))
	if err != nil {
		t.Fatalf("creating the invitation: %v", err)
	}
}

func removeInvitation(t *testing.T, token string) {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), migratorDSNForChain())
	if err != nil {
		t.Fatalf("connecting as the migrator: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(context.Background(),
		`delete from invitations where token_hash = $1`, postgres.HashInvitationToken(token)); err != nil {
		t.Fatalf("deleting the invitation: %v", err)
	}
}
