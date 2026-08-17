package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
)

// CorpusStore is the ingest path's connection pool (ENT-207).
//
// Its own pool on its own role, `kindlast_ingest`, which is NOSUPERUSER,
// NOBYPASSRLS, owns nothing, and holds grants on the ten regulatory tables and
// no others. See 00018's header for why this is a sixth role rather than the
// migrator 00002's comment named: the migrator bypasses RLS and owns the
// schema, so ingesting as it would mean the process writing the corpus could
// also rewrite any tenant's findings.
type CorpusStore struct {
	pool *pgxpool.Pool
}

// NewCorpus opens the ingest pool.
//
// The DSN must name `kindlast_ingest`. Naming the migrator would work and would
// quietly hand the corpus writer the whole database, which is the trap §14.1
// spends a paragraph on and the one an operation that runs on a schedule is
// least likely to surface.
func NewCorpus(ctx context.Context, dsn string) (*CorpusStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: opening the corpus pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: pinging as the ingest role: %w", err)
	}
	return &CorpusStore{pool: pool}, nil
}

func (c *CorpusStore) Close() { c.pool.Close() }

// Counts is what a pack wrote, by table.
//
// An upsert counts once whether it inserted or updated. The useful question
// after a re-ingest is "did the pack land", answered by the total matching the
// pack; a split between inserted and updated changes every time somebody edits
// a summary and tells nobody anything.
type Counts struct {
	Documents           int
	Articles            int
	Paragraphs          int
	Recitals            int
	Annexes             int
	AnnexItems          int
	ArticleRecitalLinks int
	Obligations         int
	Guidelines          int
	Decisions           int
}

// Ingest writes a pack, or writes none of it.
//
// # ONE TRANSACTION, AND THE CITATION CHECK IS INSIDE IT
//
// A pack that failed halfway would leave a corpus that is partly the new
// snapshot and partly the old, which is the one state nobody can reason about:
// a finding citing Article 30 would resolve and the recital beside it would
// not, with nothing to say which snapshot each came from.
//
// The citation check runs after the document is written and before the commit,
// which is the only ordering that works. Before, and a pack carrying both a
// regulation and the obligations derived from it would refuse itself, because
// the articles it cites are in the same pack and not yet stored. After the
// commit, and a dangling citation is already public.
//
// # EVERY WRITE IS AN UPSERT ON A NATURAL KEY
//
// A document on its CELEX, an article on `(document, number)`, an obligation on
// its slug. Running the same pack twice produces no duplicates and no drift,
// which matters because the caller is a schedule: §20.3 makes this a singleton
// once Temporal lands, and a schedule that double-wrote on retry would compound
// silently.
//
// `updated_at` is only bumped when something actually changed, so a re-ingest
// of an unchanged pack leaves the timestamps alone. Otherwise every scheduled
// run would rewrite the modification time of the entire corpus and "what
// changed last Tuesday" would stop being answerable.
//
// # NOTHING IS DELETED, EVER
//
// The role holds no delete grant. A row dropped from a later snapshot stays,
// because a finding cites an obligation and an obligation cites an article, and
// removing either out from under a stored finding turns a claim a customer
// could check into a dangling reference, retroactively.
func (c *CorpusStore) Ingest(
	ctx context.Context, pack corpus.Pack, dryRun bool,
) (Counts, []string, error) {
	var counts Counts

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return counts, nil, fmt.Errorf("postgres: beginning a corpus transaction: %w", err)
	}
	// A dry run rolls back through this too, so "validate without writing" is
	// the same code path rather than a second implementation that could drift
	// from the one that writes.
	defer func() { _ = tx.Rollback(ctx) }()

	if pack.Document != nil {
		if counts, err = ingestDocument(ctx, tx, pack.Document, counts); err != nil {
			return Counts{}, nil, err
		}
	}

	if counts, err = ingestGuidelines(ctx, tx, pack.Guidelines, counts); err != nil {
		return Counts{}, nil, err
	}
	if counts, err = ingestDecisions(ctx, tx, pack.EnforcementDecisions, counts); err != nil {
		return Counts{}, nil, err
	}

	// Now the citations, against what this transaction can see: the corpus as
	// it was, plus whatever this pack has just written.
	unresolved, err := unresolvedCitations(ctx, tx, pack.Obligations)
	if err != nil {
		return Counts{}, nil, err
	}
	if len(unresolved) > 0 {
		// Refused rather than skipping the offending obligations. A pack that
		// half-landed is a corpus somebody has to reconcile by hand, and an
		// obligation pointing at an article that is not there would surface to a
		// customer as a finding whose "check this against the law" goes nowhere.
		return Counts{}, unresolved, nil
	}

	if counts, err = ingestObligations(ctx, tx, pack.Obligations, counts); err != nil {
		return Counts{}, nil, err
	}

	if dryRun {
		return counts, nil, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return Counts{}, nil, fmt.Errorf("postgres: committing the corpus: %w", err)
	}
	return counts, nil, nil
}

// unresolvedCitations returns the citations that point at nothing.
//
// Read inside the ingest transaction, so a pack carrying both a regulation and
// the obligations derived from it resolves against the articles it has just
// written. Reading on another connection would refuse every self-contained
// pack, because those rows are not committed yet.
func unresolvedCitations(
	ctx context.Context, tx pgx.Tx, obligations []corpus.Obligation,
) ([]string, error) {
	var unresolved []string

	for _, obligation := range obligations {
		citation := obligation.Citation

		var exists bool
		var err error

		switch citation.Kind {
		case corpus.KindArticle:
			if citation.ParagraphLabel != "" {
				// A paragraph label that does not exist is as dangling as an
				// article number that does not, and it is the likelier mistake:
				// paragraph labels are hand-written.
				err = tx.QueryRow(ctx, `
					select exists (
						select 1
						from regulatory_article_paragraphs p
						join regulatory_articles a on a.id = p.article_id
						join regulatory_documents d on d.id = a.document_id
						where d.celex_number = $1
						  and a.article_number = $2
						  and p.paragraph_label = $3
					)
				`, citation.Celex, citation.ArticleNumber, citation.ParagraphLabel).Scan(&exists)
			} else {
				err = tx.QueryRow(ctx, `
					select exists (
						select 1
						from regulatory_articles a
						join regulatory_documents d on d.id = a.document_id
						where d.celex_number = $1 and a.article_number = $2
					)
				`, citation.Celex, citation.ArticleNumber).Scan(&exists)
			}

		case corpus.KindRecital:
			err = tx.QueryRow(ctx, `
				select exists (
					select 1
					from regulatory_recitals r
					join regulatory_documents d on d.id = r.document_id
					where d.celex_number = $1 and r.recital_number = $2
				)
			`, citation.Celex, citation.RecitalNumber).Scan(&exists)

		case corpus.KindAnnex:
			err = tx.QueryRow(ctx, `
				select exists (
					select 1
					from regulatory_annexes x
					join regulatory_documents d on d.id = x.document_id
					where d.celex_number = $1 and x.annex_label = $2
				)
			`, citation.Celex, citation.AnnexLabel).Scan(&exists)

		default:
			// Shape validation runs before this and rejects an unknown kind, so
			// reaching here means the two disagree. Treated as unresolved rather
			// than ignored: an obligation whose citation nothing understands
			// must not be stored.
			unresolved = append(unresolved, fmt.Sprintf(
				"%s cites %s", obligation.Slug, citation.Target()))
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("postgres: resolving the citation for %s: %w",
				obligation.Slug, err)
		}
		if !exists {
			unresolved = append(unresolved, fmt.Sprintf(
				"%s cites %s, which is not in the corpus", obligation.Slug, citation.Target()))
		}
	}

	return unresolved, nil
}
