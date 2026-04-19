package retrieval

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
)

// ParentFetcher handles fetching parent chunks from PostgreSQL
type ParentFetcher struct {
	db *sql.DB
}

// NewParentFetcher creates a new ParentFetcher
func NewParentFetcher(dsn string) (*ParentFetcher, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	return &ParentFetcher{
		db: db,
	}, nil
}

// Close closes the database connection
func (p *ParentFetcher) Close() error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// FetchParentChunks fetches parent chunks given child chunk IDs
func (p *ParentFetcher) FetchParentChunks(ctx context.Context, chunkIDs []string) ([]ParentChunk, error) {
	if len(chunkIDs) == 0 {
		return []ParentChunk{}, nil
	}

	// Build the query with placeholders
	placeholders := make([]string, len(chunkIDs))
	args := make([]interface{}, len(chunkIDs))
	for i, id := range chunkIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT
			p.id,
			p.content,
			p.source_url,
			p.source_name,
			p.tier,
			p.created_at
		FROM parent_chunks p
		INNER JOIN child_chunks c ON c.parent_id = p.id
		WHERE c.id IN (%s)
		ORDER BY p.created_at DESC
	`, strings.Join(placeholders, ","))

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query parent chunks: %w", err)
	}
	defer rows.Close()

	var parents []ParentChunk
	for rows.Next() {
		var parent ParentChunk
		err := rows.Scan(
			&parent.ID,
			&parent.Content,
			&parent.SourceURL,
			&parent.SourceName,
			&parent.Tier,
			&parent.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan parent chunk: %w", err)
		}
		parents = append(parents, parent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating parent chunks: %w", err)
	}

	return parents, nil
}

// FetchParentByID fetches a single parent chunk by ID
func (p *ParentFetcher) FetchParentByID(ctx context.Context, parentID string) (*ParentChunk, error) {
	query := `
		SELECT
			id,
			content,
			source_url,
			source_name,
			tier,
			created_at
		FROM parent_chunks
		WHERE id = $1
	`

	var parent ParentChunk
	err := p.db.QueryRowContext(ctx, query, parentID).Scan(
		&parent.ID,
		&parent.Content,
		&parent.SourceURL,
		&parent.SourceName,
		&parent.Tier,
		&parent.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("parent chunk not found: %s", parentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch parent chunk: %w", err)
	}

	return &parent, nil
}

// FetchParentsByIDs fetches multiple parent chunks by their IDs directly
func (p *ParentFetcher) FetchParentsByIDs(ctx context.Context, parentIDs []string) ([]ParentChunk, error) {
	if len(parentIDs) == 0 {
		return []ParentChunk{}, nil
	}

	// Build the query with placeholders
	placeholders := make([]string, len(parentIDs))
	args := make([]interface{}, len(parentIDs))
	for i, id := range parentIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT
			id,
			content,
			source_url,
			source_name,
			tier,
			created_at
		FROM parent_chunks
		WHERE id IN (%s)
		ORDER BY created_at DESC
	`, strings.Join(placeholders, ","))

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query parent chunks: %w", err)
	}
	defer rows.Close()

	var parents []ParentChunk
	for rows.Next() {
		var parent ParentChunk
		err := rows.Scan(
			&parent.ID,
			&parent.Content,
			&parent.SourceURL,
			&parent.SourceName,
			&parent.Tier,
			&parent.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan parent chunk: %w", err)
		}
		parents = append(parents, parent)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating parent chunks: %w", err)
	}

	return parents, nil
}

// GetStats returns statistics about parent chunks in the database
func (p *ParentFetcher) GetStats(ctx context.Context) (map[string]int, error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(DISTINCT source_name) as unique_sources,
			COUNT(CASE WHEN tier = 'primary' THEN 1 END) as primary_tier,
			COUNT(CASE WHEN tier = 'secondary' THEN 1 END) as secondary_tier,
			COUNT(CASE WHEN tier = 'tertiary' THEN 1 END) as tertiary_tier
		FROM parent_chunks
	`

	var total, uniqueSources, primary, secondary, tertiary int
	err := p.db.QueryRowContext(ctx, query).Scan(
		&total,
		&uniqueSources,
		&primary,
		&secondary,
		&tertiary,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stats: %w", err)
	}

	return map[string]int{
		"total":          total,
		"unique_sources": uniqueSources,
		"primary":        primary,
		"secondary":      secondary,
		"tertiary":       tertiary,
	}, nil
}
