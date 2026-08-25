package delivery

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/notify"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/modelroute"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The Messenger's half of the plan (ENT-280).
//
// # THE PLAN CARRIES AN INSTRUCTION, NEVER WORDS
//
// The workflow between the plan and the send is the only thing that talks to
// the Messenger, and everything it may hand the model is decided here, from
// rows core-api already holds. MessageContext structurally cannot carry what
// the finding says (§17.1, pinned by tests on both sides of the wire), so the
// instruction is safe to put in a workflow history and safe to lose.

// ModelRouter answers which model an organisation's runs are recorded
// against. It is the same seam CompletionService resolves through; here only
// the NAMES travel, because the instruction rides a workflow history and a
// credential cannot (ENT-236).
type ModelRouter interface {
	Resolve(ctx context.Context, orgID string) (modelroute.Route, error)
}

// Option configures the service at construction, alongside the method-form
// WithClock that predates it.
type Option func(*Service)

// WithModelRouter names the model the drafting instruction is recorded
// against. Without one the instruction still exists and names no endpoint,
// which the worker reads as the deployment's own model.
func WithModelRouter(r ModelRouter) Option {
	return func(s *Service) { s.models = r }
}

// draftInstruction builds the plan's drafting half, or nothing.
//
// Nothing is a correct answer twice over. A notification nobody will receive
// asks no model to write words nobody will read. And an organisation whose
// chosen model cannot be resolved drafts nothing rather than failing the
// plan: a doorbell nobody receives is worse than a doorbell in the template's
// words, and since no draft is asked for, there is no quiet-fallback
// disclosure of anything either.
func (s *Service) draftInstruction(
	ctx context.Context, tx pgx.Tx, bell postgres.Doorbell,
	recipients []postgres.Recipient, sending []postgres.Recipient,
) *platformv1.DraftInstruction {
	if len(sending) == 0 {
		return nil
	}

	pending, total, err := s.outbox.FindingCounts(ctx, tx, bell.OrgID, bell.FindingID)
	if err != nil {
		// The counts are colour, not authority. A plan that lost its draft
		// over a counting error would couple delivery to a read nothing else
		// depends on.
		return nil
	}

	instruction := &platformv1.DraftInstruction{
		OrgId: bell.OrgID,
		Context: &platformv1.MessageContext{
			OrgName:        sending[0].OrgName,
			Severity:       sending[0].FindingSeverity,
			OpenFindings:   pending,
			FirstForOrg:    total == 1,
			HasApproveLink: anyVerified(sending),
			Channels:       channelsOf(sending),
		},
	}

	if s.models != nil {
		route, err := s.models.Resolve(ctx, bell.OrgID)
		if err != nil {
			return nil
		}
		if route.Provider != "" && route.Provider != "instance" {
			// Names only. The credential is opened by CompletionService at
			// call time, inside core-api; an instruction that carried it
			// would write it into a workflow history.
			instruction.ModelEndpoint = &platformv1.ModelEndpoint{
				Provider: route.Provider,
				Model:    route.Model,
			}
		}
	}
	return instruction
}

func anyVerified(recipients []postgres.Recipient) bool {
	for _, r := range recipients {
		if r.EmailVerified {
			return true
		}
	}
	return false
}

// channelsOf lists the distinct channels the send will actually use, in
// route order, so the draft can say "your colleagues will read this in chat"
// only when somebody will.
func channelsOf(recipients []postgres.Recipient) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range recipients {
		route := notify.RouteFor(r.FindingChannel, r.Email, r.TelegramChatID, r.TelegramVerified)
		if !route.Deliverable() || seen[route.Channel] {
			continue
		}
		seen[route.Channel] = true
		out = append(out, route.Channel)
	}
	return out
}
