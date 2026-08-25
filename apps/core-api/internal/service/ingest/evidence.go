package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	integrationsdomain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/integrations"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// EvidenceRecorder is what this handler needs to store what a machine fetched
// (ENT-231).
//
// A fourth dependency rather than a method on RunRecorder, although both run
// on `kindlast_agent` and both could sit behind one interface. They are
// separated for the reason the three above are: an interface is a statement
// about what a caller may do, and "may record an agent run" and "may write
// into an organisation's memory" are different permissions. Merging them would
// mean the corpus loader's neighbour could do both because it happened to hold
// one handle.
type EvidenceRecorder interface {
	IngestEvidence(ctx context.Context, record postgres.FetchRecord) (postgres.Deposit, error)
}

// IngestEvidence records what a machine fetched from a customer's own system.
//
// # WHAT THIS DOES NOT DO, AND WHY THAT IS RIGHT
//
// It does not check an egress allow-list, does not decide whether a tool was
// granted, and does not redact. All three happened in the gateway, before the
// content crossed the network, and each of them needs something this process
// does not hold: the deployment's allow-list, the connection's live policy,
// and the unredacted content.
//
// Re-applying redaction here is the tempting one and it is the worst of the
// three. A second implementation is free to disagree with the first, and the
// one that decides what a customer's data looks like on the wire is the one
// that already ran. What this would add is the illusion of a second control.
//
// # WHAT IT DOES ENFORCE
//
// The shape, which is the part a caller can get wrong in ways nobody notices
// until a page will not render: an organisation and a connection that are real
// uuids, an outcome from the closed set, a reason for anything that was not a
// success, and JSON that parses. A malformed row stored now surfaces to a
// customer reading their own evidence months later, which is the wrong person
// to find it.
func (s *Service) IngestEvidence(
	ctx context.Context,
	req *connect.Request[platformv1.IngestEvidenceRequest],
) (*connect.Response[platformv1.IngestEvidenceResponse], error) {
	if s.evidence == nil {
		// Nil when this deployment runs no agents, and then Unimplemented
		// rather than a panic and rather than a 404 on a path that does exist.
		// The same shape IngestCorpus takes for its own absent dependency.
		return nil, connect.NewError(connect.CodeUnimplemented,
			errors.New("this deployment records no evidence"))
	}

	message := req.Msg

	orgID, err := uuid.Parse(strings.TrimSpace(message.GetOrgId()))
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("org_id must be a uuid"))
	}
	connectionID, err := uuid.Parse(strings.TrimSpace(message.GetConnectionId()))
	if err != nil {
		// Required even for a refusal. "What did it try to reach" is the
		// question a refusal exists to answer, and a refusal naming no
		// connection answers nothing.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("connection_id must be a uuid"))
	}

	tool := strings.TrimSpace(message.GetTool())
	if tool == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("no tool was named"))
	}

	outcome := strings.TrimSpace(message.GetOutcome())
	switch outcome {
	case integrationsdomain.OutcomeSucceeded,
		integrationsdomain.OutcomeRefused,
		integrationsdomain.OutcomeFailed:
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("outcome must be succeeded, refused or failed"))
	}

	detail := strings.TrimSpace(message.GetDetail())
	if outcome != integrationsdomain.OutcomeSucceeded && detail == "" {
		// A row that says neither what happened nor why tells nobody anything,
		// and the database refuses it too. Refusing here as well means the
		// caller gets a sentence rather than a constraint name.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("a fetch that did not succeed must say why"))
	}

	content := strings.TrimSpace(message.GetContentJson())
	if outcome != integrationsdomain.OutcomeSucceeded && content != "" {
		// The database has the same constraint, expressed as "evidence only on
		// success". Refusing here keeps a caller from discovering it as a
		// check_violation, and stops a refusal quietly carrying content it
		// should not have.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("a fetch that did not succeed has no content to store"))
	}
	if content != "" && !json.Valid([]byte(content)) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("the content is not JSON"))
	}
	if arguments := strings.TrimSpace(message.GetArgumentsJson()); arguments != "" &&
		!json.Valid([]byte(arguments)) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("the arguments are not JSON"))
	}
	if message.GetRedactions() < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("redactions cannot be negative"))
	}

	record := postgres.FetchRecord{
		OrgID:         orgID,
		ConnectionID:  connectionID,
		Tool:          tool,
		ArgumentsJSON: strings.TrimSpace(message.GetArgumentsJson()),
		ContentJSON:   content,
		Outcome:       outcome,
		Detail:        detail,
		Redactions:    message.GetRedactions(),
	}
	if stamp := message.GetObservedAt(); stamp != nil {
		record.ObservedAt = stamp.AsTime()
	}
	if stamp := message.GetRequestedAt(); stamp != nil {
		record.RequestedAt = stamp.AsTime()
	}

	deposit, err := s.evidence.IngestEvidence(ctx, record)
	if err != nil {
		// Logged as well as returned, because this caller is a schedule rather
		// than a browser and an error nobody sees is one nobody fixes.
		s.logger.Error("could not record what a fetch produced",
			"org_id", orgID, "connection_id", connectionID, "tool", tool, "error", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&platformv1.IngestEvidenceResponse{
		EvidenceId:    deposit.EvidenceID,
		FetchId:       deposit.FetchID,
		EvidenceIsNew: deposit.EvidenceIsNew,
	}), nil
}
