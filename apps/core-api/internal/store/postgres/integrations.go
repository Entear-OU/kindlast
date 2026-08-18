package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/integrations"
)

// Integrations, on the request's transaction (ENT-231, §26.4).
//
// # NO ORG PREDICATE ANYWHERE IN THIS FILE, AND HERE THAT MEANS RLS
//
// Every query below runs with both tenancy GUCs set and the policies in 00025
// supply `org_id = current_setting(...)` plus a membership check. A
// `where org_id = $1` added here would be a second, weaker copy of a check the
// database already makes, and the day the two disagree it is the weaker one
// somebody trusts. The same rule `memory.go` next door follows.
//
// # AND NO UPDATE THAT IS NOT A COLUMN THE GRANT NAMES
//
// `kindlast_app` holds `update (status, revoked_at, revoked_by)` and
// `update (credential_ciphertext, credential_key_id)` on `integrations`, and
// `update (granted, granted_at, granted_by)` on `integration_tools`. Anything
// else is a permission error rather than a policy refusal, which is the
// stronger of the two: a policy can be widened by a later migration that looks
// reasonable, where a column-level grant has to be widened on purpose.

// ErrNoConnection is returned when a connection id names nothing this caller
// can see.
//
// One error for "it does not exist" and "it is not yours", because RLS makes
// them the same read and telling them apart would be an existence oracle for
// connection ids across the whole deployment.
var ErrNoConnection = errors.New("no such connection")

// Connections returns every connection this organisation has, with its tools.
//
// Revoked connections included. A customer asking what Kindlast has been able
// to reach is asking about the past as well as the present, and a list that
// quietly forgot a revoked connection could not answer them.
func (t *Tenant) Connections(ctx context.Context) ([]integrations.Connection, error) {
	rows, err := t.tx.Query(ctx, `
		select i.id::text,
		       i.kind,
		       i.display_name,
		       i.endpoint_url,
		       i.status,
		       i.created_at,
		       i.revoked_at,
		       c.consented_at,
		       coalesce(c.consented_by::text, '')
		  from public.integrations i
		  left join lateral (
		         select consented_at, consented_by
		           from public.integration_consents
		          where integration_id = i.id
		          order by consented_at desc
		          limit 1
		       ) c on true
		 order by i.created_at desc`)
	if err != nil {
		return nil, fmt.Errorf("listing connections: %w", err)
	}

	connections, err := scanConnections(rows)
	if err != nil {
		return nil, err
	}

	// Tools in a second query rather than a join, because a join would repeat
	// every connection row once per tool and the assembly afterwards is the
	// same work with more chances to get it wrong. Two round trips inside one
	// transaction is not a cost worth optimising at this scale.
	tools, err := t.toolsByConnection(ctx)
	if err != nil {
		return nil, err
	}
	for i := range connections {
		connections[i].Tools = tools[connections[i].ID]
	}
	return connections, nil
}

// Connection returns one connection with its tools.
func (t *Tenant) Connection(ctx context.Context, id string) (integrations.Connection, error) {
	row := t.tx.QueryRow(ctx, `
		select i.id::text,
		       i.kind,
		       i.display_name,
		       i.endpoint_url,
		       i.status,
		       i.created_at,
		       i.revoked_at,
		       c.consented_at,
		       coalesce(c.consented_by::text, '')
		  from public.integrations i
		  left join lateral (
		         select consented_at, consented_by
		           from public.integration_consents
		          where integration_id = i.id
		          order by consented_at desc
		          limit 1
		       ) c on true
		 where i.id = $1`, id)

	connection, err := scanConnection(row)
	if err != nil {
		return integrations.Connection{}, err
	}

	tools, err := t.toolsOf(ctx, id)
	if err != nil {
		return integrations.Connection{}, err
	}
	connection.Tools = tools
	return connection, nil
}

// SealedCredential returns the connection's endpoint and its sealed credential.
//
// # A SEPARATE QUERY FROM EVERY OTHER READ, DELIBERATELY
//
// Nothing that renders a connection carries a credential, because this is the
// only function that selects the column. A `Connection` with a credential
// field would be a credential travelling attached to the thing a console
// renders, one careless log line away from being written down.
//
// It refuses a revoked connection here rather than leaving that to a caller,
// because this is the last read before a fetch and "revoking stops future
// fetches" should not rest on every caller remembering to check.
func (t *Tenant) SealedCredential(ctx context.Context, id string) (endpoint string, sealed []byte, keyID string, err error) {
	var status string
	row := t.tx.QueryRow(ctx, `
		select endpoint_url, status, credential_ciphertext, coalesce(credential_key_id, '')
		  from public.integrations
		 where id = $1`, id)

	if err := row.Scan(&endpoint, &status, &sealed, &keyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, "", ErrNoConnection
		}
		return "", nil, "", fmt.Errorf("reading the connection's credential: %w", err)
	}
	if status != integrations.StatusActive {
		return "", nil, "", integrations.ErrRevoked
	}
	return endpoint, sealed, keyID, nil
}

// CreateConnection records a connection, its tools and the consent, together.
//
// One transaction, which is the request's. A connection without a consent row
// would be a connection nobody agreed to, and a consent row without tools
// would record an agreement to nothing; both are states no reader could make
// sense of, so neither is reachable.
func (t *Tenant) CreateConnection(
	ctx context.Context,
	id, kind, displayName, endpoint string,
	sealed []byte,
	keyID string,
	tools []integrations.Tool,
	createdBy string,
) (integrations.Connection, error) {
	// The id is the caller's, because the credential was sealed against it
	// before this was called. See the service for why that binding needs the
	// id first.
	_, err := t.tx.Exec(ctx, `
		insert into public.integrations
		       (id, org_id, kind, display_name, endpoint_url,
		        credential_ciphertext, credential_key_id, created_by)
		values ($1::uuid, $2, $3, $4, $5, $6, nullif($7, ''), nullif($8, '')::uuid)`,
		id, t.orgID, kind, displayName, endpoint, sealed, keyID, createdBy)
	if err != nil {
		return integrations.Connection{}, fmt.Errorf("recording the connection: %w", err)
	}

	if err := t.insertTools(ctx, id, tools, createdBy); err != nil {
		return integrations.Connection{}, err
	}
	if err := t.recordConsent(ctx, id, endpoint, tools, createdBy); err != nil {
		return integrations.Connection{}, err
	}
	return t.Connection(ctx, id)
}

// SetGrants replaces which tools may be called, and records a new consent.
//
// # A NEW CONSENT ROW RATHER THAN AN EDIT TO THE OLD ONE
//
// The consent table holds no update grant for anybody, so this is enforced
// rather than intended. Widening what a product may do inside somebody's
// systems is exactly the change somebody will later need to reconstruct, and a
// table where the current state overwrote the previous one could not show it.
func (t *Tenant) SetGrants(
	ctx context.Context, id string, granted []string, by string,
) (integrations.Connection, error) {
	connection, err := t.Connection(ctx, id)
	if err != nil {
		return integrations.Connection{}, err
	}
	if connection.Status != integrations.StatusActive {
		return integrations.Connection{}, integrations.ErrRevoked
	}

	resolved, err := integrations.ResolveGrants(connection.Tools, granted)
	if err != nil {
		return integrations.Connection{}, err
	}

	for _, tool := range resolved {
		// `granted_at` and `granted_by` cleared when a grant is withdrawn, so
		// the pair always describes the CURRENT grant rather than the last one
		// there ever was. The history of grants lives in the consent rows,
		// which is where somebody reconstructing a change will look.
		_, err := t.tx.Exec(ctx, `
			update public.integration_tools
			   set granted = $3,
			       granted_at = case when $3 then now() else null end,
			       granted_by = case when $3 then nullif($4, '')::uuid else null end
			 where integration_id = $1 and name = $2`,
			id, tool.Name, tool.Granted, by)
		if err != nil {
			return integrations.Connection{}, fmt.Errorf("recording the grant for %q: %w", tool.Name, err)
		}
	}

	if err := t.recordConsent(ctx, id, connection.EndpointURL, resolved, by); err != nil {
		return integrations.Connection{}, err
	}
	return t.Connection(ctx, id)
}

// RevokeConnection stops future fetches, permanently.
//
// Idempotent: revoking an already-revoked connection changes nothing and
// returns it. A second click on a button that has already worked should not be
// an error, and it should certainly not move `revoked_at`, which would say the
// connection was live until the second click.
func (t *Tenant) RevokeConnection(ctx context.Context, id, by string) (integrations.Connection, error) {
	_, err := t.tx.Exec(ctx, `
		update public.integrations
		   set status = 'revoked',
		       revoked_at = now(),
		       revoked_by = nullif($2, '')::uuid
		 where id = $1 and status = 'active'`, id, by)
	if err != nil {
		return integrations.Connection{}, fmt.Errorf("revoking the connection: %w", err)
	}
	return t.Connection(ctx, id)
}

// RecordObservation stores what a fetch returned, and the fetch itself.
//
// Both, in one call, because a fetch record pointing at an evidence row that
// was never written is worse than either alone: the "what we fetched" view
// would offer a link to nothing.
func (t *Tenant) RecordObservation(
	ctx context.Context,
	connectionID, tool, argumentsJSON, contentJSON string,
	redactions int32,
	observedAt, requestedAt time.Time,
	requestedBy string,
) (integrations.Fetch, error) {
	var evidenceID string
	err := t.tx.QueryRow(ctx, `
		insert into public.org_evidence
		       (org_id, source, connection_id, observed_at, kind, body,
		        content_hash)
		values ($1, 'integration', $2, $3, $4, $5::jsonb,
		        encode(sha256($5::bytea), 'hex'))
		returning id::text`,
		t.orgID, connectionID, observedAt, "integration."+tool, contentJSON).Scan(&evidenceID)
	if err != nil {
		return integrations.Fetch{}, fmt.Errorf("recording the observation: %w", err)
	}

	return t.recordFetch(ctx, connectionID, tool, argumentsJSON,
		integrations.OutcomeSucceeded, "", evidenceID, redactions, requestedAt, requestedBy)
}

// RecordRefusal stores a fetch that policy or the endpoint stopped.
//
// THE ROWS THAT MAKE THE GATEWAY LEGIBLE. A log holding only successes would
// be indistinguishable from a deployment where the policy does nothing, so a
// refusal is recorded with the same care as a result.
func (t *Tenant) RecordRefusal(
	ctx context.Context,
	connectionID, tool, argumentsJSON, outcome, detail string,
	requestedAt time.Time,
	requestedBy string,
) (integrations.Fetch, error) {
	return t.recordFetch(ctx, connectionID, tool, argumentsJSON,
		outcome, detail, "", 0, requestedAt, requestedBy)
}

// Fetches returns the "what we fetched" log, newest first.
//
// Keyset paging on `requested_at`, matching how every other unbounded listing
// in this schema pages. An offset would drift as new fetches arrive, which for
// a log that grows while somebody reads it means rows appearing twice.
func (t *Tenant) Fetches(
	ctx context.Context, connectionID string, pageSize int32, before time.Time,
) ([]integrations.Fetch, error) {
	size := effectiveFetchPageSize(pageSize)

	rows, err := t.tx.Query(ctx, `
		select f.id::text,
		       f.integration_id::text,
		       i.display_name,
		       f.tool,
		       f.outcome,
		       coalesce(f.detail, ''),
		       f.requested_at,
		       f.finished_at,
		       coalesce(f.evidence_id::text, ''),
		       f.redactions,
		       coalesce(f.requested_by::text, '')
		  from public.integration_fetches f
		  join public.integrations i on i.id = f.integration_id
		 where ($1 = '' or f.integration_id = nullif($1, '')::uuid)
		   and ($2::timestamptz is null or f.requested_at < $2)
		 order by f.requested_at desc
		 limit $3`, connectionID, nullTime(before), size)
	if err != nil {
		return nil, fmt.Errorf("listing fetches: %w", err)
	}
	defer rows.Close()

	fetches := make([]integrations.Fetch, 0, size)
	for rows.Next() {
		var f integrations.Fetch
		if err := rows.Scan(
			&f.ID, &f.IntegrationID, &f.IntegrationName, &f.Tool,
			&f.Outcome, &f.Detail, &f.RequestedAt, &f.FinishedAt,
			&f.EvidenceID, &f.Redactions, &f.RequestedBy,
		); err != nil {
			return nil, fmt.Errorf("scanning a fetch: %w", err)
		}
		fetches = append(fetches, f)
	}
	return fetches, rows.Err()
}

// EffectiveFetchPageSize is the clamp, exported so a handler can ask whether a
// page was full using the size that was actually applied rather than the size
// that was requested.
func EffectiveFetchPageSize(requested int32) int32 { return effectiveFetchPageSize(requested) }

func effectiveFetchPageSize(requested int32) int32 {
	if requested <= 0 || requested > 200 {
		return 50
	}
	return requested
}

func (t *Tenant) recordFetch(
	ctx context.Context,
	connectionID, tool, argumentsJSON, outcome, detail, evidenceID string,
	redactions int32,
	requestedAt time.Time,
	requestedBy string,
) (integrations.Fetch, error) {
	if argumentsJSON == "" {
		argumentsJSON = "{}"
	}

	var id string
	err := t.tx.QueryRow(ctx, `
		insert into public.integration_fetches
		       (org_id, integration_id, tool, arguments_json, requested_at,
		        finished_at, outcome, detail, evidence_id, redactions, requested_by)
		values ($1, $2, $3, $4::jsonb, $5, now(), $6, nullif($7, ''),
		        nullif($8, '')::uuid, $9, nullif($10, '')::uuid)
		returning id::text`,
		t.orgID, connectionID, tool, argumentsJSON, requestedAt,
		outcome, detail, evidenceID, redactions, requestedBy).Scan(&id)
	if err != nil {
		return integrations.Fetch{}, fmt.Errorf("recording the fetch: %w", err)
	}

	fetches, err := t.Fetches(ctx, connectionID, 1, time.Time{})
	if err != nil || len(fetches) == 0 {
		// The row is written either way; only the echo back to the caller
		// failed. Returning what is known rather than an error, because
		// failing here would report a fetch that did happen as one that did
		// not.
		return integrations.Fetch{
			ID: id, IntegrationID: connectionID, Tool: tool,
			Outcome: outcome, Detail: detail, EvidenceID: evidenceID,
			Redactions: redactions, RequestedAt: requestedAt,
		}, nil
	}
	return fetches[0], nil
}

func (t *Tenant) insertTools(ctx context.Context, connectionID string, tools []integrations.Tool, by string) error {
	for _, tool := range tools {
		_, err := t.tx.Exec(ctx, `
			insert into public.integration_tools
			       (org_id, integration_id, name, description, write_capable,
			        granted, granted_at, granted_by)
			values ($1, $2, $3, $4, $5, $6,
			        case when $6 then now() else null end,
			        case when $6 then nullif($7, '')::uuid else null end)`,
			t.orgID, connectionID, tool.Name, tool.Description,
			tool.WriteCapable, tool.Granted, by)
		if err != nil {
			return fmt.Errorf("recording the tool %q: %w", tool.Name, err)
		}
	}
	return nil
}

func (t *Tenant) recordConsent(
	ctx context.Context, connectionID, endpoint string, tools []integrations.Tool, by string,
) error {
	offered, err := toolsJSON(tools)
	if err != nil {
		return err
	}

	_, err = t.tx.Exec(ctx, `
		insert into public.integration_consents
		       (org_id, integration_id, consented_by, endpoint_url,
		        offered_tools, granted_tools)
		values ($1, $2, $3::uuid, $4, $5::jsonb, $6)`,
		t.orgID, connectionID, by, endpoint, offered, integrations.GrantedNames(tools))
	if err != nil {
		return fmt.Errorf("recording the consent: %w", err)
	}
	return nil
}

func (t *Tenant) toolsOf(ctx context.Context, connectionID string) ([]integrations.Tool, error) {
	rows, err := t.tx.Query(ctx, `
		select name, description, write_capable, granted
		  from public.integration_tools
		 where integration_id = $1
		 order by name`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("listing tools: %w", err)
	}
	return scanTools(rows)
}

func (t *Tenant) toolsByConnection(ctx context.Context) (map[string][]integrations.Tool, error) {
	rows, err := t.tx.Query(ctx, `
		select integration_id::text, name, description, write_capable, granted
		  from public.integration_tools
		 order by integration_id, name`)
	if err != nil {
		return nil, fmt.Errorf("listing tools: %w", err)
	}
	defer rows.Close()

	byConnection := map[string][]integrations.Tool{}
	for rows.Next() {
		var connectionID string
		var tool integrations.Tool
		if err := rows.Scan(&connectionID, &tool.Name, &tool.Description,
			&tool.WriteCapable, &tool.Granted); err != nil {
			return nil, fmt.Errorf("scanning a tool: %w", err)
		}
		byConnection[connectionID] = append(byConnection[connectionID], tool)
	}
	return byConnection, rows.Err()
}

func scanTools(rows pgx.Rows) ([]integrations.Tool, error) {
	defer rows.Close()

	tools := make([]integrations.Tool, 0, 16)
	for rows.Next() {
		var tool integrations.Tool
		if err := rows.Scan(&tool.Name, &tool.Description, &tool.WriteCapable, &tool.Granted); err != nil {
			return nil, fmt.Errorf("scanning a tool: %w", err)
		}
		tools = append(tools, tool)
	}
	return tools, rows.Err()
}

func scanConnections(rows pgx.Rows) ([]integrations.Connection, error) {
	defer rows.Close()

	connections := make([]integrations.Connection, 0, 8)
	for rows.Next() {
		connection, err := readConnection(rows)
		if err != nil {
			return nil, err
		}
		connections = append(connections, connection)
	}
	return connections, rows.Err()
}

func scanConnection(row pgx.Row) (integrations.Connection, error) {
	connection, err := readConnection(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return integrations.Connection{}, ErrNoConnection
	}
	return connection, err
}

// scanner is what both a single row and a rows cursor satisfy, so the column
// list is written once. Two copies of an eleven-column Scan is two places for
// a reordering to go wrong silently.
type scanner interface {
	Scan(dest ...any) error
}

func readConnection(from scanner) (integrations.Connection, error) {
	var c integrations.Connection
	err := from.Scan(
		&c.ID, &c.Kind, &c.DisplayName, &c.EndpointURL, &c.Status,
		&c.CreatedAt, &c.RevokedAt, &c.ConsentedAt, &c.ConsentedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return integrations.Connection{}, err
		}
		return integrations.Connection{}, fmt.Errorf("scanning a connection: %w", err)
	}
	return c, nil
}

// nullTime turns the zero time into a SQL null, so a first page and a
// subsequent page use the same statement.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// toolsJSON encodes the offered tools for the consent snapshot.
//
// A hand-built shape rather than the domain struct's own JSON tags, because
// this is stored data whose field names outlive any Go type: renaming a field
// in `integrations.Tool` must not silently change what a consent row from last
// year appears to say.
func toolsJSON(tools []integrations.Tool) (string, error) {
	type stored struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		WriteCapable bool   `json:"write_capable"`
		Granted      bool   `json:"granted"`
	}

	out := make([]stored, 0, len(tools))
	for _, tool := range tools {
		out = append(out, stored{
			Name:         tool.Name,
			Description:  tool.Description,
			WriteCapable: tool.WriteCapable,
			Granted:      tool.Granted,
		})
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("encoding the consented tools: %w", err)
	}
	return string(encoded), nil
}
