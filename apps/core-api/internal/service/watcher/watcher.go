// Package watcher serves WatcherService: what an agentic Watcher reads, and
// the one thing it may write (ENT-258).
//
// On the internal chain with the sweep, on `internal:ingest`, and for the same
// reason: the caller is a service principal with no membership, so there is no
// tenancy to resolve and the organisation travels in the message. What
// replaces tenancy is the producer role's own policies, which scope every read
// and the write to the organisation the GUC names (00008).
package watcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/modelroute"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Producer is what this handler needs of the agent pool, declared where it is
// used (§21.6).
type Producer interface {
	WatcherContextFor(ctx context.Context, orgID string) (postgres.WatcherContext, error)
	RaiseSignal(ctx context.Context, orgID string, signal postgres.Signal) (string, bool, error)
	// EvidenceFor reads what one connection has already reported through one
	// of its granted tools (ENT-274). A read and nothing more: there is
	// deliberately no method here that reaches a customer's system, and the
	// RPC's own comment says what it would take to add one.
	EvidenceFor(ctx context.Context, orgID, connectionID, tool string, limit int) (string, []postgres.StoredObservation, error)
}

// ModelRoute resolves which model serves one organisation, declared here for
// the same reason (§21.6). Nil for a deployment that runs no Intelligence,
// which is what `intelligence_available: false` reports.
type ModelRoute interface {
	Resolve(ctx context.Context, orgID string) (modelroute.Route, error)
}

// The vocabulary the schema permits, checked here so a caller gets
// `invalid_argument` and the list rather than a constraint name out of a
// failed transaction (00001's `watcher_findings` check constraints).
//
// `regulatory_update` is in the list and no deterministic detector emits it:
// it is the kind an agent that reads the corpus would raise, and it has been
// permitted since the baseline.
var (
	kinds      = []string{"deadline", "profile_gap", "dsar", "regulatory_update"}
	severities = []string{"low", "medium", "high", "critical"}
)

// Service implements platformv1connect.WatcherServiceHandler.
type Service struct {
	producer Producer
	models   ModelRoute
}

// New builds the service. `models` may be nil: a deployment that runs no
// Intelligence has no route to resolve, and a context assembled for it reports
// `intelligence_available: false` rather than failing.
func New(producer Producer, models ModelRoute) *Service {
	return &Service{producer: producer, models: models}
}

// WatcherContext assembles everything one run reasons over.
func (s *Service) WatcherContext(
	ctx context.Context,
	req *connect.Request[platformv1.WatcherContextRequest],
) (*connect.Response[platformv1.WatcherContextResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	orgID := req.Msg.GetOrgId()
	if orgID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("org_id is required"))
	}

	context, err := s.producer.WatcherContextFor(ctx, orgID)
	if errors.Is(err, postgres.ErrBadOrganisation) {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	res := &platformv1.WatcherContextResponse{HasProfile: context.HasProfile}
	if !context.HasProfile {
		// An organisation part way through onboarding. Not an error: there is
		// nothing to watch yet, and a run that is told so stops rather than
		// reasoning about an empty context.
		return connect.NewResponse(res), nil
	}
	if context.LastSweptAt != nil {
		res.LastSweptAt = timestamppb.New(*context.LastSweptAt)
	}
	for _, f := range context.Facts {
		res.Facts = append(res.Facts, &platformv1.ProfileFact{
			Key: f.Key, ValueJson: f.ValueJSON, Source: f.Source,
			ValidFrom: timestamppb.New(f.ValidFrom),
		})
	}
	for _, c := range context.Connections {
		connection := &platformv1.WatchedConnection{
			ConnectionId: c.ID, Kind: c.Kind, DisplayName: c.DisplayName,
			Status: c.Status, Revoked: c.Revoked,
		}
		for _, t := range c.Tools {
			connection.Tools = append(connection.Tools, &platformv1.ConnectionTool{
				Name: t.Name, Description: t.Description,
				WriteCapable: t.WriteCapable, Granted: t.Granted,
			})
		}
		res.Connections = append(res.Connections, connection)
	}
	for _, o := range context.Obligations {
		res.Obligations = append(res.Obligations, &platformv1.CitableObligation{
			Slug: o.Slug, Title: o.Title, Summary: o.Summary,
		})
	}
	if err := s.route(ctx, orgID, res); err != nil {
		return nil, err
	}
	for _, sig := range context.OpenSignals {
		res.OpenSignals = append(res.OpenSignals, &platformv1.OpenSignal{
			SignalId: sig.ID, Kind: sig.Kind, DedupKey: sig.DedupKey,
			Title: sig.Title, Severity: sig.Severity, Source: sig.Source,
			UpdatedAt: timestamppb.New(sig.UpdatedAt),
		})
	}
	return connect.NewResponse(res), nil
}

// RaiseSignal writes one signal.
//
// Everything a model produced is validated before it reaches the database, and
// the order matters: the vocabulary first, so a wrong `kind` is a message
// naming the four rather than a check constraint; then the fields the
// deduplication depends on, because a signal with no key is a row a day; then
// the obligation, in the store, where the corpus is.
func (s *Service) RaiseSignal(
	ctx context.Context,
	req *connect.Request[platformv1.RaiseSignalRequest],
) (*connect.Response[platformv1.RaiseSignalResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	if err := validate(req.Msg); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	id, raised, err := s.producer.RaiseSignal(ctx, req.Msg.GetOrgId(), postgres.Signal{
		Kind:           req.Msg.GetKind(),
		DedupKey:       req.Msg.GetDedupKey(),
		Title:          req.Msg.GetTitle(),
		Detail:         req.Msg.GetDetail(),
		Severity:       req.Msg.GetSeverity(),
		ObligationSlug: req.Msg.GetObligationSlug(),
		MetadataJSON:   req.Msg.GetMetadataJson(),
	})
	switch {
	case errors.Is(err, postgres.ErrBadOrganisation),
		errors.Is(err, postgres.ErrUnknownObligation):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, postgres.ErrNoProfile):
		// Nothing to hang it on. `failed_precondition` rather than
		// `invalid_argument`: the request is well formed and the organisation
		// is not ready, which is a different thing to tell a caller.
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&platformv1.RaiseSignalResponse{
		SignalId: id, Raised: raised,
	}), nil
}

// ReadEvidence answers with what one connection has already reported (ENT-274).
//
// # THE HANDLER DECIDES NOTHING ABOUT THE CONTENT AND MUST NOT START
//
// It names a connection and a tool, checks the grant, and returns rows. It
// does not parse the body, branch on it, summarise it or truncate it, and the
// response carries it as a string for the reason every other third-party
// payload on this surface is one: this is content on its way in front of a
// model, and a handler that read it would be a handler a customer's system can
// steer.
//
// Bounding what a run may look at is the harness's job and is done there,
// where the budget is. What is bounded HERE is only how many rows one call may
// return, because that is a property of the query rather than of a run.
//
// # REFUSING IS THE INTERESTING PATH
//
// A tool the connection has not granted is `permission_denied`, which the
// Python harness maps onto a refusal rather than a failure: a customer's grant
// saying no is a control working. An unknown connection is `not_found`, and it
// is the same answer for a connection belonging to somebody else, because the
// producer role saw no rows either way and what a caller must never learn here
// is whether an id it guessed exists in another organisation.
func (s *Service) ReadEvidence(
	ctx context.Context,
	req *connect.Request[platformv1.ReadEvidenceRequest],
) (*connect.Response[platformv1.ReadEvidenceResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	if req.Msg.GetOrgId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("org_id is required"))
	}
	if req.Msg.GetConnectionId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("connection_id is required"))
	}
	tool := strings.TrimSpace(req.Msg.GetTool())
	if tool == "" {
		// Not defaulted to "everything this connection ever said". A grant is
		// per tool, so an unfiltered read would let one granted tool carry
		// back what an ungranted one deposited before its grant was withdrawn.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("tool is required: a grant is per tool, so there is no "+
				"reading a connection without naming one"))
	}

	name, observations, err := s.producer.EvidenceFor(
		ctx, req.Msg.GetOrgId(), req.Msg.GetConnectionId(), tool, int(req.Msg.GetLimit()))
	switch {
	case errors.Is(err, postgres.ErrBadOrganisation):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, postgres.ErrNoConnection):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, postgres.ErrToolNotGranted):
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	res := &platformv1.ReadEvidenceResponse{ConnectionName: name}
	for _, o := range observations {
		res.Observations = append(res.Observations, &platformv1.StoredObservation{
			EvidenceId:   o.EvidenceID,
			ConnectionId: o.ConnectionID,
			Tool:         o.Tool,
			ObservedAt:   timestamppb.New(o.ObservedAt),
			FetchedAt:    timestamppb.New(o.FetchedAt),
			BodyJson:     o.BodyJSON,
		})
	}
	return connect.NewResponse(res), nil
}

// route fills in which model would serve a run over this context.
//
// A provider the organisation chose and core-api cannot honour is
// `failed_precondition` rather than a silent fall back to the deployment's own
// model. `AGENTS.md` has no fallback rule for a reason that is sharper here
// than for narration: falling back would send this organisation's compliance
// context to a model it did not choose, and would record a provider that did
// not serve it.
func (s *Service) route(
	ctx context.Context, orgID string, res *platformv1.WatcherContextResponse,
) error {
	if s.models == nil {
		// No Intelligence in this deployment. Not an error, and not a reason
		// to withhold the context: a caller is told so and does not schedule
		// a run.
		return nil
	}
	route, err := s.models.Resolve(ctx, orgID)
	if err != nil {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	res.IntelligenceAvailable = true
	res.ModelProvider = route.Provider
	res.ModelName = route.Model
	return nil
}

// Bounds on what one signal may carry. Generous, and present so that a model
// that has decided to write an essay into a title cannot.
const (
	maxTitle    = 200
	maxDetail   = 4000
	maxDedupKey = 200
	maxMetadata = 16 << 10
)

func validate(msg *platformv1.RaiseSignalRequest) error {
	if msg.GetOrgId() == "" {
		return errors.New("org_id is required")
	}
	if !slices.Contains(kinds, msg.GetKind()) {
		return fmt.Errorf("kind must be one of %v", kinds)
	}
	if !slices.Contains(severities, msg.GetSeverity()) {
		return fmt.Errorf("severity must be one of %v", severities)
	}
	switch {
	case msg.GetDedupKey() == "":
		return errors.New("dedup_key is required: without it a daily sweep " +
			"raises a new signal every day for one unchanged condition")
	case len(msg.GetDedupKey()) > maxDedupKey:
		return fmt.Errorf("dedup_key exceeds %d characters", maxDedupKey)
	case msg.GetTitle() == "":
		return errors.New("title is required")
	case len(msg.GetTitle()) > maxTitle:
		return fmt.Errorf("title exceeds %d characters", maxTitle)
	case len(msg.GetDetail()) > maxDetail:
		return fmt.Errorf("detail exceeds %d characters", maxDetail)
	case len(msg.GetMetadataJson()) > maxMetadata:
		return fmt.Errorf("metadata_json exceeds %d bytes", maxMetadata)
	}
	if raw := msg.GetMetadataJson(); raw != "" {
		// Checked here rather than left to the `::jsonb` cast, so a model that
		// produced something that is not JSON is told that, rather than the
		// caller reading a Postgres syntax error out of an internal.
		var parsed json.RawMessage
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return fmt.Errorf("metadata_json is not JSON: %w", err)
		}
	}
	return nil
}
