// Package corpus serves CorpusService: the law, read (ENT-207).
//
// # THE SURFACE THE PRODUCT'S CLAIM RESTS ON
//
// A finding says an organisation has an obligation and cites the law it comes
// from. The reason anybody should believe that is that they can go and look,
// and this is the looking. AGENTS.md opens by saying anything fabricating a
// citation is worse than nothing, because the value is that a human can check
// the claim against the law; this service is the half of that sentence that has
// been missing.
//
// # READ ONLY, AND THE WRITE IS SOMEWHERE A PERSON CANNOT REACH
//
// No write RPC. Writing the corpus is IngestService on the platform surface,
// behind `internal:ingest`, on a Postgres role that holds grants on the ten
// regulatory tables and cannot read a finding. A console request that could
// change the law would make this service a formality.
//
// # NOT TENANT DATA
//
// The corpus has no `org_id`. Every member of every organisation reads the same
// rows, through ENT-192's public-read policies. The organisation header is
// still required, which keeps one shape for every request on this surface and
// means a caller has to be a member of something: the law is public, this
// deployment's copy of it is not a public API.
package corpus

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// reading is what these handlers need of the request's transaction, declared
// where it is used (§21.6).
type reading interface {
	Obligations(ctx context.Context) ([]domain.StoredObligation, error)
	Obligation(ctx context.Context, slug string) (domain.StoredObligation, domain.CitedText, error)
	Documents(ctx context.Context) ([]domain.StoredDocument, error)
}

// Service implements corev1connect.CorpusServiceHandler.
//
// No role gate. Every member sees the corpus, viewers included: it is the same
// law for every customer and it is what a finding's citation points at. A
// product whose users cannot look up the obligation they are being told about
// is asking for trust it has not earned.
type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) ListObligations(
	ctx context.Context,
	_ *connect.Request[corev1.ListObligationsRequest],
) (*connect.Response[corev1.ListObligationsResponse], error) {
	store, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	obligations, err := store.Obligations(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response := &corev1.ListObligationsResponse{}
	for _, obligation := range obligations {
		response.Obligations = append(response.Obligations, toProto(obligation))
	}
	return connect.NewResponse(response), nil
}

func (s *Service) GetObligation(
	ctx context.Context,
	req *connect.Request[corev1.GetObligationRequest],
) (*connect.Response[corev1.GetObligationResponse], error) {
	store, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	slug := req.Msg.GetSlug()
	if slug == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("no obligation named"))
	}

	obligation, cited, err := store.Obligation(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		// Plainly not found, with none of the care the tenant surfaces need
		// about telling "does not exist" from "not yours". The corpus is the
		// same for everybody, so an unknown slug is simply unknown and saying
		// so leaks nothing.
		return nil, connect.NewError(connect.CodeNotFound,
			errors.New("no such obligation"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.GetObligationResponse{
		Obligation:            toProto(obligation),
		CitedSummary:          cited.Summary,
		CitedHeading:          cited.Heading,
		CitedParagraphSummary: cited.ParagraphSummary,
	}), nil
}

func (s *Service) ListDocuments(
	ctx context.Context,
	_ *connect.Request[corev1.ListDocumentsRequest],
) (*connect.Response[corev1.ListDocumentsResponse], error) {
	store, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	documents, err := store.Documents(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response := &corev1.ListDocumentsResponse{}
	for _, d := range documents {
		response.Documents = append(response.Documents, &corev1.RegulatoryDocumentSummary{
			CelexNumber:  d.Celex,
			Title:        d.Title,
			ShortTitle:   d.ShortTitle,
			VersionDate:  d.VersionDate,
			OfficialUrl:  d.OfficialURL,
			ArticleCount: int32(d.ArticleCount),
			RecitalCount: int32(d.RecitalCount),
			AnnexCount:   int32(d.AnnexCount),
		})
	}
	return connect.NewResponse(response), nil
}

func tenantFrom(ctx context.Context) (reading, error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	tenant, ok := interceptor.TenantFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no tenant transaction"))
	}
	store, ok := tenant.(reading)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("the tenant transaction cannot read the corpus"))
	}
	return store, nil
}

func toProto(o domain.StoredObligation) *corev1.CorpusObligation {
	return &corev1.CorpusObligation{
		Slug:          o.Slug,
		Title:         o.Title,
		Summary:       o.Summary,
		Severity:      o.Severity,
		Recurrence:    o.Recurrence,
		DueWithinDays: int32(o.DueWithinDays),
		EffectiveDate: o.EffectiveDate,
		TopicTags:     o.TopicTags,
		ActionType:    o.ActionType,
		Citation: &corev1.Citation{
			// The same message FindingsService returns, so a console renders one
			// provision one way wherever it appears.
			ObligationSlug: o.Slug,
			Title:          o.Title,
			Celex:          o.Citation.Celex,
			Kind:           o.Citation.Kind,
			Article:        int32(o.Citation.ArticleNumber),
			Recital:        int32(o.Citation.RecitalNumber),
			Annex:          o.Citation.AnnexLabel,
			Paragraph:      o.Citation.ParagraphLabel,
			Label:          o.Label,
			Url:            o.URL,
		},
	}
}
