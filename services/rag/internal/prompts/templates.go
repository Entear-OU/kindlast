package prompts

import (
	"fmt"
	"strings"
)

// Topic represents the regulatory topic
type Topic string

const (
	TopicGDPR   Topic = "gdpr"
	TopicAIAct  Topic = "ai_act"
	TopicBoth   Topic = "both"
)

// Citation represents a source citation
type Citation struct {
	Source    string `json:"source"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Excerpt   string `json:"excerpt"`
	Relevance float64 `json:"relevance"`
}

// SystemPrompt returns the base system prompt for regulatory Q&A
func SystemPrompt() string {
	return `You are an expert regulatory compliance assistant specializing in EU data protection and AI regulations.

Your role is to provide accurate, cited answers to regulatory compliance questions based exclusively on the provided context from official regulatory sources.

Core principles:
1. **Cite everything**: Every claim must reference a specific source using [n] notation
2. **Stay grounded**: Only use information from the provided context - never make up citations or facts
3. **Be precise**: Quote exact article numbers, recital numbers, and guidelines when relevant
4. **Admit uncertainty**: If the context doesn't contain the answer, say so clearly
5. **Use plain language**: Translate legal jargon into clear, actionable guidance

Output format:
- Start with a direct answer to the question
- Provide detailed explanation with inline citations [1], [2], etc.
- Include practical implications where relevant
- End with a "Sources" section listing all citations

If confidence is low or the question is outside the provided context, begin your response with: "⚠️ Limited information available in regulatory sources."`
}

// TopicInstructions returns topic-specific instructions
func TopicInstructions(topic Topic) string {
	switch topic {
	case TopicGDPR:
		return `Focus on GDPR (Regulation 2016/679) and related guidance from:
- European Data Protection Board (EDPB) guidelines and opinions
- National DPA guidance and decisions
- Article 29 Working Party opinions (pre-GDPR)
- Court of Justice of the European Union (CJEU) rulings

Key areas: lawfulness, transparency, purpose limitation, data minimization, accuracy, storage limitation, integrity and confidentiality, accountability, data subject rights, transfers, DPIAs, DPOs.`

	case TopicAIAct:
		return `Focus on AI Act (Regulation 2024/1689) and related guidance:
- Risk classification (prohibited, high-risk, limited risk, minimal risk)
- Requirements for high-risk AI systems
- Transparency obligations
- General-purpose AI models (GPAI)
- Conformity assessment procedures
- Market surveillance and governance

Clarify the interaction with GDPR when processing personal data.`

	case TopicBoth:
		return `Address both GDPR and AI Act regulations as relevant to the question.

When both apply:
- Clarify which regulation governs which aspect
- Highlight overlaps (e.g., DPIAs vs AI risk assessments)
- Note cumulative obligations
- Provide integrated guidance

Default to the regulation most directly relevant to the question.`

	default:
		return ""
	}
}

// ContextTemplate formats retrieved context with citations
func ContextTemplate(citations []Citation) string {
	if len(citations) == 0 {
		return "No relevant context found in regulatory sources."
	}

	var sb strings.Builder
	sb.WriteString("# Retrieved Context\n\n")

	for i, cite := range citations {
		sb.WriteString(fmt.Sprintf("[%d] **%s** — %s\n", i+1, cite.Title, cite.Source))
		sb.WriteString(fmt.Sprintf("Relevance: %.2f\n", cite.Relevance))
		if cite.URL != "" {
			sb.WriteString(fmt.Sprintf("URL: %s\n", cite.URL))
		}
		sb.WriteString(fmt.Sprintf("\n%s\n\n", cite.Excerpt))
		sb.WriteString("---\n\n")
	}

	return sb.String()
}

// BuildPrompt constructs the complete prompt for the generation model
func BuildPrompt(query string, topic Topic, citations []Citation) string {
	var sb strings.Builder

	// System prompt
	sb.WriteString(SystemPrompt())
	sb.WriteString("\n\n")

	// Topic-specific instructions
	if topicInstr := TopicInstructions(topic); topicInstr != "" {
		sb.WriteString("## Topic Context\n\n")
		sb.WriteString(topicInstr)
		sb.WriteString("\n\n")
	}

	// Retrieved context
	sb.WriteString(ContextTemplate(citations))
	sb.WriteString("\n\n")

	// User query
	sb.WriteString("## Question\n\n")
	sb.WriteString(query)
	sb.WriteString("\n\n")

	// Instructions
	sb.WriteString("## Instructions\n\n")
	sb.WriteString("Answer the question using only the context provided above. ")
	sb.WriteString("Use [n] citations for every factual claim. ")
	sb.WriteString("If the context is insufficient, state this clearly at the beginning of your response.")

	return sb.String()
}

// LowConfidenceWarning returns a warning message for low-confidence results
func LowConfidenceWarning(maxScore float64) string {
	return fmt.Sprintf(`⚠️ **Limited Confidence in Results** (max relevance: %.2f)

The search returned results with low relevance scores. The answer below may not fully address your question.

Recommendations:
- Rephrase your question to be more specific
- Include key regulatory terms (e.g., article numbers, specific obligations)
- Try breaking complex questions into smaller parts
- Verify critical information in official sources

---

`, maxScore)
}

// StreamingChunkTypes define the types of SSE events
const (
	ChunkTypeContent  = "content"
	ChunkTypeCitation = "citation"
	ChunkTypeError    = "error"
	ChunkTypeDone     = "done"
)

// FormatSSEEvent formats data as a Server-Sent Event
func FormatSSEEvent(eventType, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, data)
}
