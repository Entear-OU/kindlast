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

	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
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

// Service implements platformv1connect.IngestServiceHandler.
type Service struct {
	store  Writer
	logger *slog.Logger
	now    func() time.Time
}

func New(store Writer, logger *slog.Logger) *Service {
	if logger == nil {
		// A discarding logger rather than a nil dereference. This handler's
		// caller is a schedule, so the one place it must not fail is while
		// reporting that something else failed.
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, logger: logger, now: time.Now}
}

func (s *Service) IngestCorpus(
	ctx context.Context,
	req *connect.Request[platformv1.IngestCorpusRequest],
) (*connect.Response[platformv1.IngestCorpusResponse], error) {
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
