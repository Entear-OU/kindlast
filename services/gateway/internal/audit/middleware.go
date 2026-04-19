package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
)

// Logger handles audit logging for artifact operations
type Logger struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewLogger creates a new audit logger
func NewLogger(db *sql.DB, logger *slog.Logger) *Logger {
	return &Logger{
		db:     db,
		logger: logger,
	}
}

// Entry represents an audit log entry to be written
type Entry struct {
	ArtifactID    string
	UserID        string
	Action        string
	PreviousState json.RawMessage
	NewState      json.RawMessage
	Metadata      Metadata
}

// Metadata stores additional information about audit entries
type Metadata struct {
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Log writes an audit entry to the database
func (l *Logger) Log(ctx context.Context, entry Entry) error {
	metadataJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		l.logger.Error("failed to marshal audit metadata", slog.String("error", err.Error()))
		metadataJSON = []byte("{}")
	}

	_, err = l.db.ExecContext(ctx, `
		INSERT INTO artifact_audit_log
			(artifact_id, user_id, action, previous_state, new_state, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, entry.ArtifactID, entry.UserID, entry.Action, entry.PreviousState, entry.NewState, metadataJSON)

	if err != nil {
		l.logger.Error("failed to write audit log",
			slog.String("error", err.Error()),
			slog.String("artifact_id", entry.ArtifactID),
			slog.String("action", entry.Action),
		)
		return err
	}

	l.logger.Info("audit entry logged",
		slog.String("artifact_id", entry.ArtifactID),
		slog.String("user_id", entry.UserID),
		slog.String("action", entry.Action),
	)

	return nil
}

// LogGenerated logs artifact generation
func (l *Logger) LogGenerated(ctx context.Context, artifactID, userID string, artifact *models.Artifact, r *http.Request) error {
	newState, _ := json.Marshal(artifact)
	return l.Log(ctx, Entry{
		ArtifactID: artifactID,
		UserID:     userID,
		Action:     models.AuditActionGenerated,
		NewState:   newState,
		Metadata:   l.extractMetadata(r),
	})
}

// LogEdited logs artifact editing
func (l *Logger) LogEdited(ctx context.Context, artifactID, userID string, previousContent, newContent json.RawMessage, r *http.Request) error {
	return l.Log(ctx, Entry{
		ArtifactID:    artifactID,
		UserID:        userID,
		Action:        models.AuditActionEdited,
		PreviousState: previousContent,
		NewState:      newContent,
		Metadata:      l.extractMetadata(r),
	})
}

// LogStatusChanged logs artifact status changes
func (l *Logger) LogStatusChanged(ctx context.Context, artifactID, userID, previousStatus, newStatus, reason string, r *http.Request) error {
	previousState, _ := json.Marshal(map[string]string{"status": previousStatus})
	newState, _ := json.Marshal(map[string]string{"status": newStatus})

	metadata := l.extractMetadata(r)
	metadata.Reason = reason

	return l.Log(ctx, Entry{
		ArtifactID:    artifactID,
		UserID:        userID,
		Action:        models.AuditActionStatusChanged,
		PreviousState: previousState,
		NewState:      newState,
		Metadata:      metadata,
	})
}

// LogExported logs artifact export
func (l *Logger) LogExported(ctx context.Context, artifactID, userID, format string, r *http.Request) error {
	newState, _ := json.Marshal(map[string]string{"format": format})
	return l.Log(ctx, Entry{
		ArtifactID: artifactID,
		UserID:     userID,
		Action:     models.AuditActionExported,
		NewState:   newState,
		Metadata:   l.extractMetadata(r),
	})
}

// LogDeleted logs artifact deletion
func (l *Logger) LogDeleted(ctx context.Context, artifactID, userID string, artifact *models.Artifact, r *http.Request) error {
	previousState, _ := json.Marshal(artifact)
	return l.Log(ctx, Entry{
		ArtifactID:    artifactID,
		UserID:        userID,
		Action:        models.AuditActionDeleted,
		PreviousState: previousState,
		Metadata:      l.extractMetadata(r),
	})
}

// extractMetadata extracts IP and user agent from the request
func (l *Logger) extractMetadata(r *http.Request) Metadata {
	if r == nil {
		return Metadata{}
	}

	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}

	return Metadata{
		IP:        ip,
		UserAgent: r.UserAgent(),
	}
}

// GetAuditEntries retrieves audit entries with optional filters
func (l *Logger) GetAuditEntries(ctx context.Context, opts AuditQueryOptions) ([]models.ArtifactAuditEntry, int, error) {
	// Build query with filters
	query := `
		SELECT id, artifact_id, user_id, action, previous_state, new_state, metadata, created_at
		FROM artifact_audit_log
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM artifact_audit_log WHERE 1=1`

	args := []interface{}{}
	argIndex := 1

	if opts.ArtifactID != "" {
		filter := " AND artifact_id = $" + itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, opts.ArtifactID)
		argIndex++
	}

	if opts.UserID != "" {
		filter := " AND user_id = $" + itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, opts.UserID)
		argIndex++
	}

	if opts.ClientID != "" {
		filter := " AND artifact_id IN (SELECT id FROM artifacts WHERE client_id = $" + itoa(argIndex) + ")"
		query += filter
		countQuery += filter
		args = append(args, opts.ClientID)
		argIndex++
	}

	if opts.Action != "" {
		filter := " AND action = $" + itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, opts.Action)
		argIndex++
	}

	if !opts.StartDate.IsZero() {
		filter := " AND created_at >= $" + itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, opts.StartDate)
		argIndex++
	}

	if !opts.EndDate.IsZero() {
		filter := " AND created_at <= $" + itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, opts.EndDate)
		argIndex++
	}

	// Get total count
	var total int
	err := l.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Add pagination
	query += " ORDER BY created_at DESC"
	query += " LIMIT $" + itoa(argIndex) + " OFFSET $" + itoa(argIndex+1)
	args = append(args, opts.PageSize, (opts.Page-1)*opts.PageSize)

	// Execute query
	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries := make([]models.ArtifactAuditEntry, 0)
	for rows.Next() {
		var entry models.ArtifactAuditEntry
		err := rows.Scan(
			&entry.ID, &entry.ArtifactID, &entry.UserID, &entry.Action,
			&entry.PreviousState, &entry.NewState, &entry.Metadata, &entry.CreatedAt,
		)
		if err != nil {
			l.logger.Error("failed to scan audit entry", slog.String("error", err.Error()))
			continue
		}
		entries = append(entries, entry)
	}

	return entries, total, nil
}

// AuditQueryOptions represents options for querying audit entries
type AuditQueryOptions struct {
	ArtifactID string
	UserID     string
	ClientID   string
	Action     string
	StartDate  time.Time
	EndDate    time.Time
	Page       int
	PageSize   int
}

// Helper to convert int to string for query building
func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return string(digits[i])
	}
	return itoa(i/10) + string(digits[i%10])
}
