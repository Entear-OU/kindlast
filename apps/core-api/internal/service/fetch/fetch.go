// Package fetch serves FetchService: the scheduled half of "a scheduled fetch
// deposits, a sweep reads" (ENT-279; ENT-231, §26.4).
//
// # WHY THIS IS NOT A METHOD ON IntegrationsService
//
// That service is the console's, on the core surface, and every one of its
// handlers resolves a signed-in person out of the request. This one is on the
// platform surface, reachable only by `internal:ingest`, and its caller is a
// Temporal schedule with nobody behind it. The package boundary is what makes
// "can a browser cause this" a question answered by an import path rather than
// by reading a handler.
//
// # NOTHING HERE OPENS A CONNECTION TO A CUSTOMER
//
// The same property `integrations` has and for the same reason: the dial
// happens in the workers gateway, behind the egress allow-list, the tool
// policy and the per-organisation rate limit. What this adds is the credential
// and the record.
//
// # A REFUSAL AND A FAILURE ARE RECORDED, NOT RETURNED
//
// A revoked connection, an ungranted tool, an endpoint that is down: each
// writes a row saying so and answers the RPC successfully. The record is the
// feature, and an error path that stored nothing could not write it. What does
// come back as an error is this deployment failing, which is the only thing
// the workflow's retry policy should key on: a customer's outage must not make
// Temporal retry somebody else's broken endpoint on our schedule.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/integrations"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/gateway"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/secrets"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// EvidenceStaleAfter is how old an observation may get before its tool is
// fetched again.
//
// # A DAY, PER CONNECTION AND PER TOOL
//
// Per tool rather than per connection, because a connection's tools answer
// different questions and one of them going stale is not a reason to dial the
// others. Per connection rather than per organisation, because an organisation
// with six connections should not have five of them held back by one slow one.
//
// A day because that is the cadence of the thing that reads it. The daily
// sweep runs the Watcher over every organisation once, so evidence fresher
// than a day is evidence no sweep will look at before it is refreshed anyway,
// and the extra calls are spent on a customer's rate limit for nothing. It is
// also the honest granularity of the claim: a compliance finding says what an
// organisation's systems reported, and "yesterday" is a fair answer to when.
//
// A CONSTANT AND NOT A REQUEST FIELD. A caller able to say "everything older
// than nothing is stale" is a caller able to dial every customer's systems at
// once, which is the blast radius this surface is arranged around.
const EvidenceStaleAfter = 24 * time.Hour

// RequestWindow is how long an agent's unserved ask keeps its pair due
// (00050). It matches the ask path's pending window in
// `service/watcher/fetchrequest.go` by design rather than by import: the two
// services answer different questions (may this ask queue; is this pair due)
// and coupling them through a shared constant would let a change to one
// silently retune the other. A mismatch is safe in both directions, only
// either widening the already-queued answer or leaving a due pair to the
// daily schedule.
const RequestWindow = time.Hour

// Bounds on one listing. What is not listed now is listed on the next tick.
const (
	DefaultListLimit = 200
	MaxListLimit     = 1000
)

// scheduledArguments is what a scheduled fetch sends a customer's tool.
//
// Nothing, and that is ENT-274's reasoning rather than a placeholder: nothing
// in this system describes what a customer's tool accepts, so the honest bound
// on what nobody can check is zero. A tool that needs arguments to be useful
// is a tool a person triggers from the Integrations page until something can
// validate them.
const scheduledArguments = "{}"

// Targets is the listing half, on the producer pool: cross-organisation, read
// only, and it learns no endpoint and no credential.
type Targets interface {
	FetchTargets(ctx context.Context, staleAfter, requestWindow time.Duration, limit int) ([]postgres.FetchTarget, error)
}

// Plans is the credential-reading half, on the application pool, acting as the
// person who consented to the connection.
type Plans interface {
	FetchPlan(ctx context.Context, integrationID, tool string) (postgres.FetchPlan, error)
}

// Evidence is the writing half, back on the producer pool. The same recorder
// IngestService holds, and the same argument for a separate interface: what a
// caller may do is a statement, and "may list what is stale" and "may write
// into an organisation's memory" are different permissions.
type Evidence interface {
	IngestEvidence(ctx context.Context, record postgres.FetchRecord) (postgres.Deposit, error)
}

// dialer is what this package needs of the gateway client, declared where it
// is used (§21.6). One method, so the whole outbound surface of a scheduled
// fetch is visible without leaving the file.
type dialer interface {
	CallTool(ctx context.Context, orgID, connectionID, endpoint, credential, tool, argumentsJSON string,
		writeCapable bool, policy gateway.Policy) (gateway.Result, error)
}

// Service implements platformv1connect.FetchServiceHandler.
type Service struct {
	targets  Targets
	plans    Plans
	evidence Evidence
	gateway  dialer
	keys     *secrets.Keyring
}

func New(targets Targets, plans Plans, evidence Evidence, client dialer, keys *secrets.Keyring) *Service {
	return &Service{targets: targets, plans: plans, evidence: evidence, gateway: client, keys: keys}
}

// ListFetchTargets lists the connection-and-tool pairs whose evidence is stale.
func (s *Service) ListFetchTargets(
	ctx context.Context,
	req *connect.Request[platformv1.ListFetchTargetsRequest],
) (*connect.Response[platformv1.ListFetchTargetsResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}

	limit := int(req.Msg.GetLimit())
	switch {
	case limit <= 0:
		limit = DefaultListLimit
	case limit > MaxListLimit:
		limit = MaxListLimit
	}

	targets, err := s.targets.FetchTargets(ctx, EvidenceStaleAfter, RequestWindow, limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	res := &platformv1.ListFetchTargetsResponse{}
	for _, target := range targets {
		res.Targets = append(res.Targets, &platformv1.FetchTarget{
			OrgId:         target.OrgID,
			IntegrationId: target.IntegrationID,
			Tool:          target.Tool,
		})
	}
	return connect.NewResponse(res), nil
}

// RunScheduledFetch fetches one granted read-only tool on one connection.
//
// # THE ORDER OF THE CHECKS IS THE PRODUCT BEHAVIOUR
//
// Plan (which refuses a revoked connection and an ungranted tool under the
// consenting person's own authority), then refuse a write-capable tool, then
// open the credential, then dial. Every refusal happens with nothing on the
// wire, and a customer revoking a connection or unticking a tool stops the
// next scheduled fetch rather than being noticed after it.
func (s *Service) RunScheduledFetch(
	ctx context.Context,
	req *connect.Request[platformv1.RunScheduledFetchRequest],
) (*connect.Response[platformv1.RunScheduledFetchResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}

	connectionID := strings.TrimSpace(req.Msg.GetIntegrationId())
	if _, err := uuid.Parse(connectionID); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("integration_id must be a uuid"))
	}
	tool := strings.TrimSpace(req.Msg.GetTool())
	if tool == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("no tool was named"))
	}

	requestedAt := time.Now().UTC()

	plan, err := s.plans.FetchPlan(ctx, connectionID, tool)
	switch {
	case err == nil:
	case errors.Is(err, postgres.ErrNoConnection) && plan.OrgID == "":
		// Nothing by that id anywhere in this deployment. The only shape of
		// "no" with no organisation to record it against, which is why it is
		// the only shape that comes back as an error.
		return nil, connect.NewError(connect.CodeNotFound,
			errors.New("no such connection"))
	case errors.Is(err, domain.ErrRevoked):
		return s.record(ctx, plan.OrgID, connectionID, tool, domain.OutcomeRefused,
			"this connection has been revoked, so it is no longer fetched", requestedAt)
	case errors.Is(err, domain.ErrNotGranted):
		return s.record(ctx, plan.OrgID, connectionID, tool, domain.OutcomeRefused,
			fmt.Sprintf("the tool %q is not granted on this connection", tool), requestedAt)
	case errors.Is(err, postgres.ErrNoConnection):
		// The row exists (the organisation came back) and the transaction
		// acting as the consenting person cannot see it. That is the
		// membership half of the policy doing its job: the standing consent
		// this fetch runs on belongs to somebody who has left.
		return s.record(ctx, plan.OrgID, connectionID, tool, domain.OutcomeRefused,
			"the person who consented to this connection is no longer a member of this organisation, "+
				"so a scheduled fetch has nobody's authority to run under", requestedAt)
	default:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// WRITE-CAPABLE TOOLS ARE NEVER FETCHED ON A SCHEDULE, EVEN GRANTED ONES.
	//
	// `fetch_targets` already excludes them, so nothing reaches this in the
	// ordinary course. It is here because the SQL filter is a filter and this
	// is the boundary: a caller that names a tool directly must meet the same
	// answer the list would have given, and a control that only holds when the
	// caller went through the list is not a control.
	//
	// A person triggering a write from the Integrations page is a person
	// deciding. A schedule doing it is nobody deciding, at three in the
	// morning, with nothing watching.
	if plan.Tool.WriteCapable {
		return s.record(ctx, plan.OrgID, connectionID, tool, domain.OutcomeRefused,
			fmt.Sprintf("the tool %q can write, and a scheduled fetch only reads", tool), requestedAt)
	}

	credential := ""
	if len(plan.Sealed) > 0 {
		credential, err = s.keys.Open(plan.Sealed, plan.KeyID, connectionID)
		if err != nil {
			// A failure rather than a refusal: nothing decided against this
			// call, this deployment's keys are wrong, and an operator needs to
			// see it. The error itself is deliberately not repeated into the
			// row, because it is about key material.
			return s.record(ctx, plan.OrgID, connectionID, tool, domain.OutcomeFailed,
				"this deployment could not open the stored credential for that connection", requestedAt)
		}
	}

	result, err := s.gateway.CallTool(ctx, plan.OrgID, connectionID,
		plan.EndpointURL, credential, tool, scheduledArguments, false,
		gateway.Policy{Granted: plan.Granted, WriteGranted: plan.WriteGranted})
	if err != nil {
		outcome := domain.OutcomeFailed
		if errors.Is(err, gateway.ErrRefused) {
			outcome = domain.OutcomeRefused
		}
		return s.record(ctx, plan.OrgID, connectionID, tool, outcome, reason(err), requestedAt)
	}

	return s.deposit(ctx, postgres.FetchRecord{
		OrgID:         mustParse(plan.OrgID),
		ConnectionID:  mustParse(connectionID),
		Tool:          tool,
		ArgumentsJSON: scheduledArguments,
		ContentJSON:   result.ContentJSON,
		Outcome:       domain.OutcomeSucceeded,
		Redactions:    result.Redactions,
		ObservedAt:    result.FetchedAt,
		RequestedAt:   requestedAt,
	})
}

// record writes a fetch that produced no observation, and answers with it.
func (s *Service) record(
	ctx context.Context,
	orgID, connectionID, tool, outcome, detail string,
	requestedAt time.Time,
) (*connect.Response[platformv1.RunScheduledFetchResponse], error) {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("a connection whose organisation is not a uuid: %w", err))
	}
	connection, err := uuid.Parse(connectionID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("a connection id that is not a uuid: %w", err))
	}
	return s.deposit(ctx, postgres.FetchRecord{
		OrgID:         org,
		ConnectionID:  connection,
		Tool:          tool,
		ArgumentsJSON: scheduledArguments,
		Outcome:       outcome,
		Detail:        detail,
		RequestedAt:   requestedAt,
	})
}

// deposit writes the record and answers with what it left behind.
//
// A write that fails is the one thing here that comes back as an error, and it
// is `internal` rather than `unavailable` because it is this deployment rather
// than a customer's: the workflow retries it, and a fetch nobody could record
// is a fetch that may as well not have happened.
func (s *Service) deposit(
	ctx context.Context, record postgres.FetchRecord,
) (*connect.Response[platformv1.RunScheduledFetchResponse], error) {
	deposit, err := s.evidence.IngestEvidence(ctx, record)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&platformv1.RunScheduledFetchResponse{
		Outcome:       record.Outcome,
		Detail:        record.Detail,
		FetchId:       deposit.FetchID,
		EvidenceId:    deposit.EvidenceID,
		EvidenceIsNew: deposit.EvidenceIsNew,
	}), nil
}

// mustParse is safe where it is used: both ids were parsed before the call
// that produced them, so a failure here is impossible rather than unhandled.
// The zero uuid it would return is refused by the store's own validation.
func mustParse(id string) uuid.UUID {
	parsed, _ := uuid.Parse(id)
	return parsed
}

// reason strips Connect's code prefix off an error message.
//
// The message ends up in a fetch record a customer reads, and
// "unavailable: the endpoint did not answer usefully" reads as a stack trace
// where the second half reads as an explanation. The same helper the console's
// path has, repeated rather than shared because sharing it would mean this
// package importing the console's.
func reason(err error) string {
	message := err.Error()
	if _, after, found := strings.Cut(message, ": "); found {
		return after
	}
	return message
}
