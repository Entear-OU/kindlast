// Package artifact provides artifact generation prompt templates for DPO Copilot.
// Each artifact type (RoPA, DPIA Screening, DPA Gap Analysis, Lawful Basis Assessment,
// AI Act Classification) has specific prompts that instruct the LLM to output
// structured JSON matching the schemas defined in schemas.go.
package artifact

import (
	"fmt"
	"strings"

	"github.com/entear/kindlast/services/rag/internal/providers"
)

// PromptPair contains the system prompt and user messages for artifact generation.
type PromptPair struct {
	System   string
	Messages []providers.Message
}

// ClientContext contains information about the DPO's client organization
// used to generate artifacts.
type ClientContext struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Sector            string   `json:"sector"`
	Country           string   `json:"country"`
	EmployeeCount     int      `json:"employee_count"`
	TechStack         []string `json:"tech_stack"`
	DataSubjects      []string `json:"data_subjects"`
	ProcessingPurposes []string `json:"processing_purposes"`
}

// ProcessorProfileData contains processor information used in prompt context.
type ProcessorProfileData struct {
	Name              string   `json:"name"`
	Slug              string   `json:"slug"`
	Category          string   `json:"category"`
	Headquarters      string   `json:"headquarters"`
	DataCategories    []string `json:"data_categories"`
	ProcessingPurposes []string `json:"processing_purposes"`
	DataLocations     []string `json:"data_locations"`
	TransferMechanism string   `json:"transfer_mechanism"`
	DPAStatus         string   `json:"dpa_status"`
	DPAURL            string   `json:"dpa_url"`
}

// RegulatoryDocument represents a retrieved regulatory source for prompt context.
type RegulatoryDocument struct {
	Title     string
	SourceURL string
	Text      string
	Tier      string
}

// BuildArtifactPrompt constructs the appropriate prompt for the given artifact type.
// It formats the regulatory context as numbered sources [1], [2], etc., and
// includes processor profile data for the client's tech stack.
func BuildArtifactPrompt(
	artifactType string,
	client ClientContext,
	processors []ProcessorProfileData,
	regulatoryContext []RegulatoryDocument,
) PromptPair {
	// Format regulatory context as numbered sources
	sourcesContext := formatRegulatoryContext(regulatoryContext)

	// Format processor profiles
	processorsContext := formatProcessorContext(processors)

	switch artifactType {
	case "ropa":
		return ropaPrompt(client, processorsContext, sourcesContext)
	case "dpia_screening":
		return dpiaPrompt(client, processorsContext, sourcesContext)
	case "dpa_gap":
		return dpaGapPrompt(client, processorsContext, sourcesContext)
	case "lawful_basis":
		return lawfulBasisPrompt(client, processorsContext, sourcesContext)
	case "ai_act_classification":
		return aiActPrompt(client, sourcesContext)
	default:
		// Default to RoPA for unknown types
		return ropaPrompt(client, processorsContext, sourcesContext)
	}
}

// formatRegulatoryContext formats regulatory documents as numbered sources.
func formatRegulatoryContext(docs []RegulatoryDocument) string {
	if len(docs) == 0 {
		return "No regulatory sources available."
	}

	var sb strings.Builder
	for i, doc := range docs {
		fmt.Fprintf(&sb, "[%d] %s", i+1, doc.Title)
		if doc.SourceURL != "" {
			fmt.Fprintf(&sb, " (%s)", doc.SourceURL)
		}
		if doc.Tier != "" {
			fmt.Fprintf(&sb, " [%s source]", doc.Tier)
		}
		sb.WriteString("\n")
		sb.WriteString(doc.Text)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// formatProcessorContext formats processor profiles for prompt inclusion.
func formatProcessorContext(processors []ProcessorProfileData) string {
	if len(processors) == 0 {
		return "No processor profiles available. Flag all processors as requiring manual review."
	}

	var sb strings.Builder
	for _, p := range processors {
		fmt.Fprintf(&sb, "- %s (%s): ", p.Name, p.Category)
		fmt.Fprintf(&sb, "HQ: %s, ", p.Headquarters)
		fmt.Fprintf(&sb, "Processes: %s, ", strings.Join(p.DataCategories, ", "))
		fmt.Fprintf(&sb, "Transfer: %s, ", p.TransferMechanism)
		fmt.Fprintf(&sb, "DPA: %s", p.DPAStatus)
		if p.DPAURL != "" {
			fmt.Fprintf(&sb, " (%s)", p.DPAURL)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// ropaPrompt generates the prompt for Record of Processing Activities (Article 30 GDPR).
func ropaPrompt(client ClientContext, processors, sources string) PromptPair {
	system := `You are a GDPR compliance expert generating a Record of Processing Activities (RoPA) under Article 30 GDPR.

OUTPUT FORMAT:
You MUST output ONLY valid JSON matching the RoPA schema. No markdown formatting, no code blocks, no commentary outside JSON. Start directly with { and end with }.

CITATION RULES:
- Every claim must reference a source using [N] notation matching the numbered sources provided.
- Include citation indices in the "citations" array for each processing activity.
- When citing GDPR articles, use exact references (e.g., "Art. 6(1)(a)", "Art. 30(1)").
- When citing EDPB guidelines, include the guideline number (e.g., "EDPB Guidelines 2/2019").

ARTICLE 30 REQUIREMENTS:
For each processing activity, you MUST identify:
1. Purpose of processing (Art. 30(1)(b))
2. Lawful basis with specific article reference (Art. 6(1)(a)-(f))
3. Categories of data subjects (Art. 30(1)(c))
4. Categories of personal data (Art. 30(1)(c))
5. Categories of recipients (Art. 30(1)(d))
6. International transfers and safeguards (Art. 30(1)(e))
7. Retention periods with rationale (Art. 30(1)(f))
8. Technical and organizational security measures (Art. 30(1)(g))
9. Whether DPIA is required (Art. 35)

LAWFUL BASIS GUIDANCE:
- Art. 6(1)(a) Consent: Must be freely given, specific, informed, unambiguous
- Art. 6(1)(b) Contract: Processing necessary for contract performance
- Art. 6(1)(c) Legal obligation: Processing required by EU/Member State law
- Art. 6(1)(d) Vital interests: Protecting life of data subject or another person
- Art. 6(1)(e) Public task: Processing necessary for public interest/official authority
- Art. 6(1)(f) Legitimate interests: Requires balancing test (LIA required)

PROCESSOR HANDLING:
- Use processor profile data when available to populate recipient details
- If a processor is not in the provided profiles, set DPA status to "unknown" and add note "requires manual review"
- For each processor, determine if international transfer occurs based on headquarters location

SPECIAL CATEGORIES (Art. 9):
- Flag ANY processing involving: racial/ethnic origin, political opinions, religious beliefs, trade union membership, genetic data, biometric data, health data, sex life/orientation
- If special category data is processed, note that Art. 9(2) derogation is required

CONSERVATIVE APPROACH:
- When uncertain about lawful basis, recommend DPO review
- When uncertain about DPIA requirement, default to "DPIA recommended"
- Flag any processing that MIGHT involve special categories for review`

	userMsg := fmt.Sprintf(`Generate a complete Record of Processing Activities (RoPA) for this organization:

ORGANIZATION DETAILS:
- Name: %s
- Sector: %s
- Country: %s
- Employee count: %d
- Description: %s

DECLARED DATA SUBJECTS: %s

DECLARED PROCESSING PURPOSES: %s

TECH STACK & PROCESSOR PROFILES:
%s

REGULATORY SOURCES (use [N] notation to cite):
%s

Generate a complete RoPA as JSON. Identify ALL processing activities based on:
1. The tech stack and processor profiles
2. The declared processing purposes
3. Typical processing for the organization's sector
4. Employee-related processing if employee count > 0

Each activity ID should follow format "PA-001", "PA-002", etc.`,
		client.Name,
		client.Sector,
		client.Country,
		client.EmployeeCount,
		client.Description,
		strings.Join(client.DataSubjects, ", "),
		strings.Join(client.ProcessingPurposes, ", "),
		processors,
		sources,
	)

	return PromptPair{
		System: system,
		Messages: []providers.Message{
			{Role: "user", Content: userMsg},
		},
	}
}

// dpiaPrompt generates the prompt for DPIA Screening (Article 35 GDPR, EDPB wp248rev.01).
func dpiaPrompt(client ClientContext, processors, sources string) PromptPair {
	system := `You are a GDPR compliance expert conducting a DPIA pre-screening assessment per Article 35 GDPR and the EDPB Guidelines on DPIA (wp248rev.01).

OUTPUT FORMAT:
You MUST output ONLY valid JSON matching the DPIAScreening schema. No markdown formatting, no code blocks, no commentary outside JSON. Start directly with { and end with }.

CITATION RULES:
- Reference sources using [N] notation matching the numbered sources provided.
- Cite specific EDPB guideline sections when evaluating criteria.
- Include exact GDPR article references (Art. 35(1), Art. 35(3), etc.).

THE 9 EDPB CRITERIA (from wp248rev.01):
You MUST evaluate ALL 9 criteria for each processing activity:

1. Evaluation or scoring
   - Including profiling and predicting (work performance, economic situation, health, personal preferences, interests, reliability, behavior, location, movements)

2. Automated decision-making with legal or similarly significant effect
   - Processing that leads to decisions which produce legal effects or similarly significantly affect the data subject

3. Systematic monitoring
   - Processing used to observe, monitor or control data subjects, including data collected through networks or systematic monitoring of a publicly accessible area

4. Sensitive data or data of a highly personal nature
   - Special categories (Art. 9), criminal data (Art. 10), or data that increases possible harm (financial data, location data, private communications)

5. Data processed on a large scale
   - Consider: number of data subjects, volume of data, duration/permanence, geographical extent

6. Matching or combining datasets
   - From different sources, in ways that exceed reasonable expectations

7. Data concerning vulnerable data subjects
   - Children, employees, mentally ill persons, asylum seekers, elderly, patients

8. Innovative use or applying new technological or organizational solutions
   - AI/ML, biometrics, IoT, smart devices

9. Processing that prevents data subjects from exercising a right or using a service or contract
   - Including automated refusal of credit, automated CV screening

DPIA REQUIREMENT RULES (per EDPB guidance):
- If 2 or more criteria are triggered: DPIA is REQUIRED
- If 1 criterion is triggered: DPIA is RECOMMENDED (DPO should assess)
- If 0 criteria triggered: DPIA is likely NOT REQUIRED (but document reasoning)

ARTICLE 35(3) MANDATORY DPIA CASES:
DPIA is always required for:
a) Systematic and extensive evaluation of personal aspects based on automated processing, including profiling, on which decisions with legal effects are based
b) Processing on a large scale of special categories (Art. 9) or criminal data (Art. 10)
c) Systematic monitoring of a publicly accessible area on a large scale

CONSERVATIVE APPROACH:
- When evidence is unclear, err on the side of "triggered"
- Flag borderline cases as "recommended" rather than "not_required"
- Include reasoning for EVERY criterion evaluation`

	userMsg := fmt.Sprintf(`Conduct a DPIA pre-screening assessment for this organization:

ORGANIZATION DETAILS:
- Name: %s
- Sector: %s
- Country: %s
- Employee count: %d
- Description: %s

DECLARED DATA SUBJECTS: %s

DECLARED PROCESSING PURPOSES: %s

TECH STACK & PROCESSOR PROFILES:
%s

REGULATORY SOURCES (use [N] notation to cite):
%s

For each identifiable processing activity:
1. Evaluate against ALL 9 EDPB criteria
2. Provide evidence for why each criterion is triggered or not
3. Determine risk level (high/medium/low)
4. Conclude whether DPIA is required

Then provide an overall screening result based on the aggregate findings.`,
		client.Name,
		client.Sector,
		client.Country,
		client.EmployeeCount,
		client.Description,
		strings.Join(client.DataSubjects, ", "),
		strings.Join(client.ProcessingPurposes, ", "),
		processors,
		sources,
	)

	return PromptPair{
		System: system,
		Messages: []providers.Message{
			{Role: "user", Content: userMsg},
		},
	}
}

// dpaGapPrompt generates the prompt for Data Processing Agreement gap analysis.
func dpaGapPrompt(client ClientContext, processors, sources string) PromptPair {
	system := `You are a GDPR compliance expert conducting a Data Processing Agreement (DPA) gap analysis per Article 28 GDPR.

OUTPUT FORMAT:
You MUST output ONLY valid JSON matching the DPAGapAnalysis schema. No markdown formatting, no code blocks, no commentary outside JSON. Start directly with { and end with }.

CITATION RULES:
- Reference sources using [N] notation matching the numbered sources provided.
- Cite Article 28 requirements specifically.
- Reference EDPB guidance on controller-processor relationships where relevant.

ARTICLE 28 REQUIREMENTS:
A valid DPA must include provisions on:
1. Processing only on documented instructions from the controller (Art. 28(3)(a))
2. Confidentiality obligations for processing personnel (Art. 28(3)(b))
3. Security measures per Article 32 (Art. 28(3)(c))
4. Conditions for engaging sub-processors (Art. 28(3)(d))
5. Assistance with data subject rights (Art. 28(3)(e))
6. Assistance with security, breach notification, DPIA (Art. 28(3)(f))
7. Deletion or return of data after service ends (Art. 28(3)(g))
8. Audit rights for the controller (Art. 28(3)(h))

INTERNATIONAL TRANSFERS (Chapter V):
For processors outside the EEA, determine transfer mechanism:
- Adequacy decision (Art. 45): UK, Switzerland, Canada, Japan, South Korea, etc.
- EU-US Data Privacy Framework (DPF): US companies with valid certification
- Standard Contractual Clauses (SCCs): Module 2 for controller-to-processor
- Binding Corporate Rules (Art. 47): Intra-group transfers

When SCCs are needed:
- Identify which SCC module applies
- Flag that a Transfer Impact Assessment (TIA) may be required (Schrems II)
- Note supplementary measures may be needed based on destination country

DPA STATUS DETERMINATION:
- "in_place": Processor offers standard DPA or we have evidence of signed DPA
- "needed": No DPA in place, one must be executed
- "unknown": Cannot determine, requires manual verification

ACTIONS FOR EACH PROCESSOR:
Generate specific, actionable items:
- "Execute DPA using processor's standard terms at [URL]"
- "Negotiate custom DPA terms for [specific concern]"
- "Implement SCCs Module 2 for international transfer"
- "Conduct Transfer Impact Assessment for [country]"
- "Verify DPF certification status at [URL]"
- "Review subprocessor list at [URL]"`

	userMsg := fmt.Sprintf(`Conduct a DPA gap analysis for this organization:

ORGANIZATION DETAILS:
- Name: %s
- Sector: %s
- Country: %s (controller location)
- Description: %s

TECH STACK & PROCESSOR PROFILES:
%s

REGULATORY SOURCES (use [N] notation to cite):
%s

For each processor in the tech stack:
1. Identify data categories being processed
2. Determine DPA status (in_place/needed/unknown)
3. Check if international transfer is required
4. Identify appropriate transfer mechanism
5. Determine if TIA is required
6. List specific actions the DPO needs to take

Provide summary statistics at the end.`,
		client.Name,
		client.Sector,
		client.Country,
		client.Description,
		processors,
		sources,
	)

	return PromptPair{
		System: system,
		Messages: []providers.Message{
			{Role: "user", Content: userMsg},
		},
	}
}

// lawfulBasisPrompt generates the prompt for lawful basis assessment.
func lawfulBasisPrompt(client ClientContext, processors, sources string) PromptPair {
	system := `You are a GDPR compliance expert conducting a lawful basis assessment per Article 6 GDPR.

OUTPUT FORMAT:
You MUST output ONLY valid JSON matching the RoPA schema (focusing on the lawful_basis field for each activity). No markdown formatting, no code blocks, no commentary outside JSON. Start directly with { and end with }.

CITATION RULES:
- Reference sources using [N] notation matching the numbered sources provided.
- Cite specific GDPR articles and recitals.
- Reference EDPB and ICO guidance on lawful basis selection.

ARTICLE 6(1) LAWFUL BASES:

Art. 6(1)(a) - CONSENT:
- Must be freely given, specific, informed, and unambiguous (Art. 4(11))
- Clear affirmative action required
- Easily withdrawable
- NOT appropriate where significant imbalance of power (employer-employee, public authority)
- Document: consent mechanism, withdrawal process, records

Art. 6(1)(b) - CONTRACT:
- Processing NECESSARY for contract performance or pre-contractual steps
- "Necessary" = processing without which contract cannot be performed
- Cannot bundle unnecessary processing into contract terms
- Document: which contract, why processing is necessary

Art. 6(1)(c) - LEGAL OBLIGATION:
- Must identify specific EU or Member State law
- Obligation must be "clear and precise"
- Cannot be mere possibility of legal request
- Document: specific legal provision, jurisdiction

Art. 6(1)(d) - VITAL INTERESTS:
- Life-threatening situations only
- Last resort - use only if no other basis applies
- Rarely applicable in commercial contexts
- Document: why other bases don't apply

Art. 6(1)(e) - PUBLIC TASK:
- Processing necessary for task in public interest
- Basis in EU or Member State law required
- Typically for public authorities
- Document: legal basis for public task

Art. 6(1)(f) - LEGITIMATE INTERESTS:
- Three-part test required:
  1. Purpose test: Legitimate interest must be identified
  2. Necessity test: Processing must be necessary for that interest
  3. Balancing test: Interests must not be overridden by data subject's rights
- Requires documented Legitimate Interests Assessment (LIA)
- Document: the LIA and its conclusion

SPECIAL CATEGORY DATA (Art. 9):
If any processing involves special categories, you must ALSO identify an Art. 9(2) derogation:
(a) Explicit consent
(b) Employment/social security law obligation
(c) Vital interests where subject incapable of consenting
(d) Not-for-profit body processing of members
(e) Data manifestly made public
(f) Legal claims
(g) Substantial public interest
(h) Health/social care
(i) Public health
(j) Archiving/research/statistics

CONSERVATIVE APPROACH:
- If multiple bases could apply, recommend the most protective
- Flag where LIA is required but not conducted
- Note where consent mechanisms need review
- Highlight any processing that may lack valid basis`

	userMsg := fmt.Sprintf(`Conduct a lawful basis assessment for this organization's processing activities:

ORGANIZATION DETAILS:
- Name: %s
- Sector: %s
- Country: %s
- Employee count: %d
- Description: %s

DECLARED DATA SUBJECTS: %s

DECLARED PROCESSING PURPOSES: %s

TECH STACK & PROCESSOR PROFILES:
%s

REGULATORY SOURCES (use [N] notation to cite):
%s

For each identifiable processing activity:
1. Identify the most appropriate lawful basis
2. Provide detailed reasoning with citations
3. Note if LIA is required (for legitimate interests)
4. Flag any special category data and required Art. 9 derogation
5. Identify any risks or issues with the lawful basis selection`,
		client.Name,
		client.Sector,
		client.Country,
		client.EmployeeCount,
		client.Description,
		strings.Join(client.DataSubjects, ", "),
		strings.Join(client.ProcessingPurposes, ", "),
		processors,
		sources,
	)

	return PromptPair{
		System: system,
		Messages: []providers.Message{
			{Role: "user", Content: userMsg},
		},
	}
}

// aiActPrompt generates the prompt for EU AI Act risk classification.
func aiActPrompt(client ClientContext, sources string) PromptPair {
	system := `You are an EU AI Act compliance expert conducting an AI system risk classification assessment per Regulation (EU) 2024/1689.

OUTPUT FORMAT:
You MUST output ONLY valid JSON matching the AIActClassification schema. No markdown formatting, no code blocks, no commentary outside JSON. Start directly with { and end with }.

CITATION RULES:
- Reference sources using [N] notation matching the numbered sources provided.
- Cite specific AI Act articles and annexes.
- Reference recitals for interpretive guidance.

AI ACT RISK CATEGORIES:

1. UNACCEPTABLE RISK (Article 5) - PROHIBITED:
- Social scoring by public authorities
- Real-time remote biometric identification in public spaces (with limited exceptions)
- Exploitation of vulnerabilities (age, disability, social/economic situation)
- Subliminal manipulation causing harm
- Emotion recognition in workplace/education (with limited exceptions)
- Biometric categorization inferring sensitive attributes
- Untargeted scraping for facial recognition databases

2. HIGH-RISK (Article 6, Annex III):
Annex III lists high-risk AI systems in these areas:
(1) Biometric identification and categorization
(2) Management of critical infrastructure
(3) Education and vocational training (access, evaluation, proctoring)
(4) Employment (recruitment, task allocation, evaluation, termination)
(5) Access to essential services (credit scoring, emergency services)
(6) Law enforcement
(7) Migration, asylum, border control
(8) Administration of justice and democratic processes

High-risk systems embedded in regulated products (Annex I) also captured.

REQUIREMENTS FOR HIGH-RISK (Chapter III, Section 2):
- Risk management system (Art. 9)
- Data governance (Art. 10)
- Technical documentation (Art. 11)
- Record-keeping (Art. 12)
- Transparency and information to deployers (Art. 13)
- Human oversight (Art. 14)
- Accuracy, robustness, cybersecurity (Art. 15)
- Conformity assessment (Art. 43)
- EU database registration (Art. 49)

3. LIMITED RISK (Article 50) - TRANSPARENCY OBLIGATIONS:
- AI systems interacting with persons (chatbots)
- Emotion recognition systems
- Biometric categorization systems
- AI-generated content (deepfakes, synthetic media)

Requirements: Clear disclosure that AI is being used, content is AI-generated, etc.

4. MINIMAL RISK:
- No specific obligations under AI Act
- Voluntary codes of conduct encouraged

GENERAL-PURPOSE AI (GPAI) MODELS (Chapter V):
If the organization uses or provides GPAI models:
- Transparency requirements (Art. 53)
- Systemic risk assessment for high-impact models (Art. 55)

TIMELINES:
- Prohibited practices: February 2025
- GPAI obligations: August 2025
- High-risk systems (Annex III): August 2026
- High-risk in Annex I products: August 2027

INTERACTION WITH GDPR:
- AI Act does NOT replace GDPR obligations
- Personal data processing must comply with both regulations
- Highlight where DPIA may be required for AI systems

CONSERVATIVE APPROACH:
- When classification is uncertain, lean toward higher risk category
- Flag borderline cases for legal review
- Note any systems that may become high-risk as AI Act interpretation evolves`

	userMsg := fmt.Sprintf(`Conduct an EU AI Act risk classification for this organization:

ORGANIZATION DETAILS:
- Name: %s
- Sector: %s
- Country: %s
- Description: %s

REGULATORY SOURCES (use [N] notation to cite):
%s

Identify ALL AI components and systems used or developed by this organization based on the description and sector. For each AI component:
1. Describe the system and its purpose
2. Classify into risk category (unacceptable/high/limited/minimal)
3. Cite the specific AI Act provision for classification (Article/Annex)
4. List applicable obligations
5. Identify compliance timeline
6. List transparency requirements
7. Provide practical recommendations

End with a summary of the organization's overall AI Act compliance posture.`,
		client.Name,
		client.Sector,
		client.Country,
		client.Description,
		sources,
	)

	return PromptPair{
		System: system,
		Messages: []providers.Message{
			{Role: "user", Content: userMsg},
		},
	}
}
