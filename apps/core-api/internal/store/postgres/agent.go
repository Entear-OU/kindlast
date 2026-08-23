package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentStore is the producer's connection pool, separate from the application's.
//
// It connects as `kindlast_agent` rather than `kindlast_app`, and the split is
// a security boundary rather than a convenience. The application deliberately
// cannot create findings: the thing that serves requests should not be able to
// fabricate a claim about a customer's legal exposure. Running the sweeps needs
// a role that can, so it is a different role on a different pool, holding
// nothing the application holds and nothing on organisations, memberships or
// audit_log (00008).
//
// Kept in the same package as Store because the SET LOCAL that makes policies
// bite belongs here either way (§21.6), and because two packages emitting the
// same GUC would be two places to get it wrong.
type AgentStore struct {
	pool *pgxpool.Pool
	// logger carries the one thing a sweep has to be able to say out loud:
	// that it skipped a signal citing an obligation the corpus does not have.
	// The plpgsql said it with `raise log`, which goes to the Postgres log,
	// where nobody operating this product is looking.
	logger *slog.Logger
}

// NewAgent opens the producer pool.
//
// The DSN must name `kindlast_agent`. Naming the migrator or the superuser
// would work and would silently disable every policy this role is scoped by,
// which is the same trap New() warns about for the application.
func NewAgent(ctx context.Context, dsn string) (*AgentStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: opening the agent pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: pinging as the agent: %w", err)
	}
	return &AgentStore{pool: pool, logger: slog.Default()}, nil
}

// WithLogger returns the store logging to the given logger.
//
// Separate from NewAgent so the DSN stays the only thing a caller must supply,
// and so a test can capture what a sweep decided to skip.
func (a *AgentStore) WithLogger(logger *slog.Logger) *AgentStore {
	if logger == nil {
		return a
	}
	a.logger = logger
	return a
}

func (a *AgentStore) Close() { a.pool.Close() }

// Sweep is the result of running the producer over one organisation.
type Sweep struct {
	Signals  int32
	Findings int32
	RanAt    time.Time
}

// RunSweep runs the Watcher, then the Analyst, for one organisation.
//
// One transaction with one GUC. There is no `app.current_user_id` and that is
// deliberate: a sweep is started by the system, so there is no member to name,
// and the agent's policies are written to expect exactly that (00008). Setting
// a user here would be inventing an actor.
//
// The org id is parsed before it reaches SQL. Passing a malformed value through
// would surface as a cast error from inside a policy, which reads as a server
// fault rather than as a bad request.
func (a *AgentStore) RunSweep(ctx context.Context, orgID string, detectOnly bool) (Sweep, error) {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return Sweep{}, fmt.Errorf("%w: %q is not a uuid", ErrBadOrganisation, orgID)
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return Sweep{}, fmt.Errorf("postgres: beginning the sweep: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocal(ctx, tx, "app.current_org_id", org.String()); err != nil {
		return Sweep{}, err
	}

	var result Sweep

	// The detectors and the Analyst are Go as of ENT-259; see sweep.go. They
	// need no organisation parameter for the same reason the plpgsql did not:
	// the session already says, and the agent's policies are what make that
	// true rather than a predicate anybody has to remember.
	result.Signals, err = runWatcher(ctx, tx, a.logger)
	if err != nil {
		return Sweep{}, err
	}

	if !detectOnly {
		if result.Findings, err = runAnalyst(ctx, tx, a.logger); err != nil {
			return Sweep{}, err
		}
	}

	// Read inside the transaction, so the timestamp reported is the one the
	// sweep actually stamped rather than the moment the response was built.
	if err := tx.QueryRow(ctx, `select now()`).Scan(&result.RanAt); err != nil {
		return Sweep{}, fmt.Errorf("postgres: reading the sweep time: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Sweep{}, fmt.Errorf("postgres: committing the sweep: %w", err)
	}
	return result, nil
}

// ErrBadOrganisation is returned when the organisation header is not a uuid.
var ErrBadOrganisation = errors.New("postgres: the organisation is not a uuid")

// Expiry is the result of one snooze-expiry pass.
type Expiry struct {
	Reemerged int32
	RanAt     time.Time
}

// ExpireSnoozes brings back every finding whose deferral has run out, across
// every organisation (ENT-256, part two).
//
// NO GUC, AND THAT IS THE WHOLE DIFFERENCE FROM RunSweep ABOVE. A sweep names
// one organisation because "sweep everyone" is a blast radius somebody should
// have to write a loop for. This is the opposite case: a maintenance pass
// whose job is every organisation, that decides nothing (the person decided
// when they deferred), and for which there is no GUC value meaning "all of
// them". So `expire_snoozed_findings()` is SECURITY DEFINER as of 00034,
// executable by this role and no other, and bounded by its own body: one
// UPDATE on findings, snoozed rows whose date has passed, nothing else.
//
// Idempotent by construction. A finding comes back once; a second call finds
// nothing and reports zero, which is what makes it safe for a scheduler to
// retry without anybody reasoning about it.
func (a *AgentStore) ExpireSnoozes(ctx context.Context) (Expiry, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return Expiry{}, fmt.Errorf("postgres: beginning the snooze expiry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var result Expiry
	if err := tx.QueryRow(ctx, `select public.expire_snoozed_findings()`).Scan(&result.Reemerged); err != nil {
		return Expiry{}, fmt.Errorf("postgres: expiring snoozes: %w", err)
	}
	// Inside the transaction, so the timestamp reported is the one the
	// findings actually moved at, not the moment the response was built.
	if err := tx.QueryRow(ctx, `select now()`).Scan(&result.RanAt); err != nil {
		return Expiry{}, fmt.Errorf("postgres: reading the expiry time: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Expiry{}, fmt.Errorf("postgres: committing the snooze expiry: %w", err)
	}
	return result, nil
}
