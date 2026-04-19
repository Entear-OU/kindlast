package retrieval

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNewParentFetcher(t *testing.T) {
	t.Run("invalid DSN", func(t *testing.T) {
		_, err := NewParentFetcher("invalid://dsn")
		if err == nil {
			t.Error("Expected error for invalid DSN, got nil")
		}
	})
}

func TestParentFetcher_FetchParentChunks(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock: %v", err)
	}
	defer db.Close()

	fetcher := &ParentFetcher{db: db}

	t.Run("empty chunk IDs", func(t *testing.T) {
		results, err := fetcher.FetchParentChunks(context.Background(), []string{})
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Expected 0 results, got %d", len(results))
		}
	})

	t.Run("single chunk ID", func(t *testing.T) {
		chunkIDs := []string{"child1"}
		now := time.Now()

		rows := sqlmock.NewRows([]string{
			"id", "content", "source_url", "source_name", "tier", "created_at",
		}).AddRow(
			"parent1",
			"Parent content",
			"https://example.com/doc",
			"Example Document",
			"primary",
			now,
		)

		mock.ExpectQuery("SELECT DISTINCT(.+)FROM parent_chunks").
			WithArgs("child1").
			WillReturnRows(rows)

		results, err := fetcher.FetchParentChunks(context.Background(), chunkIDs)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("Expected 1 result, got %d", len(results))
		}

		parent := results[0]
		if parent.ID != "parent1" {
			t.Errorf("Expected ID 'parent1', got '%s'", parent.ID)
		}
		if parent.Content != "Parent content" {
			t.Errorf("Expected content 'Parent content', got '%s'", parent.Content)
		}
		if parent.Tier != "primary" {
			t.Errorf("Expected tier 'primary', got '%s'", parent.Tier)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("multiple chunk IDs", func(t *testing.T) {
		chunkIDs := []string{"child1", "child2", "child3"}
		now := time.Now()

		rows := sqlmock.NewRows([]string{
			"id", "content", "source_url", "source_name", "tier", "created_at",
		}).
			AddRow("parent1", "Content 1", "https://example.com/1", "Doc 1", "primary", now).
			AddRow("parent2", "Content 2", "https://example.com/2", "Doc 2", "secondary", now)

		mock.ExpectQuery("SELECT DISTINCT(.+)FROM parent_chunks").
			WithArgs("child1", "child2", "child3").
			WillReturnRows(rows)

		results, err := fetcher.FetchParentChunks(context.Background(), chunkIDs)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(results) != 2 {
			t.Fatalf("Expected 2 results, got %d", len(results))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("database error", func(t *testing.T) {
		chunkIDs := []string{"child1"}

		mock.ExpectQuery("SELECT DISTINCT(.+)FROM parent_chunks").
			WithArgs("child1").
			WillReturnError(sql.ErrConnDone)

		_, err := fetcher.FetchParentChunks(context.Background(), chunkIDs)
		if err == nil {
			t.Error("Expected error, got nil")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})
}

func TestParentFetcher_FetchParentByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock: %v", err)
	}
	defer db.Close()

	fetcher := &ParentFetcher{db: db}

	t.Run("existing parent", func(t *testing.T) {
		parentID := "parent1"
		now := time.Now()

		rows := sqlmock.NewRows([]string{
			"id", "content", "source_url", "source_name", "tier", "created_at",
		}).AddRow(
			"parent1",
			"Parent content",
			"https://example.com/doc",
			"Example Document",
			"primary",
			now,
		)

		mock.ExpectQuery("SELECT(.+)FROM parent_chunks WHERE id").
			WithArgs(parentID).
			WillReturnRows(rows)

		result, err := fetcher.FetchParentByID(context.Background(), parentID)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if result.ID != "parent1" {
			t.Errorf("Expected ID 'parent1', got '%s'", result.ID)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("non-existing parent", func(t *testing.T) {
		parentID := "nonexistent"

		mock.ExpectQuery("SELECT(.+)FROM parent_chunks WHERE id").
			WithArgs(parentID).
			WillReturnError(sql.ErrNoRows)

		_, err := fetcher.FetchParentByID(context.Background(), parentID)
		if err == nil {
			t.Error("Expected error for non-existent parent, got nil")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})
}

func TestParentFetcher_FetchParentsByIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock: %v", err)
	}
	defer db.Close()

	fetcher := &ParentFetcher{db: db}

	t.Run("empty parent IDs", func(t *testing.T) {
		results, err := fetcher.FetchParentsByIDs(context.Background(), []string{})
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Expected 0 results, got %d", len(results))
		}
	})

	t.Run("multiple parent IDs", func(t *testing.T) {
		parentIDs := []string{"parent1", "parent2"}
		now := time.Now()

		rows := sqlmock.NewRows([]string{
			"id", "content", "source_url", "source_name", "tier", "created_at",
		}).
			AddRow("parent1", "Content 1", "https://example.com/1", "Doc 1", "primary", now).
			AddRow("parent2", "Content 2", "https://example.com/2", "Doc 2", "secondary", now)

		mock.ExpectQuery("SELECT(.+)FROM parent_chunks WHERE id IN").
			WithArgs("parent1", "parent2").
			WillReturnRows(rows)

		results, err := fetcher.FetchParentsByIDs(context.Background(), parentIDs)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(results) != 2 {
			t.Fatalf("Expected 2 results, got %d", len(results))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})
}

func TestParentFetcher_GetStats(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock: %v", err)
	}
	defer db.Close()

	fetcher := &ParentFetcher{db: db}

	t.Run("get statistics", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{
			"total", "unique_sources", "primary_tier", "secondary_tier", "tertiary_tier",
		}).AddRow(100, 10, 50, 30, 20)

		mock.ExpectQuery("SELECT(.+)FROM parent_chunks").
			WillReturnRows(rows)

		stats, err := fetcher.GetStats(context.Background())
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if stats["total"] != 100 {
			t.Errorf("Expected total 100, got %d", stats["total"])
		}
		if stats["unique_sources"] != 10 {
			t.Errorf("Expected unique_sources 10, got %d", stats["unique_sources"])
		}
		if stats["primary"] != 50 {
			t.Errorf("Expected primary 50, got %d", stats["primary"])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("database error", func(t *testing.T) {
		mock.ExpectQuery("SELECT(.+)FROM parent_chunks").
			WillReturnError(sql.ErrConnDone)

		_, err := fetcher.GetStats(context.Background())
		if err == nil {
			t.Error("Expected error, got nil")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unfulfilled expectations: %v", err)
		}
	})
}

func TestParentFetcher_Close(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock: %v", err)
	}

	fetcher := &ParentFetcher{db: db}

	mock.ExpectClose()

	err = fetcher.Close()
	if err != nil {
		t.Errorf("Expected no error on close, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}
