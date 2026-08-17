package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
)

// The corpus upserts (ENT-207).
//
// # WHY EVERY UPSERT CARRIES A `where` ON ITS `do update`
//
// Every corpus table has a `set_updated_at` BEFORE UPDATE trigger from 00001.
// It stamps `now()` on any update that fires, whatever the statement sets, so
// "only bump the timestamp when something changed" cannot be expressed as an
// assignment: the trigger wins. The `where` is the version that works, because
// it stops the UPDATE from firing at all.
//
// This is worth the trouble. Without it a scheduled re-ingest rewrites the
// modification time of the entire corpus on every run, and "what changed last
// Tuesday" stops being a question the database can answer. That matters more
// here than in most tables: the corpus is the reference a customer checks a
// finding against, so "when did this obligation last change" is something
// somebody will eventually need in an argument with a regulator.
//
// # AND WHY THE ID LOOKUPS ARE TWO STEPS
//
// A `where` that filters out the update means the statement touches no row,
// which means `returning` yields nothing. So an unchanged row needs a second
// query to learn its id. That is one extra round trip in the common case of a
// re-ingest changing nothing, which is the case where there is nothing else to
// do anyway.
//
// The alternative, an unconditional `do update set id = id` to force a row out
// of `returning`, would fire the trigger and lose the property above. Trading a
// timestamp everybody can see for a round trip nobody can was the wrong way
// round.

// upsertReturningID runs an upsert whose `returning id::text` may match no row,
// and falls back to the lookup when it does not.
func upsertReturningID(
	ctx context.Context, tx pgx.Tx,
	upsert string, upsertArgs []any,
	lookup string, lookupArgs []any,
	what string,
) (string, error) {
	var id string
	err := tx.QueryRow(ctx, upsert, upsertArgs...).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("postgres: writing %s: %w", what, err)
	}

	// No row came back, so the row exists and is unchanged. Not an error: it is
	// the ordinary outcome of re-ingesting a corpus nobody has edited.
	if err := tx.QueryRow(ctx, lookup, lookupArgs...).Scan(&id); err != nil {
		return "", fmt.Errorf("postgres: resolving %s after an unchanged upsert: %w", what, err)
	}
	return id, nil
}

func ingestDocument(
	ctx context.Context, tx pgx.Tx, doc *corpus.Document, counts Counts,
) (Counts, error) {
	documentID, err := upsertReturningID(ctx, tx,
		`insert into regulatory_documents
			(celex_number, title, short_title, version_date, official_url)
		 values ($1, $2, $3, $4::date, $5)
		 on conflict (celex_number) do update set
			title        = excluded.title,
			short_title  = excluded.short_title,
			version_date = excluded.version_date,
			official_url = excluded.official_url
		 where (regulatory_documents.title, regulatory_documents.short_title,
		        regulatory_documents.version_date, regulatory_documents.official_url)
		    is distinct from
		       (excluded.title, excluded.short_title,
		        excluded.version_date, excluded.official_url)
		 returning id::text`,
		[]any{doc.Celex, doc.Title, doc.ShortTitle, doc.VersionDate, doc.OfficialURL},
		`select id::text from regulatory_documents where celex_number = $1`,
		[]any{doc.Celex},
		fmt.Sprintf("document %s", doc.Celex),
	)
	if err != nil {
		return counts, err
	}
	counts.Documents++

	articleIDs := make(map[int]string, len(doc.Articles))
	for _, article := range doc.Articles {
		articleID, err := upsertReturningID(ctx, tx,
			`insert into regulatory_articles
				(document_id, article_number, heading, summary, effective_date)
			 values ($1, $2, $3, $4, $5::date)
			 on conflict (document_id, article_number) do update set
				heading        = excluded.heading,
				summary        = excluded.summary,
				effective_date = excluded.effective_date
			 where (regulatory_articles.heading, regulatory_articles.summary,
			        regulatory_articles.effective_date)
			    is distinct from
			       (excluded.heading, excluded.summary, excluded.effective_date)
			 returning id::text`,
			[]any{documentID, article.Number, article.Heading, article.Summary,
				nullableDate(article.EffectiveDate)},
			`select id::text from regulatory_articles
			  where document_id = $1 and article_number = $2`,
			[]any{documentID, article.Number},
			fmt.Sprintf("article %d", article.Number),
		)
		if err != nil {
			return counts, err
		}
		counts.Articles++
		articleIDs[article.Number] = articleID

		for _, paragraph := range article.Paragraphs {
			_, err := tx.Exec(ctx, `
				insert into regulatory_article_paragraphs
					(article_id, paragraph_label, summary, ordering)
				values ($1, $2, $3, $4)
				on conflict (article_id, paragraph_label) do update set
					summary  = excluded.summary,
					ordering = excluded.ordering
				where (regulatory_article_paragraphs.summary,
				       regulatory_article_paragraphs.ordering)
				   is distinct from (excluded.summary, excluded.ordering)
			`, articleID, paragraph.Label, paragraph.Summary, paragraph.Ordering)
			if err != nil {
				return counts, fmt.Errorf("postgres: writing article %d paragraph %s: %w",
					article.Number, paragraph.Label, err)
			}
			counts.Paragraphs++
		}
	}

	recitalIDs := make(map[int]string, len(doc.Recitals))
	for _, recital := range doc.Recitals {
		recitalID, err := upsertReturningID(ctx, tx,
			`insert into regulatory_recitals (document_id, recital_number, summary)
			 values ($1, $2, $3)
			 on conflict (document_id, recital_number) do update set
				summary = excluded.summary
			 where regulatory_recitals.summary is distinct from excluded.summary
			 returning id::text`,
			[]any{documentID, recital.Number, recital.Summary},
			`select id::text from regulatory_recitals
			  where document_id = $1 and recital_number = $2`,
			[]any{documentID, recital.Number},
			fmt.Sprintf("recital %d", recital.Number),
		)
		if err != nil {
			return counts, err
		}
		counts.Recitals++
		recitalIDs[recital.Number] = recitalID
	}

	for _, annex := range doc.Annexes {
		annexID, err := upsertReturningID(ctx, tx,
			`insert into regulatory_annexes
				(document_id, annex_label, heading, summary, effective_date)
			 values ($1, $2, $3, $4, $5::date)
			 on conflict (document_id, annex_label) do update set
				heading        = excluded.heading,
				summary        = excluded.summary,
				effective_date = excluded.effective_date
			 where (regulatory_annexes.heading, regulatory_annexes.summary,
			        regulatory_annexes.effective_date)
			    is distinct from
			       (excluded.heading, excluded.summary, excluded.effective_date)
			 returning id::text`,
			[]any{documentID, annex.Label, annex.Heading, annex.Summary,
				nullableDate(annex.EffectiveDate)},
			`select id::text from regulatory_annexes
			  where document_id = $1 and annex_label = $2`,
			[]any{documentID, annex.Label},
			fmt.Sprintf("annex %s", annex.Label),
		)
		if err != nil {
			return counts, err
		}
		counts.Annexes++

		for _, item := range annex.Items {
			_, err := tx.Exec(ctx, `
				insert into regulatory_annex_items
					(annex_id, item_label, heading, summary, effective_date, ordering)
				values ($1, $2, $3, $4, $5::date, $6)
				on conflict (annex_id, item_label) do update set
					heading        = excluded.heading,
					summary        = excluded.summary,
					effective_date = excluded.effective_date,
					ordering       = excluded.ordering
				where (regulatory_annex_items.heading, regulatory_annex_items.summary,
				       regulatory_annex_items.effective_date, regulatory_annex_items.ordering)
				   is distinct from
				      (excluded.heading, excluded.summary,
				       excluded.effective_date, excluded.ordering)
			`, annexID, item.Label, nullableText(item.Heading), item.Summary,
				nullableDate(item.EffectiveDate), item.Ordering)
			if err != nil {
				return counts, fmt.Errorf("postgres: writing annex %s item %s: %w",
					annex.Label, item.Label, err)
			}
			counts.AnnexItems++
		}
	}

	// Additive. The junction has no way to tell a link somebody deleted from a
	// link a smaller pack simply did not mention, so nothing here removes one.
	for _, link := range doc.ArticleRecitals {
		_, err := tx.Exec(ctx, `
			insert into regulatory_article_recitals (article_id, recital_id)
			values ($1, $2)
			on conflict (article_id, recital_id) do nothing
		`, articleIDs[link.ArticleNumber], recitalIDs[link.RecitalNumber])
		if err != nil {
			return counts, fmt.Errorf("postgres: linking article %d to recital %d: %w",
				link.ArticleNumber, link.RecitalNumber, err)
		}
		counts.ArticleRecitalLinks++
	}

	return counts, nil
}

func ingestObligations(
	ctx context.Context, tx pgx.Tx, obligations []corpus.Obligation, counts Counts,
) (Counts, error) {
	for _, o := range obligations {
		appliesWhen := o.AppliesWhenJSON
		if appliesWhen == "" {
			appliesWhen = "{}"
		}

		// `action_type` is NOT NULL with a default of `review` and a check
		// constraint naming four values (00007). An empty field in a pack means
		// "approving this records the decision and creates no register row",
		// which is what `review` says: it is a value rather than an absence, and
		// storing NULL would be refused by the column.
		actionType := o.ActionType
		if actionType == "" {
			actionType = "review"
		}

		_, err := tx.Exec(ctx, `
			insert into obligations (
				slug, title, summary,
				citation_celex, citation_kind, citation_article, citation_recital,
				citation_annex, citation_paragraph,
				applies_when, severity, due_within_days, recurrence,
				effective_date, topic_tags, action_type
			)
			values (
				$1, $2, $3,
				$4, $5, $6, $7, $8, $9,
				$10::jsonb, $11, $12, $13, $14::date, coalesce($15::text[], '{}'), $16
			)
			on conflict (slug) do update set
				title              = excluded.title,
				summary            = excluded.summary,
				citation_celex     = excluded.citation_celex,
				citation_kind      = excluded.citation_kind,
				citation_article   = excluded.citation_article,
				citation_recital   = excluded.citation_recital,
				citation_annex     = excluded.citation_annex,
				citation_paragraph = excluded.citation_paragraph,
				applies_when       = excluded.applies_when,
				severity           = excluded.severity,
				due_within_days    = excluded.due_within_days,
				recurrence         = excluded.recurrence,
				effective_date     = excluded.effective_date,
				topic_tags         = excluded.topic_tags,
				action_type        = excluded.action_type
			where (obligations.title, obligations.summary, obligations.citation_celex,
			       obligations.citation_kind, obligations.citation_article,
			       obligations.citation_recital, obligations.citation_annex,
			       obligations.citation_paragraph, obligations.applies_when,
			       obligations.severity, obligations.due_within_days,
			       obligations.recurrence, obligations.effective_date,
			       obligations.topic_tags, obligations.action_type)
			   is distinct from
			      (excluded.title, excluded.summary, excluded.citation_celex,
			       excluded.citation_kind, excluded.citation_article,
			       excluded.citation_recital, excluded.citation_annex,
			       excluded.citation_paragraph, excluded.applies_when,
			       excluded.severity, excluded.due_within_days,
			       excluded.recurrence, excluded.effective_date,
			       excluded.topic_tags, excluded.action_type)
		`,
			o.Slug, o.Title, o.Summary,
			o.Citation.Celex, o.Citation.Kind,
			nullableInt(o.Citation.ArticleNumber), nullableInt(o.Citation.RecitalNumber),
			nullableText(o.Citation.AnnexLabel), nullableText(o.Citation.ParagraphLabel),
			appliesWhen, o.Severity, nullableInt(o.DueWithinDays),
			nullableText(o.Recurrence), nullableDate(o.EffectiveDate),
			o.TopicTags, actionType,
		)
		if err != nil {
			return counts, fmt.Errorf("postgres: writing obligation %s: %w", o.Slug, err)
		}
		counts.Obligations++
	}
	return counts, nil
}

func ingestGuidelines(
	ctx context.Context, tx pgx.Tx, guidelines []corpus.Guideline, counts Counts,
) (Counts, error) {
	for _, g := range guidelines {
		_, err := tx.Exec(ctx, `
			insert into regulatory_guidelines
				(slug, publisher, title, adopted_date, version, source_url, topic_tags)
			values ($1, $2, $3, $4::date, $5, $6, coalesce($7::text[], '{}'))
			on conflict (slug) do update set
				publisher    = excluded.publisher,
				title        = excluded.title,
				adopted_date = excluded.adopted_date,
				version      = excluded.version,
				source_url   = excluded.source_url,
				topic_tags   = excluded.topic_tags
			where (regulatory_guidelines.publisher, regulatory_guidelines.title,
			       regulatory_guidelines.adopted_date, regulatory_guidelines.version,
			       regulatory_guidelines.source_url, regulatory_guidelines.topic_tags)
			   is distinct from
			      (excluded.publisher, excluded.title, excluded.adopted_date,
			       excluded.version, excluded.source_url, excluded.topic_tags)
		`, g.Slug, g.Publisher, g.Title, g.AdoptedDate,
			nullableText(g.Version), g.SourceURL, g.TopicTags)
		if err != nil {
			return counts, fmt.Errorf("postgres: writing guideline %s: %w", g.Slug, err)
		}
		counts.Guidelines++
	}
	return counts, nil
}

func ingestDecisions(
	ctx context.Context, tx pgx.Tx, decisions []corpus.EnforcementDecision, counts Counts,
) (Counts, error) {
	for _, d := range decisions {
		// A decision with no fine stores NULL rather than zero. A reprimand or a
		// processing ban is an outcome too, and reading "no fine" as zero would
		// make an enforcement register look like a list of free passes.
		var fine any
		if d.HasFine {
			fine = d.FineEUR
		}

		_, err := tx.Exec(ctx, `
			insert into regulatory_enforcement_decisions
				(slug, dpa, title, decision_date, fine_eur, summary, source_url,
				 gdpr_articles, topic_tags)
			values ($1, $2, $3, $4::date, $5, $6, $7,
			        coalesce($8::integer[], '{}'), coalesce($9::text[], '{}'))
			on conflict (slug) do update set
				dpa           = excluded.dpa,
				title         = excluded.title,
				decision_date = excluded.decision_date,
				fine_eur      = excluded.fine_eur,
				summary       = excluded.summary,
				source_url    = excluded.source_url,
				gdpr_articles = excluded.gdpr_articles,
				topic_tags    = excluded.topic_tags
			where (regulatory_enforcement_decisions.dpa,
			       regulatory_enforcement_decisions.title,
			       regulatory_enforcement_decisions.decision_date,
			       regulatory_enforcement_decisions.fine_eur,
			       regulatory_enforcement_decisions.summary,
			       regulatory_enforcement_decisions.source_url,
			       regulatory_enforcement_decisions.gdpr_articles,
			       regulatory_enforcement_decisions.topic_tags)
			   is distinct from
			      (excluded.dpa, excluded.title, excluded.decision_date,
			       excluded.fine_eur, excluded.summary, excluded.source_url,
			       excluded.gdpr_articles, excluded.topic_tags)
		`, d.Slug, d.DPA, d.Title, d.DecisionDate, fine, d.Summary, d.SourceURL,
			d.GDPRArticles, d.TopicTags)
		if err != nil {
			return counts, fmt.Errorf("postgres: writing enforcement decision %s: %w", d.Slug, err)
		}
		counts.Decisions++
	}
	return counts, nil
}

// nullableDate turns an empty date string into SQL NULL.
//
// An empty string cast to `date` is an error rather than a null, so a pack
// omitting an optional effective date would fail on `22007` naming the value
// rather than the field.
func nullableDate(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// nullableInt treats zero as absent.
//
// Safe for every column it is used on: article and recital numbers start at 1,
// and a `due_within_days` of zero would mean "due the moment it applies", which
// no obligation in the corpus says and which the schema would rather store as
// unset than as an accident of a missing proto field.
func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
