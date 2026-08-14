package oidc_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// Why this endpoint is reached at all.
//
// An access token is not required to carry `name` or `email`, and the bundled
// Zitadel carries neither: measured on the running stack, its access tokens
// hold `sub`, `aud`, `exp` and its own roles claim, and nothing describing the
// human. UserInfo (OIDC Core §5.3) is the mechanism specified for exactly that
// gap, and it works against any conformant provider rather than only this one.

func TestDiscoveryExposesTheUserInfoEndpoint(t *testing.T) {
	server := newAuthServer(t)

	provider, err := oidc.Discover(context.Background(), nil, server.issuer())
	if err != nil {
		t.Fatalf("discovering: %v", err)
	}

	if provider.UserInfoURI != server.userInfoURI() {
		t.Fatalf("UserInfoURI = %q, want %q", provider.UserInfoURI, server.userInfoURI())
	}
}

// A document that declares no userinfo endpoint is not an error. Discovery
// still has to succeed, because verification is what this package exists for
// and it needs only the JWKS.
func TestDiscoveryToleratesNoUserInfoEndpoint(t *testing.T) {
	server := newBareAuthServer(t)

	provider, err := oidc.Discover(context.Background(), nil, server.issuer())
	if err != nil {
		t.Fatalf("discovery must not fail when userinfo is absent: %v", err)
	}
	if provider.UserInfoURI != "" {
		t.Fatalf("UserInfoURI = %q, want empty", provider.UserInfoURI)
	}
}

func TestUserInfoReturnsTheProfileTheTokenOmits(t *testing.T) {
	server := newAuthServer(t)
	server.serveUserInfo(map[string]any{
		"sub":            "user-subject-1",
		"name":           "Ada Lovelace",
		"email":          "ada@example.com",
		"email_verified": true,
	})

	profile, err := oidc.FetchUserInfo(
		context.Background(), nil, server.userInfoURI(), "an-access-token", "user-subject-1")
	if err != nil {
		t.Fatalf("fetching userinfo: %v", err)
	}

	if profile.Name != "Ada Lovelace" {
		t.Errorf("Name = %q, want %q", profile.Name, "Ada Lovelace")
	}
	if profile.Email != "ada@example.com" {
		t.Errorf("Email = %q, want %q", profile.Email, "ada@example.com")
	}
	if !profile.EmailVerified {
		t.Error("EmailVerified = false, want true")
	}
}

// The call is made as the user, with their access token, not with a service
// credential. Anything else would be this service asking the authorization
// server about someone it has not been authorised to ask about.
func TestUserInfoPresentsTheCallersOwnToken(t *testing.T) {
	server := newAuthServer(t)
	server.serveUserInfo(map[string]any{"sub": "user-subject-1"})

	if _, err := oidc.FetchUserInfo(
		context.Background(), nil, server.userInfoURI(), "an-access-token", "user-subject-1"); err != nil {
		t.Fatalf("fetching userinfo: %v", err)
	}

	if got := server.userInfoAuthorization(); got != "Bearer an-access-token" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer an-access-token")
	}
}

// OIDC Core §5.3.2 requires this comparison, and the reason is not pedantry.
// Without it, any response this service can be pointed at renames the caller's
// organisation after whoever the document describes, and a provider or proxy
// that returns the wrong body silently attaches one person's name to another
// person's tenant.
func TestUserInfoRefusesAProfileForADifferentSubject(t *testing.T) {
	server := newAuthServer(t)
	server.serveUserInfo(map[string]any{
		"sub":   "somebody-else",
		"name":  "Grace Hopper",
		"email": "grace@example.com",
	})

	_, err := oidc.FetchUserInfo(
		context.Background(), nil, server.userInfoURI(), "an-access-token", "user-subject-1")
	if !errors.Is(err, oidc.ErrSubjectMismatch) {
		t.Fatalf("err = %v, want ErrSubjectMismatch", err)
	}
}

// A document with no `sub` at all fails the same comparison. An omitted claim
// is not a match.
func TestUserInfoRefusesAProfileWithNoSubject(t *testing.T) {
	server := newAuthServer(t)
	server.serveUserInfo(map[string]any{"name": "Anonymous"})

	_, err := oidc.FetchUserInfo(
		context.Background(), nil, server.userInfoURI(), "an-access-token", "user-subject-1")
	if !errors.Is(err, oidc.ErrSubjectMismatch) {
		t.Fatalf("err = %v, want ErrSubjectMismatch", err)
	}
}

func TestUserInfoReportsANonOKStatus(t *testing.T) {
	server := newAuthServer(t)
	server.failUserInfo(http.StatusForbidden)

	_, err := oidc.FetchUserInfo(
		context.Background(), nil, server.userInfoURI(), "an-access-token", "user-subject-1")
	if err == nil {
		t.Fatal("a 403 from userinfo must be an error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v, want it to name the status", err)
	}
}

// Calling with no endpoint is a caller error worth naming, because the
// discovery document is allowed to omit it and the caller has to decide what
// to do about that rather than send a request to the empty string.
func TestUserInfoRefusesAnEmptyEndpoint(t *testing.T) {
	if _, err := oidc.FetchUserInfo(
		context.Background(), nil, "", "an-access-token", "user-subject-1"); err == nil {
		t.Fatal("an empty endpoint must be an error")
	}
}
