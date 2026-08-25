package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/integrations"
)

// The two reads a scheduled fetch makes, on two pools (ENT-279).
//
// # WHY THIS IS SPLIT ACROSS ROLES AT ALL
//
// Listing what is due is a question about every organisation at once, which
// only the producer role may ask. Reading the endpoint and the sealed
// credential is a question the producer role deliberately cannot ask:
// `kindlast_agent` holds a column-limited select on `integrations` that omits
// `credential_ciphertext` (00025), and widening it is a security decision
// nobody has taken. So the listing runs on the agent pool and the plan runs on
// the application pool, acting as the person who consented to the connection.
//
// # AND WHY THE PLAN IS ITS OWN TRANSACTION, ENDING BEFORE THE DIAL
//
// The manual path holds the request's tenant transaction open across the
// gateway call, which is fine for one person clicking Fetch and wrong for a
// schedule: a customer's slow endpoint would hold a pool connection for its
// whole timeout, once per due tool, for as long as it takes. So the plan is
// read and committed, the dial happens with no transaction open, and the
// outcome is written afterwards through IngestEvidence on the agent pool.
//
// The cost of that split is a crash between the two losing the record of an
// attempt that reached a customer. What it buys is that a hundred stale tools
// on a hundred unreachable endpoints cannot exhaust the pool the whole product
// runs on, which is the failure that takes everything down rather than one
// fetch.

// FetchTarget is one connection and one tool that is due a scheduled fetch.
type FetchTarget struct {
	OrgID         string
	IntegrationID string
	Tool          string
}

// FetchPlan is everything one fetch needs, read under the consenting person's
// authority.
//
// Sealed rather than opened: the keyring lives in the service layer and this
// store has never held a plaintext credential.
type FetchPlan struct {
	OrgID       string
	EndpointURL string
	Sealed      []byte
	KeyID       string
	// Tool as the connection stores it, so the caller can see `write_capable`
	// and `granted` rather than being told the answer.
	Tool integrations.Tool
	// The whole connection's policy, sent to the gateway per call so its
	// decision is made against what the customer granted rather than against
	// what this process believes.
	Granted      []string
	WriteGranted []string
}

// FetchTargets lists what is due, across every organisation.
//
// `fetch_targets` is a SECURITY DEFINER function because the producer role
// cannot enumerate tenants; 00048 carries the argument. The staleness interval
// arrives from the caller and is a server constant rather than a request
// field, because a caller that could send zero would dial every customer's
// systems at once.
func (a *AgentStore) FetchTargets(
	ctx context.Context, staleAfter time.Duration, limit int,
) ([]FetchTarget, error) {
	rows, err := a.pool.Query(ctx,
		`select org_id::text, integration_id::text, tool
		   from public.fetch_targets($1, $2)`,
		staleAfter, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing fetch targets: %w", err)
	}
	defer rows.Close()

	var targets []FetchTarget
	for rows.Next() {
		var target FetchTarget
		if err := rows.Scan(&target.OrgID, &target.IntegrationID, &target.Tool); err != nil {
			return nil, fmt.Errorf("postgres: reading a fetch target: %w", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: listing fetch targets: %w", err)
	}
	return targets, nil
}

// FetchPlan reads what one scheduled fetch needs, as the consenting person.
//
// # THE FIRST READ HAPPENS BEFORE THE TENANCY, AND IT HAS TO
//
// The same shape ExecuteJob has, for the same reason. Every policy on
// `integrations` tests both GUCs, and the GUCs are what this read produces:
// there is no order in which a tenant transaction reads the row that tells it
// which tenant to be. So it goes through `integration_fetch_context()`, which
// answers that one question about one row addressed by its primary key. Every
// read after it, including the connection itself, happens under the ordinary
// two-GUC policy.
//
// # WHAT COMES BACK WHEN THE ANSWER IS NO
//
//	ErrNoConnection             nothing by that id, or the person who consented
//	                            is no longer a member of the organisation, so
//	                            the policy hides it. The organisation is still
//	                            returned, because a refusal has to be recorded
//	                            against one.
//	integrations.ErrRevoked     the customer revoked the connection
//	integrations.ErrNotGranted  the customer did not grant that tool, or the
//	                            connection never offered it
func (s *Store) FetchPlan(ctx context.Context, integrationID, tool string) (FetchPlan, error) {
	id, err := uuid.Parse(integrationID)
	if err != nil {
		return FetchPlan{}, ErrNoConnection
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return FetchPlan{}, fmt.Errorf("postgres: planning a fetch: %w", err)
	}
	// Read only, so the rollback is the ordinary end of this transaction
	// rather than an error path.
	defer func() { _ = tx.Rollback(ctx) }()

	var orgID, consentedBy string
	err = tx.QueryRow(ctx, `
		select org_id::text, coalesce(consented_by::text, '')
		  from public.integration_fetch_context($1)
	`, id).Scan(&orgID, &consentedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return FetchPlan{}, ErrNoConnection
	}
	if err != nil {
		return FetchPlan{}, fmt.Errorf("postgres: reading a connection's context: %w", err)
	}
	if consentedBy == "" {
		// A connection whose consent names nobody cannot be fetched under
		// anybody's authority, and inventing one is the thing this whole path
		// is arranged to avoid. The organisation travels back so the refusal
		// is recorded rather than lost.
		return FetchPlan{OrgID: orgID}, integrations.ErrNotGranted
	}

	if err := setLocal(ctx, tx, "app.current_user_id", consentedBy); err != nil {
		return FetchPlan{OrgID: orgID}, err
	}
	if err := setLocal(ctx, tx, "app.current_org_id", orgID); err != nil {
		return FetchPlan{OrgID: orgID}, err
	}

	tenant := &Tenant{tx: tx, orgID: orgID, userID: consentedBy}

	connection, err := tenant.Connection(ctx, integrationID)
	if err != nil {
		// Includes the case worth naming: the consenting person has left the
		// organisation, the membership `exists` fails, and the connection is
		// invisible. A standing consent nobody stands behind any more stops
		// producing fetches, which is the answer a customer reading their own
		// audit log would want.
		return FetchPlan{OrgID: orgID}, err
	}

	stored, offered := integrations.Find(connection.Tools, tool)
	if !offered || !stored.Granted {
		return FetchPlan{OrgID: orgID}, integrations.ErrNotGranted
	}

	// LAST, AND ONLY ONCE EVERYTHING ELSE HAS SAID YES. `SealedCredential` is
	// the only read in this process that touches the credential column, and it
	// refuses a revoked connection itself.
	endpoint, sealed, keyID, err := tenant.SealedCredential(ctx, integrationID)
	if err != nil {
		return FetchPlan{OrgID: orgID}, err
	}

	return FetchPlan{
		OrgID:        orgID,
		EndpointURL:  endpoint,
		Sealed:       sealed,
		KeyID:        keyID,
		Tool:         stored,
		Granted:      integrations.GrantedNames(connection.Tools),
		WriteGranted: integrations.WriteGrants(connection.Tools),
	}, nil
}
