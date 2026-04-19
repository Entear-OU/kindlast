// Package artifact defines the structured JSON schemas for DPO Copilot artifacts.
// These types are used to generate and validate compliance deliverables such as
// RoPAs (Records of Processing Activities), DPIA Screenings, DPA Gap Analyses,
// and AI Act Classifications.
package artifact

// RoPA represents a Record of Processing Activities under Article 30 GDPR.
// It documents all processing activities performed by an organization.
type RoPA struct {
	OrganizationName string               `json:"organization_name"`
	DPOName          string               `json:"dpo_name,omitempty"`
	GeneratedDate    string               `json:"generated_date"`
	Activities       []ProcessingActivity `json:"activities"`
}

// ProcessingActivity represents a single data processing activity within a RoPA.
type ProcessingActivity struct {
	ID                 string           `json:"id"`                   // e.g., "PA-001"
	Name               string           `json:"name"`                 // e.g., "Email marketing via HubSpot"
	Purpose            string           `json:"purpose"`
	LawfulBasis        LawfulBasisEntry `json:"lawful_basis"`
	DataCategories     []string         `json:"data_categories"`      // e.g., ["email", "name", "purchase_history"]
	DataSubjects       []string         `json:"data_subjects"`        // e.g., ["customers"]
	Recipients         []Recipient      `json:"recipients"`
	Transfers          []Transfer       `json:"transfers,omitempty"`
	RetentionPeriod    string           `json:"retention_period"`     // e.g., "24 months after last interaction"
	RetentionRationale string           `json:"retention_rationale"`
	SecurityMeasures   []string         `json:"security_measures"`
	DPIARequired       bool             `json:"dpia_required"`
	DPIARationale      string           `json:"dpia_rationale"`
	Notes              string           `json:"notes,omitempty"`
	Citations          []int            `json:"citations"`            // indices into artifact citations array
}

// LawfulBasisEntry represents the lawful basis for a processing activity.
type LawfulBasisEntry struct {
	Basis       string `json:"basis"`        // "consent" | "contract" | "legal_obligation" | "vital_interests" | "public_task" | "legitimate_interests"
	Article     string `json:"article"`      // e.g., "Art. 6(1)(a)"
	Reasoning   string `json:"reasoning"`    // why this basis applies
	LIARequired bool   `json:"lia_required"` // true if legitimate interests; needs balancing test
}

// Recipient represents a data recipient (processor, controller, or joint controller).
type Recipient struct {
	Name      string `json:"name"`       // e.g., "HubSpot Inc."
	Role      string `json:"role"`       // "processor" | "controller" | "joint_controller"
	Purpose   string `json:"purpose"`
	DPAStatus string `json:"dpa_status"` // "in_place" | "needed" | "unknown"
}

// Transfer represents an international data transfer.
type Transfer struct {
	Destination string `json:"destination"` // e.g., "US"
	Mechanism   string `json:"mechanism"`   // "scc" | "dpf" | "adequacy" | "derogation"
	Notes       string `json:"notes"`
}

// DPIAScreening represents a DPIA pre-assessment under Article 35 GDPR.
// It evaluates whether a full DPIA is required based on EDPB criteria.
type DPIAScreening struct {
	ClientName       string              `json:"client_name"`
	GeneratedDate    string              `json:"generated_date"`
	ScreeningResult  string              `json:"screening_result"`  // "required" | "recommended" | "not_required"
	OverallRationale string              `json:"overall_rationale"`
	Activities       []DPIAActivityCheck `json:"activities"`
	EDPBCriteria     []CriterionCheck    `json:"edpb_criteria"`     // 9 EDPB criteria checked
	Recommendations  []string            `json:"recommendations"`
	Citations        []int               `json:"citations"`
}

// DPIAActivityCheck represents the DPIA screening result for a single activity.
type DPIAActivityCheck struct {
	ActivityName    string   `json:"activity_name"`
	RiskLevel       string   `json:"risk_level"`       // "high" | "medium" | "low"
	TriggerCriteria []string `json:"trigger_criteria"` // which EDPB criteria triggered
	Rationale       string   `json:"rationale"`
	RequiresDPIA    bool     `json:"requires_dpia"`
}

// CriterionCheck represents the evaluation of a single EDPB criterion.
type CriterionCheck struct {
	Number    int    `json:"number"`    // 1-9
	Name      string `json:"name"`      // e.g., "Evaluation or scoring"
	Triggered bool   `json:"triggered"`
	Evidence  string `json:"evidence"`  // why it was triggered or not
}

// DPAGapAnalysis represents an analysis of Data Processing Agreement coverage.
type DPAGapAnalysis struct {
	ClientName    string        `json:"client_name"`
	GeneratedDate string        `json:"generated_date"`
	Processors    []DPACheck    `json:"processors"`
	Summary       DPAGapSummary `json:"summary"`
	Citations     []int         `json:"citations"`
}

// DPACheck represents the DPA status check for a single processor.
type DPACheck struct {
	ProcessorName     string   `json:"processor_name"`
	Category          string   `json:"category"`
	DataCategories    []string `json:"data_categories"`
	Headquarters      string   `json:"headquarters"`
	DPAStatus         string   `json:"dpa_status"`           // "in_place" | "needed" | "unknown"
	DPAPublicURL      string   `json:"dpa_public_url,omitempty"`
	TransferRequired  bool     `json:"transfer_required"`
	TransferMechanism string   `json:"transfer_mechanism,omitempty"`
	TIARequired       bool     `json:"tia_required"`         // Transfer Impact Assessment
	SCCType           string   `json:"scc_type,omitempty"`   // "module_2" (controller-to-processor) etc.
	Actions           []string `json:"actions"`              // what DPO needs to do
}

// DPAGapSummary provides aggregate statistics for the DPA gap analysis.
type DPAGapSummary struct {
	TotalProcessors   int `json:"total_processors"`
	DPAsInPlace       int `json:"dpas_in_place"`
	DPAsNeeded        int `json:"dpas_needed"`
	TransfersRequired int `json:"transfers_required"`
	TIAsRequired      int `json:"tias_required"`
}

// AIActClassification represents an EU AI Act risk classification assessment.
type AIActClassification struct {
	ClientName    string        `json:"client_name"`
	GeneratedDate string        `json:"generated_date"`
	AIComponents  []AIComponent `json:"ai_components"`
	Summary       string        `json:"summary"`
	Citations     []int         `json:"citations"`
}

// AIComponent represents a single AI system component and its classification.
type AIComponent struct {
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	RiskCategory        string   `json:"risk_category"`        // "unacceptable" | "high" | "limited" | "minimal"
	ClassificationBasis string   `json:"classification_basis"` // which Annex/Article
	Obligations         []string `json:"obligations"`
	Timeline            string   `json:"timeline"`             // when obligations apply
	TransparencyReqs    []string `json:"transparency_reqs"`
	Recommendations     []string `json:"recommendations"`
}

// ArtifactType represents the type of compliance artifact.
type ArtifactType string

const (
	ArtifactTypeRoPA              ArtifactType = "ropa"
	ArtifactTypeDPIAScreening     ArtifactType = "dpia_screening"
	ArtifactTypeDPAGap            ArtifactType = "dpa_gap"
	ArtifactTypeLawfulBasis       ArtifactType = "lawful_basis"
	ArtifactTypeAIActClassification ArtifactType = "ai_act_classification"
)

// ArtifactStatus represents the workflow status of an artifact.
type ArtifactStatus string

const (
	ArtifactStatusDraft    ArtifactStatus = "draft"
	ArtifactStatusReviewed ArtifactStatus = "reviewed"
	ArtifactStatusApproved ArtifactStatus = "approved"
	ArtifactStatusExported ArtifactStatus = "exported"
)

// LawfulBasis represents valid GDPR Article 6(1) lawful bases.
type LawfulBasis string

const (
	LawfulBasisConsent             LawfulBasis = "consent"
	LawfulBasisContract            LawfulBasis = "contract"
	LawfulBasisLegalObligation     LawfulBasis = "legal_obligation"
	LawfulBasisVitalInterests      LawfulBasis = "vital_interests"
	LawfulBasisPublicTask          LawfulBasis = "public_task"
	LawfulBasisLegitimateInterests LawfulBasis = "legitimate_interests"
)

// RecipientRole represents the role of a data recipient.
type RecipientRole string

const (
	RecipientRoleProcessor       RecipientRole = "processor"
	RecipientRoleController      RecipientRole = "controller"
	RecipientRoleJointController RecipientRole = "joint_controller"
)

// TransferMechanism represents valid international transfer mechanisms.
type TransferMechanism string

const (
	TransferMechanismSCC       TransferMechanism = "scc"
	TransferMechanismDPF       TransferMechanism = "dpf"
	TransferMechanismAdequacy  TransferMechanism = "adequacy"
	TransferMechanismDerogation TransferMechanism = "derogation"
)

// DPAStatusValue represents the status of a Data Processing Agreement.
type DPAStatusValue string

const (
	DPAStatusInPlace DPAStatusValue = "in_place"
	DPAStatusNeeded  DPAStatusValue = "needed"
	DPAStatusUnknown DPAStatusValue = "unknown"
)

// DPIAScreeningResult represents the outcome of a DPIA screening.
type DPIAScreeningResult string

const (
	DPIAScreeningResultRequired    DPIAScreeningResult = "required"
	DPIAScreeningResultRecommended DPIAScreeningResult = "recommended"
	DPIAScreeningResultNotRequired DPIAScreeningResult = "not_required"
)

// RiskLevel represents risk levels used in DPIA and AI Act assessments.
type RiskLevel string

const (
	RiskLevelHigh   RiskLevel = "high"
	RiskLevelMedium RiskLevel = "medium"
	RiskLevelLow    RiskLevel = "low"
)

// AIActRiskCategory represents EU AI Act risk categories.
type AIActRiskCategory string

const (
	AIActRiskCategoryUnacceptable AIActRiskCategory = "unacceptable"
	AIActRiskCategoryHigh         AIActRiskCategory = "high"
	AIActRiskCategoryLimited      AIActRiskCategory = "limited"
	AIActRiskCategoryMinimal      AIActRiskCategory = "minimal"
)

// Citation represents a reference to a regulatory source used in artifact generation.
type Citation struct {
	Index     int    `json:"index"`
	SourceURL string `json:"source_url"`
	Title     string `json:"title"`
	Section   string `json:"section,omitempty"`
	ChunkText string `json:"chunk_text,omitempty"`
}

// GenerationMeta contains metadata about how an artifact was generated.
type GenerationMeta struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	TokensUsed    int    `json:"tokens_used"`
	LatencyMs     int64  `json:"latency_ms"`
	CorpusVersion string `json:"corpus_version"`
}
