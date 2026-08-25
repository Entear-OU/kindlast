package watcher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// How often a customer's system can be dialled because a model asked
// (ENT-279).
//
// Server constants, deliberately not request fields, for the reason the
// scheduled fetch keeps its staleness interval out of its request: a caller
// able to send zero would be able to dial every customer's systems at once.
//
// # ONE HOUR, AND WHY
//
// The scheduled fetch refreshes every granted read-only tool daily, so an
// agent's ask only matters for a pair the schedule has not reached: a
// connection made this morning, or evidence the model judges too stale to
// raise a signal on. An hour is short enough that a sweep which found
// twenty-hour-old evidence can have it refreshed before the next day's sweep,
// and long enough that a sweep activity's three retries, each asking again,
// cause one dial rather than three: the retry's ask lands inside the cooldown
// or the pending window and queues nothing. The arithmetic that matters is
// the ceiling per customer: with one sweep a day and this cooldown, an agent
// adds at most a couple of dials per pair per day on top of the schedule,
// whatever the model does.
const (
	// A pair attempted this recently is answered from the record, whatever
	// the outcome. Attempts count rather than successes, so a down endpoint
	// is not redialled on every ask.
	fetchCooldown = time.Hour
	// An unserved request this young answers `already_queued` rather than
	// queueing a second row. Old unserved requests stop counting, so a relay
	// outage does not leave a pair permanently unable to ask again.
	fetchPendingWindow = time.Hour
)

// A sentence, not an essay: the reason is model output on its way into a
// customer's record. Matches the check constraint on `fetch_requests.reason`.
const maxFetchReason = 500

// RequestFetch queues a fetch of one granted tool on one connection, or says
// why nothing was queued (ENT-279).
//
// # THE AGENT ASKS, THIS DECIDES, THE GATEWAY FETCHES
//
// The producer role can write an ask and nothing else: no credential, no
// endpoint, no dial. The fetch a queued ask causes runs later, through the
// workers gateway, behind the egress allow-list, under the connection's own
// standing consent, exactly as a scheduled fetch does. So the answer here is
// an acknowledgement, and it says so in words, because the one dishonest
// thing this RPC could do is let a model believe it fetched something.
//
// # A REFUSAL HERE IS A CODE, NOT A RECORDED FETCH
//
// FetchNow records refusals in `integration_fetches` because a person asked
// and the log is their answer. Here the asker is a run, and the run's own
// record is where the ask lands: the dispatcher writes every call, refused
// ones included, into `agent_runs.tool_calls` (ENT-277). Writing a second
// copy into the fetch log would put rows nobody caused a dial with into the
// list a customer reads as "what was fetched".
func (s *Service) RequestFetch(
	ctx context.Context,
	req *connect.Request[platformv1.RequestFetchRequest],
) (*connect.Response[platformv1.RequestFetchResponse], error) {
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
			errors.New("connection_id is required: say which connection to fetch from"))
	}
	tool := strings.TrimSpace(req.Msg.GetTool())
	if tool == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("tool is required: a fetch is of one granted tool, so there is "+
				"no fetching a connection without naming one"))
	}
	reason := strings.TrimSpace(req.Msg.GetReason())
	if len(reason) > maxFetchReason {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("reason exceeds %d characters", maxFetchReason))
	}

	now := time.Now().UTC()
	ask, err := s.producer.RequestFetch(ctx, req.Msg.GetOrgId(), req.Msg.GetConnectionId(),
		tool, reason, now.Add(-fetchCooldown), now.Add(-fetchPendingWindow))
	switch {
	case errors.Is(err, postgres.ErrBadOrganisation):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, postgres.ErrNoConnection):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, postgres.ErrConnectionRevoked):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, postgres.ErrToolNotGranted):
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, postgres.ErrToolWrites):
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&platformv1.RequestFetchResponse{
		State:     ask.State,
		Detail:    acknowledge(ask, tool, now),
		RequestId: ask.RequestID,
	}), nil
}

// acknowledge is the sentence a model reads back, and it is OUR sentence.
//
// Nothing here repeats `ask.LastDetail`. That column derives from a
// customer's endpoint, and this is the one message on this surface a model
// always reads, so relaying it would open a path from an endpoint into every
// asking run's context. The outcome and the age are core-api's own facts and
// they are all a model needs to decide what to do next.
func acknowledge(ask postgres.FetchAsk, tool string, now time.Time) string {
	where := tool
	if ask.ConnectionName != "" {
		where = fmt.Sprintf("%s on %q", tool, ask.ConnectionName)
	}

	switch ask.State {
	case postgres.FetchAskAlreadyQueued:
		return fmt.Sprintf("a fetch of %s is already queued and has not run yet; "+
			"asking again changes nothing", where)
	case postgres.FetchAskRecentlyFetched:
		age := ago(now.Sub(ask.LastAttemptAt))
		if ask.LastOutcome == "succeeded" {
			return fmt.Sprintf("%s was fetched successfully %s, so what it reported "+
				"is already stored; read it with read_evidence", where, age)
		}
		return fmt.Sprintf("the last fetch of %s, %s, %s; it is not retried this soon",
			where, age, ask.LastOutcome)
	default:
		return fmt.Sprintf("queued: a fetch of %s will run in the background and "+
			"deposit what it reports. This run will not see the result; what is "+
			"already stored is readable now with read_evidence, and the next sweep "+
			"will see what this fetch deposits", where)
	}
}

func ago(d time.Duration) string {
	minutes := int(d.Minutes())
	switch {
	case minutes < 1:
		return "less than a minute ago"
	case minutes == 1:
		return "1 minute ago"
	default:
		return fmt.Sprintf("%d minutes ago", minutes)
	}
}
