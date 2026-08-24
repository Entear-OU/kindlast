package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/notify"
)

// The linked-channel half of ENT-263, migration 00044.
//
// Every method here runs in the caller's own tenant transaction, on
// `kindlast_app`, under policies that pin `user_id` to `app.current_user_id`.
// So a caller cannot read, link or unlink a colleague's chat even by naming
// their id, because there is no place to name one: none of these methods takes
// a user, exactly as the preferences methods do not. The authority is the
// policy, and the absence of the parameter is what stops a handler ever
// becoming the thing that refuses.

// LinkedChannel is one row as a caller may see it.
//
// Note what is not here: the verification code hash. It is written and
// compared inside this package and never leaves it, so no handler can return
// it and no proto message can carry it by accident.
type LinkedChannel struct {
	Kind     string
	ChatID   string
	Verified bool
	// PendingUntil is when an outstanding code expires. Zero when the channel
	// is verified or has no code, which the check constraint makes the same
	// two states rather than three.
	PendingUntil time.Time
	CreatedAt    time.Time
}

// ErrChatAlreadyLinked is what claiming a chat another member of the same
// organisation already holds comes back as.
//
// Recovered from the unique violation rather than from a pre-read, because a
// pre-read is a race: two people submitting the same chat id at the same
// moment both find it free and one of them then gets a constraint error the
// handler has never seen. The database decides, and this reads its answer.
var ErrChatAlreadyLinked = errors.New("postgres: that chat is already linked in this organisation")

// ErrNoPendingChannel is what verifying a channel with no outstanding code
// comes back as: it was never linked, it was unlinked, or it is already
// verified.
//
// One error for the three, deliberately, and it is the same reasoning
// `redeem_capability_token` uses. Distinguishing them tells a caller which
// chat ids exist in an organisation they may not be entitled to know about,
// and none of the three is actionable differently: the answer to all of them
// is "start again".
var ErrNoPendingChannel = errors.New("postgres: there is no chat awaiting verification")

// ErrTooManyVerificationAttempts is what a caller who has spent the row's
// budget gets. The pending code is destroyed by the same statement, so this is
// terminal for that code rather than a rate limit that lifts.
var ErrTooManyVerificationAttempts = errors.New("postgres: too many verification attempts")

// LinkedChannels lists the caller's own channels in the active organisation.
func (t *Tenant) LinkedChannels(ctx context.Context) ([]LinkedChannel, error) {
	rows, err := t.tx.Query(ctx, `
		select kind, chat_id, verified_at is not null,
		       coalesce(verification_expires_at, 'epoch'::timestamptz),
		       created_at
		  from notification_channels
		 where org_id = $1 and user_id = $2
		 order by kind
	`, t.orgID, t.userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing linked channels: %w", err)
	}
	defer rows.Close()

	var out []LinkedChannel
	for rows.Next() {
		var c LinkedChannel
		var pendingUntil time.Time
		if err := rows.Scan(&c.Kind, &c.ChatID, &c.Verified, &pendingUntil, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: reading a linked channel: %w", err)
		}
		if pendingUntil.Year() > 1970 {
			c.PendingUntil = pendingUntil
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: reading linked channels: %w", err)
	}
	return out, nil
}

// LinkTelegramChat records a claim and the code that will prove it.
//
// An upsert on `(org_id, user_id, kind)`, and the replacement is the point
// rather than a convenience. Relinking has to reset everything: a person who
// linked the wrong chat, or whose code expired, or who moved to a new device,
// must end up with exactly one row, one live code, and a fresh attempt budget.
// An insert that failed on the conflict would leave the old chat verified and
// still receiving.
//
// `verified_at` goes back to null with the new code, which is what makes this
// safe to call on an already verified channel: claiming a new chat stops
// delivery to the old one at the moment of the claim rather than at the moment
// the new one is proved. That direction is deliberate. Somebody relinking
// because they lost the old chat should not keep receiving compliance
// notifications there while they finish.
func (t *Tenant) LinkTelegramChat(ctx context.Context, chatID, codeHash string, expiresAt time.Time) error {
	_, err := t.tx.Exec(ctx, `
		insert into notification_channels
			(org_id, user_id, kind, chat_id,
			 verification_code_hash, verification_expires_at, verification_attempts)
		values ($1, $2, 'telegram', $3, $4, $5, 0)
		on conflict (org_id, user_id, kind) do update
		   set chat_id                 = excluded.chat_id,
		       verification_code_hash  = excluded.verification_code_hash,
		       verification_expires_at = excluded.verification_expires_at,
		       verification_attempts   = 0,
		       verified_at             = null
	`, t.orgID, t.userID, chatID, codeHash, expiresAt)
	if err != nil {
		var pgErr *pgconn.PgError
		const uniqueViolation = "23505"
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation &&
			pgErr.ConstraintName == "notification_channels_one_owner_per_chat" {
			return ErrChatAlreadyLinked
		}
		return fmt.Errorf("postgres: linking a Telegram chat: %w", err)
	}
	return nil
}

// VerifyTelegramChat spends one attempt against the pending code and, if it
// matches, marks the channel verified.
//
// # WHY THE ATTEMPT IS SPENT BEFORE THE COMPARISON
//
// Because the alternative leaks the budget. Incrementing only on a wrong
// answer means a caller who guesses right on the first try pays nothing, which
// is correct, and a caller whose request is cancelled mid-flight pays nothing
// either, which is a way to guess forever. The row is updated first and the
// comparison happens against what that update returned, so every call through
// this path costs one attempt whatever happens to it afterwards.
//
// # WHY THE ROW IS LOCKED
//
// Five attempts counted across concurrent requests is five, not five per
// connection. `for update` makes the read-then-write one thing.
func (t *Tenant) VerifyTelegramChat(ctx context.Context, code string) error {
	var (
		codeHash  string
		expiresAt time.Time
		attempts  int
	)
	err := t.tx.QueryRow(ctx, `
		select verification_code_hash, verification_expires_at, verification_attempts
		  from notification_channels
		 where org_id = $1 and user_id = $2 and kind = 'telegram'
		   and verification_code_hash is not null
		 for update
	`, t.orgID, t.userID).Scan(&codeHash, &expiresAt, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoPendingChannel
	}
	if err != nil {
		return fmt.Errorf("postgres: reading a pending channel: %w", err)
	}

	if attempts >= notify.MaxVerificationAttempts || !time.Now().Before(expiresAt) {
		// The code is destroyed rather than left to expire quietly, so a
		// spent or stale one cannot be tried again by a caller who kept it.
		// The chat id stays, so the person sees the claim they made and can
		// ask for a new code without retyping it.
		if err := t.clearPendingCode(ctx); err != nil {
			return err
		}
		if attempts >= notify.MaxVerificationAttempts {
			return ErrTooManyVerificationAttempts
		}
		return ErrNoPendingChannel
	}

	if _, err := t.tx.Exec(ctx, `
		update notification_channels
		   set verification_attempts = verification_attempts + 1
		 where org_id = $1 and user_id = $2 and kind = 'telegram'
	`, t.orgID, t.userID); err != nil {
		return fmt.Errorf("postgres: counting a verification attempt: %w", err)
	}

	if !notify.VerificationCodeMatches(code, codeHash) {
		return ErrNoPendingChannel
	}

	// The check constraint refuses a row holding both, so clearing the code
	// and stamping `verified_at` is one statement or it is a failed write.
	if _, err := t.tx.Exec(ctx, `
		update notification_channels
		   set verified_at             = now(),
		       verification_code_hash  = null,
		       verification_expires_at = null,
		       verification_attempts   = 0
		 where org_id = $1 and user_id = $2 and kind = 'telegram'
	`, t.orgID, t.userID); err != nil {
		return fmt.Errorf("postgres: verifying a Telegram chat: %w", err)
	}
	return nil
}

func (t *Tenant) clearPendingCode(ctx context.Context) error {
	if _, err := t.tx.Exec(ctx, `
		update notification_channels
		   set verification_code_hash  = null,
		       verification_expires_at = null
		 where org_id = $1 and user_id = $2 and kind = 'telegram'
	`, t.orgID, t.userID); err != nil {
		return fmt.Errorf("postgres: clearing a spent verification code: %w", err)
	}
	return nil
}

// UnlinkTelegramChat removes the caller's chat and reports whether there was
// one.
//
// A delete rather than a flag. The acceptance criterion is that after
// unlinking, messages go to the remaining channel or nowhere and never to the
// unlinked chat, and a soft delete would leave a row the dispatcher had to
// remember to filter: the shape of bug that is invisible until the day
// somebody writes a query that forgets.
//
// The preference is deliberately NOT reset to email here. Somebody who unlinks
// in order to relink a different chat has said what channel they want, and
// silently changing it back would undo a setting they never touched.
// notify.RouteFor already sends them by email in the meantime, with the reason
// recorded.
func (t *Tenant) UnlinkTelegramChat(ctx context.Context) (bool, error) {
	tag, err := t.tx.Exec(ctx, `
		delete from notification_channels
		 where org_id = $1 and user_id = $2 and kind = 'telegram'
	`, t.orgID, t.userID)
	if err != nil {
		return false, fmt.Errorf("postgres: unlinking a Telegram chat: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
