// Package ingest serves IngestService: writing the corpus (ENT-207).
//
// # NOTHING HERE IS REACHABLE BY A PERSON
//
// The RPC declares `internal:ingest`, which the seed issues to service clients
// through client credentials and never to the browser client, and it lives in
// the platform package so "is this reachable by a human" is answered by the
// import path rather than by reading a handler.
//
// The reason is the product's central claim rather than a tenancy concern. A
// customer trusts a finding because they can check it against the law; that is
// worth nothing if a request from the console could change the law it is
// checked against.
//
// # AND IT RUNS ON NO TENANT TRANSACTION
//
// Unlike every handler in `core.v1`, this one takes no organisation. The corpus
// has no `org_id`: it is the same regulation for every customer, and ENT-207
// says in terms not to give it one. So there is no `Kindlast-Org-Id` here and
// no tenancy interceptor in front of it, the same shorter chain SweepService
// runs on and for a related reason.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/delegation"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// writer is what this handler needs of the corpus store, declared where it is
// used (§21.6).
// Writer is exported so the server package can declare the dependency without
// importing a database driver.
type Writer interface {
	Ingest(ctx context.Context, pack domain.Pack, dryRun bool) (postgres.Counts, []string, error)
}

// RunRecorder is what this handler needs to record an agent run (ENT-218).
//
// A second dependency rather than a method on Writer, because the two run on
// different pools as different roles: the corpus writes as `kindlast_ingest`,
// which holds grants on the ten regulatory tables and nothing else, and a run
// record writes as `kindlast_agent`, which holds insert on `agent_runs` and
// cannot touch the corpus at all. One interface would suggest one credential.
type RunRecorder interface {
	RecordAgentRun(ctx context.Context, run postgres.AgentRun) (uuid.UUID, error)
}

// Delegations resolves the delegation a run presents as evidence that a person
// asked for it (ENT-230).
//
// A third dependency rather than a method on either of the two above, for the
// same reason those are separate: it runs on the application pool, which is
// neither the corpus role nor the agent role. It is also the only one of the
// three that is not a write.
type Delegations interface {
	ResolveDelegation(ctx context.Context, token string) (delegation.Grant, error)
}

// Service implements platformv1connect.IngestServiceHandler.
type Service struct {
	store       Writer
	runs        RunRecorder
	delegations Delegations
	logger      *slog.Logger
	now         func() time.Time
}

func New(store Writer, runs RunRecorder, delegations Delegations, logger *slog.Logger) *Service {
	if logger == nil {
		// A discarding logger rather than a nil dereference. This handler's
		// caller is a schedule, so the one place it must not fail is while
		// reporting that something else failed.
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{
		store: store, runs: runs, delegations: delegations,
		logger: logger, now: time.Now,
	}
}

func (s *Service) IngestCorpus(
	ctx context.Context,
	req *connect.Request[platformv1.IngestCorpusRequest],
) (*connect.Response[platformv1.IngestCorpusResponse], error) {
	// Nil since ENT-218, because the service is now registered when EITHER
	// dependency exists: a deployment may run agents against a corpus somebody
	// else loaded. Unimplemented rather than a panic, and rather than a 404 on
	// a path that does exist.
	if s.store == nil {
		return nil, connect.NewError(connect.CodeUnimplemented,
			errors.New("this deployment does not write the corpus"))
	}

	pack := toPack(req.Msg.GetPack())

	// Shape first, before anything touches the database. Everything checkable
	// without a query is checked here, and the result is every problem rather
	// than the first: a curator fixing a pack wants the list, and an ingest
	// reporting one problem per run turns a ten-error pack into ten round trips
	// through a schedule.
	if problems := pack.Validate(); len(problems) > 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("the pack is not valid: %s", domain.Problems(problems)))
	}

	counts, unresolved, err := s.store.Ingest(ctx, pack, req.Msg.GetDryRun())
	if err != nil {
		s.logger.Error("ingesting the corpus failed",
			"pack", pack.ID, "error", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if len(unresolved) > 0 {
		if req.Msg.GetDryRun() {
			// A dry run's whole job is to report this without failing, so a
			// curator can see every dangling citation at once and decide whether
			// to fix the pack or ingest the corpus it depends on first.
			return connect.NewResponse(&platformv1.IngestCorpusResponse{
				Applied:             false,
				Counts:              toCounts(counts),
				UnresolvedCitations: unresolved,
				IngestedAt:          timestamppb.New(s.now()),
			}), nil
		}

		// A real run fails. AGENTS.md opens by calling a fabricated citation
		// worse than nothing, and an obligation pointing at an article that is
		// not in the corpus surfaces to a customer as a finding whose "check
		// this against the law" goes nowhere. Reporting it as a success with a
		// note attached is how that ships.
		s.logger.Error("refusing a pack with citations that do not resolve",
			"pack", pack.ID, "unresolved", len(unresolved))
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("%d citation(s) do not resolve: %s",
				len(unresolved), joinLimited(unresolved, 10)))
	}

	s.logger.Info("ingested a regulation pack",
		"pack", pack.ID,
		"dry_run", req.Msg.GetDryRun(),
		"documents", counts.Documents,
		"articles", counts.Articles,
		"obligations", counts.Obligations)

	return connect.NewResponse(&platformv1.IngestCorpusResponse{
		Applied:    !req.Msg.GetDryRun(),
		Counts:     toCounts(counts),
		IngestedAt: timestamppb.New(s.now()),
	}), nil
}

// joinLimited keeps an error message readable when a pack is badly wrong.
//
// A pack with four hundred dangling citations would otherwise produce an error
// nobody can read in a terminal, and the first ten are enough to tell somebody
// what kind of mistake they made. The dry run reports all of them.
func joinLimited(messages []string, limit int) string {
	if len(messages) <= limit {
		return joinAll(messages)
	}
	return fmt.Sprintf("%s, and %d more (run with dry_run to see them all)",
		joinAll(messages[:limit]), len(messages)-limit)
}

func joinAll(messages []string) string {
	out := ""
	for i, message := range messages {
		if i > 0 {
			out += "; "
		}
		out += message
	}
	return out
}

func toPack(in *platformv1.RegulationPack) domain.Pack {
	if in == nil {
		// Validation reports this as an empty pack, which is the message a
		// caller that failed to load its file needs.
		return domain.Pack{}
	}

	pack := domain.Pack{ID: in.GetPackId()}

	if doc := in.GetDocument(); doc != nil {
		pack.Document = &domain.Document{
			Celex:       doc.GetCelexNumber(),
			Title:       doc.GetTitle(),
			ShortTitle:  doc.GetShortTitle(),
			VersionDate: doc.GetVersionDate(),
			OfficialURL: doc.GetOfficialUrl(),
		}
		for _, a := range doc.GetArticles() {
			article := domain.Article{
				Number:        int(a.GetArticleNumber()),
				Heading:       a.GetHeading(),
				Summary:       a.GetSummary(),
				EffectiveDate: a.GetEffectiveDate(),
			}
			for _, p := range a.GetParagraphs() {
				article.Paragraphs = append(article.Paragraphs, domain.Paragraph{
					Label:    p.GetLabel(),
					Summary:  p.GetSummary(),
					Ordering: int(p.GetOrdering()),
				})
			}
			pack.Document.Articles = append(pack.Document.Articles, article)
		}
		for _, r := range doc.GetRecitals() {
			pack.Document.Recitals = append(pack.Document.Recitals, domain.Recital{
				Number:  int(r.GetRecitalNumber()),
				Summary: r.GetSummary(),
			})
		}
		for _, x := range doc.GetAnnexes() {
			annex := domain.Annex{
				Label:         x.GetLabel(),
				Heading:       x.GetHeading(),
				Summary:       x.GetSummary(),
				EffectiveDate: x.GetEffectiveDate(),
			}
			for _, item := range x.GetItems() {
				annex.Items = append(annex.Items, domain.AnnexItem{
					Label:         item.GetLabel(),
					Heading:       item.GetHeading(),
					Summary:       item.GetSummary(),
					EffectiveDate: item.GetEffectiveDate(),
					Ordering:      int(item.GetOrdering()),
				})
			}
			pack.Document.Annexes = append(pack.Document.Annexes, annex)
		}
		for _, link := range doc.GetArticleRecitals() {
			pack.Document.ArticleRecitals = append(pack.Document.ArticleRecitals,
				domain.ArticleRecitalLink{
					ArticleNumber: int(link.GetArticleNumber()),
					RecitalNumber: int(link.GetRecitalNumber()),
				})
		}
	}

	for _, o := range in.GetObligations() {
		obligation := domain.Obligation{
			Slug:            o.GetSlug(),
			Title:           o.GetTitle(),
			Summary:         o.GetSummary(),
			AppliesWhenJSON: o.GetAppliesWhenJson(),
			Severity:        o.GetSeverity(),
			DueWithinDays:   int(o.GetDueWithinDays()),
			Recurrence:      o.GetRecurrence(),
			EffectiveDate:   o.GetEffectiveDate(),
			TopicTags:       o.GetTopicTags(),
			ActionType:      o.GetActionType(),
		}
		if c := o.GetCitation(); c != nil {
			obligation.Citation = domain.Citation{
				Kind:           c.GetKind(),
				Celex:          c.GetCelex(),
				ArticleNumber:  int(c.GetArticleNumber()),
				RecitalNumber:  int(c.GetRecitalNumber()),
				AnnexLabel:     c.GetAnnexLabel(),
				ParagraphLabel: c.GetParagraphLabel(),
			}
		}
		pack.Obligations = append(pack.Obligations, obligation)
	}

	for _, g := range in.GetGuidelines() {
		pack.Guidelines = append(pack.Guidelines, domain.Guideline{
			Slug:        g.GetSlug(),
			Publisher:   g.GetPublisher(),
			Title:       g.GetTitle(),
			AdoptedDate: g.GetAdoptedDate(),
			Version:     g.GetVersion(),
			SourceURL:   g.GetSourceUrl(),
			TopicTags:   g.GetTopicTags(),
		})
	}

	for _, d := range in.GetEnforcementDecisions() {
		decision := domain.EnforcementDecision{
			Slug:         d.GetSlug(),
			DPA:          d.GetDpa(),
			Title:        d.GetTitle(),
			DecisionDate: d.GetDecisionDate(),
			FineEUR:      d.GetFineEur(),
			HasFine:      d.GetHasFine(),
			Summary:      d.GetSummary(),
			SourceURL:    d.GetSourceUrl(),
			TopicTags:    d.GetTopicTags(),
		}
		for _, article := range d.GetGdprArticles() {
			decision.GDPRArticles = append(decision.GDPRArticles, int(article))
		}
		pack.EnforcementDecisions = append(pack.EnforcementDecisions, decision)
	}

	return pack
}

// RecordAgentRun stores one finished run (ENT-218, §26.3).
//
// The organisation comes from the message rather than a header, because a run
// happens for whichever tenant the work belonged to and Intelligence holds no
// session to derive one from. See the store's comment for why that is safe and
// what would stop it being so.
func (s *Service) RecordAgentRun(
	ctx context.Context,
	req *connect.Request[platformv1.RecordAgentRunRequest],
) (*connect.Response[platformv1.RecordAgentRunResponse], error) {
	if s.runs == nil {
		return nil, connect.NewError(connect.CodeUnimplemented,
			errors.New("this deployment records no agent runs"))
	}

	msg := req.Msg

	orgID, err := uuid.Parse(msg.GetOrgId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("org_id is not a uuid: %w", err))
	}

	// Required, and refused rather than defaulted. A run recorded without a
	// skill or model version is a row that answers "what produced this" with
	// "something", which is worse than no row because it looks like an answer.
	for name, value := range map[string]string{
		"skill":         msg.GetSkill(),
		"skill_version": msg.GetSkillVersion(),
		"model":         msg.GetModel(),
		"model_version": msg.GetModelVersion(),
	} {
		if value == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("%s is required: a run whose provenance is unknown is not a record", name))
		}
	}

	outcome, err := toOutcome(msg.GetOutcome())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	onBehalfOf, err := s.personFor(ctx, msg, orgID)
	if err != nil {
		return nil, err
	}

	// All three timestamps are required. Defaulting a missing one to now()
	// would put a plausible number in a column ENT-238 measures queue wait
	// with, and a wrong measurement is harder to notice than a missing one.
	if msg.GetQueuedAt() == nil || msg.GetStartedAt() == nil || msg.GetFinishedAt() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("queued_at, started_at and finished_at are all required"))
	}

	usage := msg.GetUsage()
	id, err := s.runs.RecordAgentRun(ctx, postgres.AgentRun{
		OrgID:             orgID,
		Skill:             msg.GetSkill(),
		SkillVersion:      msg.GetSkillVersion(),
		Model:             msg.GetModel(),
		ModelVersion:      msg.GetModelVersion(),
		OnBehalfOfUserID:  onBehalfOf,
		RequestJSON:       msg.GetRequestJson(),
		ToolCallsJSON:     msg.GetToolCallsJson(),
		CitationsJSON:     msg.GetCitationsJson(),
		Outcome:           outcome,
		OutcomeDetail:     msg.GetOutcomeDetail(),
		RefusalJSON:       msg.GetRefusalJson(),
		InputTokens:       usage.GetInputTokens(),
		CachedInputTokens: usage.GetCachedInputTokens(),
		OutputTokens:      usage.GetOutputTokens(),
		CostMicros:        usage.GetCostMicros(),
		QueuedAt:          msg.GetQueuedAt().AsTime(),
		StartedAt:         msg.GetStartedAt().AsTime(),
		FinishedAt:        msg.GetFinishedAt().AsTime(),
	})
	if err != nil {
		s.logger.Error("recording an agent run failed",
			"org", orgID, "skill", msg.GetSkill(), "error", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Logged at info even on success, because a run that produced a finding is
	// the thing an operator traces backwards from when a customer disputes one.
	s.logger.Info("recorded an agent run",
		"id", id, "org", orgID,
		"skill", msg.GetSkill(), "skill_version", msg.GetSkillVersion(),
		"model", msg.GetModel(), "outcome", outcome)

	return connect.NewResponse(&platformv1.RecordAgentRunResponse{Id: id.String()}), nil
}

// personFor decides who a run is recorded as having been for (ENT-230).
//
// # THE FIELD ALONE IS NOT EVIDENCE, AND USED TO BE TREATED AS THOUGH IT WERE
//
// `on_behalf_of_user_id` arrived with ENT-218 and was written straight to the
// column. That made the run record say "Ada asked for this" on the word of a
// caller holding `internal:intelligence`, which is a machine principal: nothing
// stopped it naming a person who had never heard of the run, in that person's
// own organisation's compliance record. The record is the thing a customer
// reads to decide whether to trust a finding, so a lie in it is worse than an
// absence.
//
// So a run that names a person must present the delegation that person's own
// session minted, and the two must agree. A run that presents none must name
// nobody, which is exactly what a scheduled sweep does.
//
// # WHY THE ORGANISATION IS CHECKED TOO
//
// A delegation is single-org. Recording a run against a different tenant from
// the one the person authorised would put a person's name on work in an
// organisation they may not even belong to, so the mismatch is refused rather
// than resolved in either direction.
//
// # AND WHY EVERY REFUSAL HERE IS PermissionDenied
//
// Not InvalidArgument, even though a mismatched pair is a caller bug. The
// distinction between "your delegation expired", "your delegation is for
// somebody else" and "there is no such delegation" is only useful to something
// probing which delegations are live, and the caller has presented a credential
// rather than proved a right to that difference.
func (s *Service) personFor(
	ctx context.Context, msg *platformv1.RecordAgentRunRequest, orgID uuid.UUID,
) (*uuid.UUID, error) {
	claimed := msg.GetOnBehalfOfUserId()
	presented := msg.GetDelegation()

	if presented == "" {
		if claimed != "" {
			return nil, connect.NewError(connect.CodePermissionDenied,
				errors.New("a run recorded for a person must present that person's delegation"))
		}
		// The ordinary scheduled sweep: for the organisation, for nobody in
		// particular.
		return nil, nil
	}

	if s.delegations == nil {
		// A deployment that cannot check a delegation refuses the run rather
		// than recording it unchecked. The alternative writes exactly the row
		// this function exists to prevent.
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("no usable delegation"))
	}

	grant, err := s.delegations.ResolveDelegation(ctx, presented)
	if err != nil {
		if errors.Is(err, delegation.ErrUnusable) {
			return nil, connect.NewError(connect.CodePermissionDenied,
				errors.New("no usable delegation"))
		}
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("resolving the delegation: %w", err))
	}

	if grant.OrgID != orgID.String() {
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("no usable delegation"))
	}
	if claimed != "" && claimed != grant.UserID {
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("no usable delegation"))
	}

	person, err := uuid.Parse(grant.UserID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("the delegation names no user: %w", err))
	}
	return &person, nil
}

// toOutcome maps the enum to the column's check constraint.
//
// UNSPECIFIED is refused rather than defaulted to failed. A run whose outcome
// nobody set is a caller bug, and guessing would put a definite-looking value
// in the column a customer reads to decide whether to trust a finding.
func toOutcome(outcome platformv1.AgentRunOutcome) (string, error) {
	switch outcome {
	case platformv1.AgentRunOutcome_AGENT_RUN_OUTCOME_SUCCEEDED:
		return "succeeded", nil
	case platformv1.AgentRunOutcome_AGENT_RUN_OUTCOME_REFUSED:
		return "refused", nil
	case platformv1.AgentRunOutcome_AGENT_RUN_OUTCOME_FAILED:
		return "failed", nil
	default:
		return "", errors.New("outcome is required: succeeded, refused or failed")
	}
}

func toCounts(counts postgres.Counts) *platformv1.IngestCounts {
	return &platformv1.IngestCounts{
		Documents:            int32(counts.Documents),
		Articles:             int32(counts.Articles),
		Paragraphs:           int32(counts.Paragraphs),
		Recitals:             int32(counts.Recitals),
		Annexes:              int32(counts.Annexes),
		AnnexItems:           int32(counts.AnnexItems),
		ArticleRecitalLinks:  int32(counts.ArticleRecitalLinks),
		Obligations:          int32(counts.Obligations),
		Guidelines:           int32(counts.Guidelines),
		EnforcementDecisions: int32(counts.Decisions),
	}
}
