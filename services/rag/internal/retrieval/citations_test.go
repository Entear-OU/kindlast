package retrieval

import (
	"strings"
	"testing"
	"time"
)

func TestNewCitationBuilder(t *testing.T) {
	cb := NewCitationBuilder()
	if cb == nil {
		t.Fatal("Expected citation builder, got nil")
	}
	if cb.maxExcerptLength != 500 {
		t.Errorf("Expected default excerpt length 500, got %d", cb.maxExcerptLength)
	}
}

func TestNewCitationBuilderWithExcerptLength(t *testing.T) {
	cb := NewCitationBuilderWithExcerptLength(300)
	if cb == nil {
		t.Fatal("Expected citation builder, got nil")
	}
	if cb.maxExcerptLength != 300 {
		t.Errorf("Expected excerpt length 300, got %d", cb.maxExcerptLength)
	}
}

func TestCitationBuilder_BuildCitations(t *testing.T) {
	cb := NewCitationBuilder()

	t.Run("empty parents", func(t *testing.T) {
		citations := cb.BuildCitations([]ParentChunk{})
		if len(citations) != 0 {
			t.Errorf("Expected 0 citations, got %d", len(citations))
		}
	})

	t.Run("single parent", func(t *testing.T) {
		parents := []ParentChunk{
			{
				ID:         "parent1",
				Content:    "This is the content of the parent chunk.",
				SourceURL:  "https://example.com/doc",
				SourceName: "Example Document",
				Tier:       "primary",
			},
		}

		citations := cb.BuildCitations(parents)
		if len(citations) != 1 {
			t.Fatalf("Expected 1 citation, got %d", len(citations))
		}

		citation := citations[0]
		if citation.ID != "parent1" {
			t.Errorf("Expected ID 'parent1', got '%s'", citation.ID)
		}
		if citation.SourceName != "Example Document" {
			t.Errorf("Expected source 'Example Document', got '%s'", citation.SourceName)
		}
		if citation.Tier != "primary" {
			t.Errorf("Expected tier 'primary', got '%s'", citation.Tier)
		}
	})

	t.Run("multiple parents", func(t *testing.T) {
		parents := []ParentChunk{
			{ID: "p1", Content: "Content 1", SourceURL: "url1", SourceName: "Doc 1", Tier: "primary"},
			{ID: "p2", Content: "Content 2", SourceURL: "url2", SourceName: "Doc 2", Tier: "secondary"},
			{ID: "p3", Content: "Content 3", SourceURL: "url3", SourceName: "Doc 3", Tier: "tertiary"},
		}

		citations := cb.BuildCitations(parents)
		if len(citations) != 3 {
			t.Fatalf("Expected 3 citations, got %d", len(citations))
		}
	})
}

func TestCitationBuilder_BuildCitation(t *testing.T) {
	cb := NewCitationBuilder()

	parent := ParentChunk{
		ID:         "parent1",
		Content:    "Test content",
		SourceURL:  "https://example.com",
		SourceName: "Test Doc",
		Tier:       "primary",
	}

	citation := cb.BuildCitation(parent)
	if citation.ID != "parent1" {
		t.Errorf("Expected ID 'parent1', got '%s'", citation.ID)
	}
}

func TestCitationBuilder_CreateExcerpt(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		maxLength      int
		wantTruncated  bool
		wantEllipsis   bool
	}{
		{
			name:          "short content",
			content:       "This is a short text.",
			maxLength:     100,
			wantTruncated: false,
			wantEllipsis:  false,
		},
		{
			name:          "long content",
			content:       strings.Repeat("This is a long text. ", 50),
			maxLength:     100,
			wantTruncated: true,
			wantEllipsis:  true,
		},
		{
			name:          "exact length",
			content:       strings.Repeat("a", 100),
			maxLength:     100,
			wantTruncated: false,
			wantEllipsis:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCitationBuilderWithExcerptLength(tt.maxLength)
			excerpt := cb.createExcerpt(tt.content)

			if tt.wantTruncated && len(excerpt) > tt.maxLength+3 { // +3 for "..."
				t.Errorf("Expected excerpt length <= %d, got %d", tt.maxLength+3, len(excerpt))
			}

			if tt.wantEllipsis && !strings.HasSuffix(excerpt, "...") {
				t.Error("Expected excerpt to end with ellipsis")
			}

			if !tt.wantEllipsis && strings.HasSuffix(excerpt, "...") {
				t.Error("Expected excerpt without ellipsis")
			}
		})
	}
}

func TestCitationBuilder_BuildCitationsWithContext(t *testing.T) {
	cb := NewCitationBuilderWithExcerptLength(100)

	t.Run("with query terms", func(t *testing.T) {
		parents := []ParentChunk{
			{
				ID:         "p1",
				Content:    "The beginning of the text. GDPR requires explicit consent from users. The end of the text.",
				SourceURL:  "url1",
				SourceName: "Doc 1",
				Tier:       "primary",
			},
		}
		queryTerms := []string{"GDPR", "consent"}

		citations := cb.BuildCitationsWithContext(parents, queryTerms)
		if len(citations) != 1 {
			t.Fatalf("Expected 1 citation, got %d", len(citations))
		}

		excerpt := citations[0].Excerpt
		if !strings.Contains(excerpt, "GDPR") && !strings.Contains(excerpt, "gdpr") {
			t.Error("Expected excerpt to contain query term")
		}
	})

	t.Run("without query terms", func(t *testing.T) {
		parents := []ParentChunk{
			{
				ID:         "p1",
				Content:    "Test content",
				SourceURL:  "url1",
				SourceName: "Doc 1",
				Tier:       "primary",
			},
		}

		citations := cb.BuildCitationsWithContext(parents, []string{})
		if len(citations) != 1 {
			t.Fatalf("Expected 1 citation, got %d", len(citations))
		}
	})
}

func TestCitationBuilder_DeduplicateCitations(t *testing.T) {
	cb := NewCitationBuilder()

	t.Run("no duplicates", func(t *testing.T) {
		citations := []Citation{
			{ID: "c1", SourceName: "Doc 1"},
			{ID: "c2", SourceName: "Doc 2"},
			{ID: "c3", SourceName: "Doc 3"},
		}

		result := cb.DeduplicateCitations(citations)
		if len(result) != 3 {
			t.Errorf("Expected 3 citations, got %d", len(result))
		}
	})

	t.Run("with duplicates", func(t *testing.T) {
		citations := []Citation{
			{ID: "c1", SourceName: "Doc 1"},
			{ID: "c2", SourceName: "Doc 2"},
			{ID: "c1", SourceName: "Doc 1 Duplicate"},
			{ID: "c3", SourceName: "Doc 3"},
			{ID: "c2", SourceName: "Doc 2 Duplicate"},
		}

		result := cb.DeduplicateCitations(citations)
		if len(result) != 3 {
			t.Errorf("Expected 3 unique citations, got %d", len(result))
		}

		// Verify first occurrence is kept
		if result[0].SourceName != "Doc 1" {
			t.Errorf("Expected first occurrence to be kept")
		}
	})

	t.Run("empty citations", func(t *testing.T) {
		result := cb.DeduplicateCitations([]Citation{})
		if len(result) != 0 {
			t.Errorf("Expected 0 citations, got %d", len(result))
		}
	})
}

func TestCitationBuilder_FormatCitation(t *testing.T) {
	cb := NewCitationBuilder()

	citation := Citation{
		ID:         "c1",
		SourceName: "Test Document",
		SourceURL:  "https://example.com/doc",
		Excerpt:    "This is an excerpt.",
		Tier:       "primary",
	}

	formatted := cb.FormatCitation(citation)
	if !strings.Contains(formatted, "Test Document") {
		t.Error("Expected formatted citation to contain source name")
	}
	if !strings.Contains(formatted, "https://example.com/doc") {
		t.Error("Expected formatted citation to contain URL")
	}
	if !strings.Contains(formatted, "This is an excerpt.") {
		t.Error("Expected formatted citation to contain excerpt")
	}
}

func TestCitationBuilder_FormatCitations(t *testing.T) {
	cb := NewCitationBuilder()

	t.Run("empty citations", func(t *testing.T) {
		formatted := cb.FormatCitations([]Citation{})
		if formatted != "" {
			t.Error("Expected empty string for empty citations")
		}
	})

	t.Run("multiple citations", func(t *testing.T) {
		citations := []Citation{
			{ID: "c1", SourceName: "Doc 1", SourceURL: "url1", Excerpt: "Excerpt 1", Tier: "primary"},
			{ID: "c2", SourceName: "Doc 2", SourceURL: "url2", Excerpt: "Excerpt 2", Tier: "secondary"},
		}

		formatted := cb.FormatCitations(citations)
		if !strings.Contains(formatted, "## Citations") {
			t.Error("Expected citations header")
		}
		if !strings.Contains(formatted, "Doc 1") || !strings.Contains(formatted, "Doc 2") {
			t.Error("Expected both document names in formatted output")
		}
	})
}

func TestCitationBuilder_GroupCitationsByTier(t *testing.T) {
	cb := NewCitationBuilder()

	citations := []Citation{
		{ID: "c1", Tier: "primary"},
		{ID: "c2", Tier: "secondary"},
		{ID: "c3", Tier: "primary"},
		{ID: "c4", Tier: "tertiary"},
		{ID: "c5", Tier: ""},
	}

	grouped := cb.GroupCitationsByTier(citations)

	if len(grouped["primary"]) != 2 {
		t.Errorf("Expected 2 primary citations, got %d", len(grouped["primary"]))
	}
	if len(grouped["secondary"]) != 1 {
		t.Errorf("Expected 1 secondary citation, got %d", len(grouped["secondary"]))
	}
	if len(grouped["tertiary"]) != 1 {
		t.Errorf("Expected 1 tertiary citation, got %d", len(grouped["tertiary"]))
	}
	if len(grouped["unknown"]) != 1 {
		t.Errorf("Expected 1 unknown citation, got %d", len(grouped["unknown"]))
	}
}

func TestCitationBuilder_SortCitationsByTier(t *testing.T) {
	cb := NewCitationBuilder()

	citations := []Citation{
		{ID: "c1", Tier: "tertiary"},
		{ID: "c2", Tier: "primary"},
		{ID: "c3", Tier: "secondary"},
		{ID: "c4", Tier: "primary"},
		{ID: "c5", Tier: "unknown"},
	}

	sorted := cb.SortCitationsByTier(citations)

	if len(sorted) != 5 {
		t.Fatalf("Expected 5 citations, got %d", len(sorted))
	}

	// Verify primary comes first
	if sorted[0].Tier != "primary" || sorted[1].Tier != "primary" {
		t.Error("Expected primary citations first")
	}

	// Verify secondary comes after primary
	if sorted[2].Tier != "secondary" {
		t.Error("Expected secondary citation in position 2")
	}

	// Verify tertiary comes after secondary
	if sorted[3].Tier != "tertiary" {
		t.Error("Expected tertiary citation in position 3")
	}
}

func TestCitationBuilder_FormatInlineCitation(t *testing.T) {
	cb := NewCitationBuilder()

	citation := Citation{
		ID:         "c1",
		SourceName: "Test Doc",
		SourceURL:  "https://example.com",
	}

	formatted := cb.FormatInlineCitation(0, citation)
	if !strings.Contains(formatted, "[1]") {
		t.Error("Expected inline citation to contain [1]")
	}
	if !strings.Contains(formatted, "https://example.com") {
		t.Error("Expected inline citation to contain URL")
	}
}

// Test with actual parent chunks
func TestCitationBuilder_Integration(t *testing.T) {
	cb := NewCitationBuilderWithExcerptLength(200)

	parents := []ParentChunk{
		{
			ID:         "gdpr-art-7",
			Content:    "Article 7 of GDPR states that consent must be freely given, specific, informed and unambiguous. The data subject must have genuine choice and control. Organizations must be able to demonstrate that consent was given.",
			SourceURL:  "https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32016R0679",
			SourceName: "GDPR Article 7 - Conditions for consent",
			Tier:       "primary",
			CreatedAt:  time.Now(),
		},
		{
			ID:         "edpb-guidelines",
			Content:    "The EDPB Guidelines on consent clarify that consent requires a clear affirmative action. Pre-ticked boxes do not constitute valid consent under GDPR.",
			SourceURL:  "https://edpb.europa.eu/our-work-tools/our-documents/guidelines/guidelines-052020-consent-under-regulation-2016679_en",
			SourceName: "EDPB Guidelines 05/2020 on consent",
			Tier:       "secondary",
			CreatedAt:  time.Now(),
		},
	}

	citations := cb.BuildCitations(parents)

	if len(citations) != 2 {
		t.Fatalf("Expected 2 citations, got %d", len(citations))
	}

	// Test deduplication
	duplicatedCitations := append(citations, citations[0])
	deduplicated := cb.DeduplicateCitations(duplicatedCitations)
	if len(deduplicated) != 2 {
		t.Errorf("Expected 2 deduplicated citations, got %d", len(deduplicated))
	}

	// Test sorting
	sorted := cb.SortCitationsByTier(citations)
	if sorted[0].Tier != "primary" {
		t.Error("Expected primary tier first after sorting")
	}

	// Test formatting
	formatted := cb.FormatCitations(sorted)
	if !strings.Contains(formatted, "GDPR Article 7") {
		t.Error("Expected formatted output to contain citation")
	}
}
