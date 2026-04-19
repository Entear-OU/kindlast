package retrieval

import (
	"fmt"
	"strings"
)

// CitationBuilder handles building citation objects from parent chunks
type CitationBuilder struct {
	maxExcerptLength int
}

// NewCitationBuilder creates a new citation builder
func NewCitationBuilder() *CitationBuilder {
	return &CitationBuilder{
		maxExcerptLength: 500, // Default excerpt length
	}
}

// NewCitationBuilderWithExcerptLength creates a citation builder with custom excerpt length
func NewCitationBuilderWithExcerptLength(maxLength int) *CitationBuilder {
	return &CitationBuilder{
		maxExcerptLength: maxLength,
	}
}

// BuildCitations creates citation objects from parent chunks
func (cb *CitationBuilder) BuildCitations(parents []ParentChunk) []Citation {
	citations := make([]Citation, 0, len(parents))

	for _, parent := range parents {
		citation := Citation{
			ID:         parent.ID,
			SourceName: parent.SourceName,
			SourceURL:  parent.SourceURL,
			Excerpt:    cb.createExcerpt(parent.Content),
			Tier:       parent.Tier,
		}
		citations = append(citations, citation)
	}

	return citations
}

// BuildCitation creates a single citation from a parent chunk
func (cb *CitationBuilder) BuildCitation(parent ParentChunk) Citation {
	return Citation{
		ID:         parent.ID,
		SourceName: parent.SourceName,
		SourceURL:  parent.SourceURL,
		Excerpt:    cb.createExcerpt(parent.Content),
		Tier:       parent.Tier,
	}
}

// BuildCitationsWithContext creates citations with context-aware excerpts
// It tries to find the most relevant part of the content based on query terms
func (cb *CitationBuilder) BuildCitationsWithContext(parents []ParentChunk, queryTerms []string) []Citation {
	citations := make([]Citation, 0, len(parents))

	for _, parent := range parents {
		citation := Citation{
			ID:         parent.ID,
			SourceName: parent.SourceName,
			SourceURL:  parent.SourceURL,
			Excerpt:    cb.createContextualExcerpt(parent.Content, queryTerms),
			Tier:       parent.Tier,
		}
		citations = append(citations, citation)
	}

	return citations
}

// createExcerpt creates a truncated excerpt from the content
func (cb *CitationBuilder) createExcerpt(content string) string {
	// Clean up the content
	cleaned := strings.TrimSpace(content)

	// If content is shorter than max length, return as is
	if len(cleaned) <= cb.maxExcerptLength {
		return cleaned
	}

	// Truncate to max length
	excerpt := cleaned[:cb.maxExcerptLength]

	// Try to cut at the last sentence boundary
	if lastPeriod := strings.LastIndex(excerpt, ". "); lastPeriod > 0 {
		excerpt = excerpt[:lastPeriod+1]
	} else if lastSpace := strings.LastIndex(excerpt, " "); lastSpace > 0 {
		// If no sentence boundary, cut at last word
		excerpt = excerpt[:lastSpace]
	}

	return excerpt + "..."
}

// createContextualExcerpt creates an excerpt focusing on query-relevant content
func (cb *CitationBuilder) createContextualExcerpt(content string, queryTerms []string) string {
	// Clean up the content
	cleaned := strings.TrimSpace(content)

	// If no query terms or content is short, use standard excerpt
	if len(queryTerms) == 0 || len(cleaned) <= cb.maxExcerptLength {
		return cb.createExcerpt(content)
	}

	// Find the first occurrence of any query term
	lowerContent := strings.ToLower(cleaned)
	bestPos := -1

	for _, term := range queryTerms {
		lowerTerm := strings.ToLower(term)
		if pos := strings.Index(lowerContent, lowerTerm); pos != -1 {
			if bestPos == -1 || pos < bestPos {
				bestPos = pos
			}
		}
	}

	// If no query term found, use standard excerpt
	if bestPos == -1 {
		return cb.createExcerpt(content)
	}

	// Calculate excerpt bounds centered around the term
	halfLength := cb.maxExcerptLength / 2
	start := bestPos - halfLength
	if start < 0 {
		start = 0
	}

	end := start + cb.maxExcerptLength
	if end > len(cleaned) {
		end = len(cleaned)
		start = end - cb.maxExcerptLength
		if start < 0 {
			start = 0
		}
	}

	excerpt := cleaned[start:end]

	// Add ellipsis if truncated
	prefix := ""
	suffix := ""
	if start > 0 {
		prefix = "..."
		// Find first space to avoid cutting words
		if spaceIdx := strings.Index(excerpt, " "); spaceIdx > 0 && spaceIdx < 50 {
			excerpt = excerpt[spaceIdx+1:]
		}
	}
	if end < len(cleaned) {
		suffix = "..."
		// Find last space to avoid cutting words
		if spaceIdx := strings.LastIndex(excerpt, " "); spaceIdx > len(excerpt)-50 && spaceIdx > 0 {
			excerpt = excerpt[:spaceIdx]
		}
	}

	return prefix + strings.TrimSpace(excerpt) + suffix
}

// DeduplicateCitations removes duplicate citations based on ID
func (cb *CitationBuilder) DeduplicateCitations(citations []Citation) []Citation {
	seen := make(map[string]bool)
	result := make([]Citation, 0, len(citations))

	for _, citation := range citations {
		if !seen[citation.ID] {
			seen[citation.ID] = true
			result = append(result, citation)
		}
	}

	return result
}

// FormatCitation formats a citation for display
func (cb *CitationBuilder) FormatCitation(citation Citation) string {
	return fmt.Sprintf("[%s](%s)\n%s",
		citation.SourceName,
		citation.SourceURL,
		citation.Excerpt,
	)
}

// FormatCitations formats multiple citations for display
func (cb *CitationBuilder) FormatCitations(citations []Citation) string {
	if len(citations) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Citations\n\n")

	for i, citation := range citations {
		sb.WriteString(fmt.Sprintf("%d. [%s](%s) _%s tier_\n",
			i+1,
			citation.SourceName,
			citation.SourceURL,
			citation.Tier,
		))
		sb.WriteString(fmt.Sprintf("   > %s\n\n", citation.Excerpt))
	}

	return sb.String()
}

// FormatInlineCitation creates an inline citation reference
func (cb *CitationBuilder) FormatInlineCitation(index int, citation Citation) string {
	return fmt.Sprintf("[%d](%s)", index+1, citation.SourceURL)
}

// GroupCitationsByTier groups citations by their tier
func (cb *CitationBuilder) GroupCitationsByTier(citations []Citation) map[string][]Citation {
	grouped := make(map[string][]Citation)

	for _, citation := range citations {
		tier := citation.Tier
		if tier == "" {
			tier = "unknown"
		}
		grouped[tier] = append(grouped[tier], citation)
	}

	return grouped
}

// SortCitationsByTier sorts citations by tier priority (primary > secondary > tertiary)
func (cb *CitationBuilder) SortCitationsByTier(citations []Citation) []Citation {
	tierOrder := map[string]int{
		"primary":   1,
		"secondary": 2,
		"tertiary":  3,
		"unknown":   4,
	}

	// Simple bubble sort
	sorted := make([]Citation, len(citations))
	copy(sorted, citations)

	n := len(sorted)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			tier1 := tierOrder[sorted[j].Tier]
			tier2 := tierOrder[sorted[j+1].Tier]
			if tier1 == 0 {
				tier1 = tierOrder["unknown"]
			}
			if tier2 == 0 {
				tier2 = tierOrder["unknown"]
			}

			if tier1 > tier2 {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	return sorted
}
