// Package identity resolves the human-readable half of a caller's identity,
// which an access token is under no obligation to carry.
//
// It exists as its own package for the §21.6 reason: the session handler is
// meant to validate, decide and store, and giving it an HTTP client would make
// "talks to the authorization server" a property of the service layer. Here it
// is one adapter with one job, and the handler depends on an interface it
// declares itself.
package identity

import (
	"context"

	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// UserInfo fetches profile claims from an OIDC provider's userinfo endpoint.
//
// The endpoint is resolved once at start-up from the discovery document, the
// same way the JWKS is, so nothing about the provider is hard-coded and a
// self-hoster pointing at their own IdP needs no code change (§18.2).
type UserInfo struct {
	endpoint  string
	transport *oidc.Transport
}

// NewUserInfo binds an adapter to a discovered endpoint.
//
// An empty endpoint is allowed and is not an error: the discovery document may
// declare none, and a provider without userinfo must still be usable. Profile
// then reports that there is nothing to ask, and the caller degrades.
func NewUserInfo(endpoint string, transport *oidc.Transport) *UserInfo {
	return &UserInfo{endpoint: endpoint, transport: transport}
}

// Profile asks the authorization server who this token belongs to.
//
// The subject is passed so the response can be checked against it, which OIDC
// Core §5.3.2 requires; oidc.FetchUserInfo does the comparison and refuses a
// document describing anyone else.
func (u *UserInfo) Profile(ctx context.Context, accessToken, subject string) (*oidc.Profile, error) {
	return oidc.FetchUserInfo(ctx, u.transport, u.endpoint, accessToken, subject)
}
