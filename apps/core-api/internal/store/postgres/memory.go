package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
)

// Organisation memory, on the request's transaction (ENT-228, §26.5).
//
// # NO ORG PREDICATE ANYWHERE IN THIS FILE, AND HERE THAT MEANS RLS
//
// Unlike `corpus_read.go`, where the absence means the table has no tenancy at
// all, every query below runs with both GUCs set and the policies supply
// `org_id = current_setting(...)`. A `where org_id = $1` added here would be a
// second, weaker copy of a check the database already makes, and the day the
// two disagree it is the weaker one somebody trusts.
//
// # CORRECTION IS CLOSE-THEN-INSERT, NEVER UPDATE
//
// It cannot be anything else. `kindlast_app` holds `update (valid_to)` and not
// `update (value)`, so an in-place rewrite is refused by the database rather
// than avoided by convention here. This file is written so that shape is
// obvious to whoever reads it next and does not look like an oversight.

const profileFactColumns = `
	key,
	value::text,
	source,
	coalesce(evidence_id::text, ''),
	valid_from,
	valid_to,
	coalesce(recorded_by::text, ''),
	coalesce(note, '')
`

func scanFacts(rows pgx.Rows) ([]memory.Fact, error) {
	defer rows.Close()

	facts := make([]memory.Fact, 0, 16)
	for rows.Next() {
		var f memory.Fact
		if err := rows.Scan(
			&f.Key,
			&f.ValueJSON,
			&f.Source,
			&f.EvidenceID,
			&f.ValidFrom,
			&f.ValidTo,
			&f.RecordedBy,
			&f.Note,
		); err != nil {
			return nil, fmt.Errorf("scanning a profile fact: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

// ProfileFacts returns what the organisation is currently believed to be.
//
// Open values only. History is a separate question with its own RPC, and
// carrying it here would make the common read return every superseded value
// the organisation has ever had.
func (t *Tenant) ProfileFacts(ctx context.Context) ([]memory.Fact, error) {
	rows, err := t.tx.Query(ctx, `
		select `+profileFactColumns+`
		  from public.org_profile_facts
		 where valid_to is null
		 order by key`)
	if err != nil {
		return nil, fmt.Errorf("listing profile facts: %w", err)
	}
	return scanFacts(rows)
}

// FactHistory returns every value one fact has ever had, newest first.
//
// The read that makes correction meaningful: without it, correcting a fact is
// indistinguishable from our having always thought the new thing, and what
// somebody checking an old finding is asking is exactly what we believed then.
func (t *Tenant) FactHistory(ctx context.Context, key string) ([]memory.Fact, error) {
	rows, err := t.tx.Query(ctx, `
		select `+profileFactColumns+`
		  from public.org_profile_facts
		 where key = $1
		 order by valid_from desc`, key)
	if err != nil {
		return nil, fmt.Errorf("reading the history of %q: %w", key, err)
	}
	return scanFacts(rows)
}

// CorrectFact records a new value and closes the previous one.
//
// Returns the stored fact and whether anything changed.
//
// # CORRECTING A FACT TO WHAT IT ALREADY SAYS WRITES NOTHING
//
// Not an optimisation. The history is a document a customer reads to see how
// our picture of them moved, and a console that re-submits a form would
// otherwise fill it with "changed from yes to yes". A history whose rows are
// mostly noise is one nobody scrolls, which costs precisely the question it
// exists to answer.
//
// The comparison is on the stored jsonb rather than on the text, so
// `{"a":1,"b":2}` and `{"b":2,"a":1}` are one value. Comparing text would make
// key order a change.
//
// # ONE TRANSACTION, AND THE DATABASE ENFORCES THE PAIRING ANYWAY
//
// Both statements run on the request's transaction, so a crash between them
// leaves neither. And were this ever rewritten to skip the close, the partial
// unique index refuses the insert: the invariant does not rest on this
// function being right.
func (t *Tenant) CorrectFact(
	ctx context.Context,
	key, valueJSON, source, note string,
) (memory.Fact, bool, error) {
	if err := memory.ValidateValue(key, valueJSON); err != nil {
		return memory.Fact{}, false, err
	}
	if err := memory.ValidateSource(source); err != nil {
		return memory.Fact{}, false, err
	}

	// ONE INSTANT, TAKEN ONCE, AND `clock_timestamp()` RATHER THAN `now()`.
	//
	// This cost an afternoon and the test that found it is in this package.
	// `now()` is the TRANSACTION timestamp: it is the same value for every
	// statement in a transaction. So closing with `now()` and opening with
	// `now()` produced two rows with identical `valid_from`, a zero-length
	// interval for the superseded value, and a history whose order was
	// undefined because the column it orders by tied.
	//
	// Nothing about that is visible in a single-correction request, which is
	// why it survived being written. It appears the moment two corrections
	// share a transaction, and it would appear in production as a history that
	// sometimes lists itself backwards.
	//
	// `clock_timestamp()` moves within a transaction, so the instant is real.
	// Taken once and passed to both statements so the intervals meet exactly:
	// a gap, however small, is an instant where an as-of query finds no value
	// and reports that we believed nothing.
	var at time.Time
	if err := t.tx.QueryRow(ctx, `select clock_timestamp()`).Scan(&at); err != nil {
		return memory.Fact{}, false, fmt.Errorf("reading the clock: %w", err)
	}

	var currentID string
	var same bool
	err := t.tx.QueryRow(ctx, `
		select id::text, value = $2::jsonb
		  from public.org_profile_facts
		 where key = $1 and valid_to is null`, key, valueJSON).Scan(&currentID, &same)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No current value. The first assertion of a fact corrects nothing and
		// needs no close.
	case err != nil:
		return memory.Fact{}, false, fmt.Errorf("reading the current value of %q: %w", key, err)
	case same:
		stored, err := t.currentFact(ctx, key)
		return stored, false, err
	default:
		if _, err := t.tx.Exec(ctx, `
			update public.org_profile_facts
			   set valid_to = $2
			 where id = $1::uuid`, currentID, at); err != nil {
			return memory.Fact{}, false, fmt.Errorf("closing the previous value of %q: %w", key, err)
		}
	}

	// `recorded_by` comes from the verified subject on the transaction, never
	// from the request. A caller naming which human corrected a fact is a
	// caller that can attribute a change to somebody else.
	if _, err := t.tx.Exec(ctx, `
		insert into public.org_profile_facts
			(org_id, key, value, source, recorded_by, note, valid_from)
		values (
			(select current_setting('app.current_org_id')::uuid),
			$1, $2::jsonb, $3, nullif($4, '')::uuid, nullif($5, ''), $6
		)`, key, valueJSON, source, t.userID, note, at); err != nil {
		return memory.Fact{}, false, fmt.Errorf("recording the new value of %q: %w", key, err)
	}

	stored, err := t.currentFact(ctx, key)
	return stored, true, err
}

func (t *Tenant) currentFact(ctx context.Context, key string) (memory.Fact, error) {
	rows, err := t.tx.Query(ctx, `
		select `+profileFactColumns+`
		  from public.org_profile_facts
		 where key = $1 and valid_to is null`, key)
	if err != nil {
		return memory.Fact{}, fmt.Errorf("reading back %q: %w", key, err)
	}
	facts, err := scanFacts(rows)
	if err != nil {
		return memory.Fact{}, err
	}
	if len(facts) == 0 {
		// Reachable only if the row vanished between the write and this read
		// inside one transaction, which it cannot. An error rather than a zero
		// value so that if it ever does happen, it says so.
		return memory.Fact{}, fmt.Errorf("no open value for %q after writing one", key)
	}
	return facts[0], nil
}

// Observations returns evidence for the organisation, newest first.
//
// Keyset over `observed_at`, matching how `audit_log` and `agent_runs` are
// read. An offset would drift as observations arrive during paging, which on
// an append-only table is the normal case rather than a rare one.
func (t *Tenant) Observations(
	ctx context.Context,
	pageSize int32,
	before time.Time,
) ([]memory.Observation, error) {
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}

	var cursor any
	if !before.IsZero() {
		cursor = before
	}

	rows, err := t.tx.Query(ctx, `
		select id::text,
		       source,
		       kind,
		       coalesce(connection_id::text, ''),
		       observed_at,
		       fetched_at,
		       body::text,
		       coalesce(superseded_by::text, '')
		  from public.org_evidence
		 where ($1::timestamptz is null or observed_at < $1)
		 order by observed_at desc
		 limit $2`, cursor, pageSize)
	if err != nil {
		return nil, fmt.Errorf("listing evidence: %w", err)
	}
	defer rows.Close()

	out := make([]memory.Observation, 0, pageSize)
	for rows.Next() {
		var o memory.Observation
		if err := rows.Scan(
			&o.ID,
			&o.Source,
			&o.Kind,
			&o.ConnectionID,
			&o.ObservedAt,
			&o.FetchedAt,
			&o.BodyJSON,
			&o.SupersededBy,
		); err != nil {
			return nil, fmt.Errorf("scanning an observation: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
