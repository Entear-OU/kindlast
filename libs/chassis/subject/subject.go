// Package subject maps an IdP subject claim onto the uuid this system stores
// it as.
//
// It exists because of a mismatch measured against the running stack rather
// than assumed. `memberships.user_id` is a `uuid` (ENT-192, migration
// 00002_organisations.sql). Zitadel subjects are snowflake integers rendered
// as strings, for example `386089961457188867`, which is not a uuid and never
// casts to one. Keycloak happens to issue uuids, Entra issues its own object
// ids, Auth0 issues `auth0|abc123`. So the schema's column type is a
// commitment the IdP does not make.
//
// Deriving a uuid is the settled answer (§20.1), rather than widening the
// column to text: widening rewrites the neighbour of the tenancy key on the
// table every RLS policy joins against, for nothing the derivation does not
// already provide.
//
// The derivation lives here rather than in one service because `core-api` and
// `intelligence` must agree about who a user is. Two copies would eventually
// disagree while both believed they agreed, which is the worst available
// version of that bug (§1.6).
//
// # Two consequences, both easy to miss
//
// **The issuer is set-once per deployed instance.** It is an input to every
// derived id, so changing it re-derives all of them and orphans every
// membership row: the rows remain, and nobody maps to them. Moving an IdP's
// public URL is an identity migration with a backfill, not a settings change.
// docs/core-api-configuration.md says so where an operator will read it.
//
// **The derivation is one-way.** A uuid cannot be turned back into a subject,
// so provisioning stores the raw `iss` and `sub` alongside the derived id.
// That is what answers "who is this uuid" during an incident, and what a
// subject access request needs in a product whose whole subject is GDPR.
package subject

import (
	"errors"

	"github.com/google/uuid"
)

// namespace derives from a URL rather than being an invented constant, so the
// value in this file can be recomputed by anyone rather than trusted because
// it is written down.
var namespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://kindlast.eu/ns/idp-subject"))

// UUID returns the uuid a subject is stored as.
//
// A subject that is already a uuid is used unchanged, which keeps fixtures and
// any uuid-issuing IdP mapping to themselves. Anything else derives a version
// 5 uuid from the issuer and the subject together.
//
// The issuer is part of the input deliberately. Without it, two deployments
// federating different IdPs that happened to issue the same subject string
// would collide onto one identity, and the collision would present as one
// user seeing another's organisations.
func UUID(issuer, subject string) (uuid.UUID, error) {
	if subject == "" {
		return uuid.Nil, errors.New("subject: empty subject claim")
	}
	if issuer == "" {
		return uuid.Nil, errors.New("subject: empty issuer")
	}

	if parsed, err := uuid.Parse(subject); err == nil {
		return parsed, nil
	}

	// A NUL separator, so an issuer ending in the subject's first characters
	// cannot produce the same input as a different issuer and subject pair.
	input := make([]byte, 0, len(issuer)+len(subject)+1)
	input = append(input, issuer...)
	input = append(input, 0)
	input = append(input, subject...)

	return uuid.NewSHA1(namespace, input), nil
}
