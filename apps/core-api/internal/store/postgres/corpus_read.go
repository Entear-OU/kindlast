package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
)

// Reading the corpus, on the request's transaction (ENT-207).
//
// # NO ORG PREDICATE, AND HERE THAT IS NOT ABOUT RLS SUPPLYING ONE
//
// Everywhere else in this package the absence of `where org_id = $1` means RLS
// supplies it. Here it means there is nothing to supply: the corpus has no
// `org_id` because it is the same regulation for every customer, and ENT-192's
// ten public-read policies are `using (true)`.
//
// Worth stating because the shape is identical to a tenancy bug. A reviewer
// scanning this file for a missing predicate should find this comment rather
// than filing one.
//
// # THE CITATION LABEL AND URL ARE RENDERED IN GO, AND STILL ONLY ONCE
//
// This used to call `analyst_citation_label` and `analyst_citation_url`, and
// said so at length: rendering the label here as well as in the Analyst would
// be two implementations of "what is this citation called", and a finding
// saying `GDPR Art. 30` while this page says `32016R0679 Art. 30` is the
// inconsistency that makes a customer stop trusting both.
//
// That reasoning still holds and it is why the move was all or nothing.
// ENT-259 took the Analyst's conversion out of plpgsql, so leaving these two
// behind would have created exactly the divergence the old comment warned
// about. There is still one renderer, `corpus.Citation`'s Label and URL, and
// both callers use it. Its tests pin the output against what the plpgsql
// produced, read off a running database rather than derived from the body.

const obligationColumns = `
	o.slug,
	o.title,
	o.summary,
	o.severity,
	coalesce(o.recurrence, ''),
	coalesce(o.due_within_days, 0),
	coalesce(o.effective_date::text, ''),
	o.topic_tags,
	o.action_type,
	o.citation_kind,
	o.citation_celex,
	coalesce(o.citation_article, 0),
	coalesce(o.citation_recital, 0),
	coalesce(o.citation_annex, ''),
	coalesce(o.citation_paragraph, '')
`

func scanObligation(row pgx.Row) (corpus.StoredObligation, error) {
	var o corpus.StoredObligation
	err := row.Scan(
		&o.Slug, &o.Title, &o.Summary, &o.Severity, &o.Recurrence,
		&o.DueWithinDays, &o.EffectiveDate, &o.TopicTags, &o.ActionType,
		&o.Citation.Kind, &o.Citation.Celex,
		&o.Citation.ArticleNumber, &o.Citation.RecitalNumber,
		&o.Citation.AnnexLabel, &o.Citation.ParagraphLabel,
	)
	if err != nil {
		return o, err
	}
	// Derived from the parts the row carries, rather than selected beside them.
	// See the header for why this is the only place it happens.
	o.Label = o.Citation.Label()
	o.URL = o.Citation.URL()
	return o, nil
}

// Obligations returns every obligation this deployment checks against.
//
// Unpaged, deliberately. There are fifteen, they change when a regulation
// changes rather than when a customer does something, and somebody reading this
// list wants the whole thing rather than a window onto it. A keyset cursor here
// would be machinery for a page that fits on a screen.
func (t *Tenant) Obligations(ctx context.Context) ([]corpus.StoredObligation, error) {
	// Ordered by severity then slug, so the list reads worst-first rather than
	// alphabetically. `high` sorts after `low` and `medium` alphabetically,
	// which is the wrong way round, hence the explicit ranking.
	rows, err := t.tx.Query(ctx, fmt.Sprintf(`
		select %s
		from obligations o
		order by
			case o.severity when 'high' then 0 when 'medium' then 1 else 2 end,
			o.slug
	`, obligationColumns))
	if err != nil {
		return nil, fmt.Errorf("postgres: listing obligations: %w", err)
	}
	defer rows.Close()

	var obligations []corpus.StoredObligation
	for rows.Next() {
		obligation, err := scanObligation(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scanning an obligation: %w", err)
		}
		obligations = append(obligations, obligation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: reading obligations: %w", err)
	}
	return obligations, nil
}

// Obligation returns one obligation and the regulatory text behind it.
//
// Returns pgx.ErrNoRows for a slug that does not exist, which the handler
// renders as not found. There is no cross-tenant concern here to be careful
// about: the corpus is the same for everybody, so an unknown slug is simply
// unknown.
func (t *Tenant) Obligation(
	ctx context.Context, slug string,
) (corpus.StoredObligation, corpus.CitedText, error) {
	obligation, err := scanObligation(t.tx.QueryRow(ctx, fmt.Sprintf(`
		select %s from obligations o where o.slug = $1
	`, obligationColumns), slug))
	if err != nil {
		return corpus.StoredObligation{}, corpus.CitedText{}, err
	}

	// The text the citation points at. A left join in one query rather than a
	// switch in Go, because exactly one of the three branches can match: the
	// schema's `obligations_citation_matches_kind` constraint guarantees only
	// one target column is non-null.
	var cited corpus.CitedText
	err = t.tx.QueryRow(ctx, `
		select
			coalesce(a.summary, r.summary, x.summary, ''),
			coalesce(a.heading, x.heading, ''),
			coalesce(p.summary, '')
		from obligations o
		join regulatory_documents d on d.celex_number = o.citation_celex
		left join regulatory_articles a
			on a.document_id = d.id and a.article_number = o.citation_article
		left join regulatory_article_paragraphs p
			on p.article_id = a.id and p.paragraph_label = o.citation_paragraph
		left join regulatory_recitals r
			on r.document_id = d.id and r.recital_number = o.citation_recital
		left join regulatory_annexes x
			on x.document_id = d.id and x.annex_label = o.citation_annex
		where o.slug = $1
	`, slug).Scan(&cited.Summary, &cited.Heading, &cited.ParagraphSummary)
	if err != nil {
		// Not fatal, and this is the one branch worth being careful about. A
		// citation whose document is missing yields no row from the join above,
		// and the obligation itself is still worth showing: the page then says
		// the regulatory text is unavailable rather than 404ing an obligation
		// that plainly exists. Ingest refuses to create this state, so reaching
		// it means something wrote the corpus another way.
		if errors.Is(err, pgx.ErrNoRows) {
			return obligation, corpus.CitedText{}, nil
		}
		return corpus.StoredObligation{}, corpus.CitedText{},
			fmt.Errorf("postgres: reading the cited text for %s: %w", slug, err)
	}

	return obligation, cited, nil
}

// Documents returns the regulations this deployment holds, with their sizes.
//
// The counts are the point rather than decoration: a compliance product that
// cannot say which version of the law it is working from, and how much of it it
// has, is asking for trust it has not earned.
func (t *Tenant) Documents(ctx context.Context) ([]corpus.StoredDocument, error) {
	rows, err := t.tx.Query(ctx, `
		select
			d.celex_number,
			d.title,
			d.short_title,
			d.version_date::text,
			d.official_url,
			(select count(*) from regulatory_articles a where a.document_id = d.id),
			(select count(*) from regulatory_recitals r where r.document_id = d.id),
			(select count(*) from regulatory_annexes x where x.document_id = d.id)
		from regulatory_documents d
		order by d.short_title
	`)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing regulatory documents: %w", err)
	}
	defer rows.Close()

	var documents []corpus.StoredDocument
	for rows.Next() {
		var d corpus.StoredDocument
		if err := rows.Scan(
			&d.Celex, &d.Title, &d.ShortTitle, &d.VersionDate, &d.OfficialURL,
			&d.ArticleCount, &d.RecitalCount, &d.AnnexCount,
		); err != nil {
			return nil, fmt.Errorf("postgres: scanning a regulatory document: %w", err)
		}
		documents = append(documents, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: reading regulatory documents: %w", err)
	}
	return documents, nil
}
