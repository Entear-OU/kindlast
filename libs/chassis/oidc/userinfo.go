package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrSubjectMismatch means a userinfo document described somebody other than
// the holder of the token used to fetch it.
var ErrSubjectMismatch = errors.New("oidc: userinfo describes a different subject")

// Profile is the human-readable half of an identity: the claims a resource
// server may want for a label, and must never use for authorization.
//
// Deliberately separate from Claims. Claims is what a signature proves, and it
// is the only thing a policy decision is allowed to read. This is what an
// endpoint said over a connection, which is a weaker thing, and keeping the
// two types apart is what stops the weaker one drifting into an access check.
type Profile struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// FetchUserInfo asks the authorization server who the bearer of this token is.
//
// This is a network call to the authorization server, so it belongs nowhere
// near a per-request path: the whole point of local verification (§1.4) is
// that a page render does not depend on `auth` being up. Call it when the
// answer is needed for a decision that happens once, such as naming something
// after the person on first arrival, and treat failure as "no profile" rather
// than as a failed request.
//
// expectedSubject is required and is compared against the document's `sub`.
// OIDC Core §5.3.2 mandates the comparison, and skipping it is not a small
// omission: this function is given an endpoint from a discovery document, and
// without the check any response that endpoint can be made to return is
// accepted as the caller's own identity.
func FetchUserInfo(
	ctx context.Context,
	transport *Transport,
	endpoint, accessToken, expectedSubject string,
) (*Profile, error) {
	if endpoint == "" {
		return nil, errors.New("oidc: no userinfo endpoint; the discovery document declared none")
	}
	if accessToken == "" {
		return nil, errors.New("oidc: userinfo needs the caller's access token")
	}
	if expectedSubject == "" {
		return nil, errors.New("oidc: userinfo needs the subject the token proved")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: building userinfo request: %w", err)
	}
	// The caller's own token, per OIDC Core §5.3.1. Not a service credential:
	// this service has no business asking the authorization server about a
	// person who has not just presented proof of being that person.
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	if transport != nil && transport.Host != "" {
		request.Host = transport.Host
	}

	response, err := transport.client().Do(request)
	if err != nil {
		return nil, fmt.Errorf("oidc: fetching userinfo: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: userinfo returned %s", response.Status)
	}

	// A signed response (`application/jwt`) is permitted by the specification
	// and is not handled here. Reading its payload without verifying it would
	// be worse than refusing, and no provider this runs against is configured
	// to return one.
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oidc: reading userinfo: %w", err)
	}

	var document struct {
		Subject       string       `json:"sub"`
		Email         string       `json:"email"`
		EmailVerified flexibleBool `json:"email_verified"`
		Name          string       `json:"name"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("oidc: parsing userinfo: %w", err)
	}

	if document.Subject != expectedSubject {
		// The subject is not quoted into the error. It is the identifier of a
		// real person and this string reaches a log.
		return nil, ErrSubjectMismatch
	}

	return &Profile{
		Subject:       document.Subject,
		Email:         document.Email,
		EmailVerified: bool(document.EmailVerified),
		Name:          document.Name,
	}, nil
}
