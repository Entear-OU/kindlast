package models

import (
	"encoding/json"
	"time"
)

// User represents a user account
type User struct {
	ID           string    `json:"id" db:"id"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"`
	FullName     string    `json:"full_name" db:"full_name"`
	Plan         string    `json:"plan" db:"plan"` // free, professional, enterprise
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// UserProfile is the public representation of a user
type UserProfile struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	FullName  string    `json:"full_name"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"created_at"`
}

// RegisterRequest represents registration payload
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
}

// LoginRequest represents login payload
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RefreshRequest represents token refresh payload
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// AuthResponse represents authentication response
type AuthResponse struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	User         UserProfile `json:"user"`
}

// UpdateProfileRequest represents profile update payload
type UpdateProfileRequest struct {
	FullName string `json:"full_name,omitempty"`
}

// PlanDetails represents subscription plan information
type PlanDetails struct {
	Plan            string `json:"plan"`
	QueriesPerMonth int    `json:"queries_per_month"`
	QueriesUsed     int    `json:"queries_used"`
	RateLimitPerMin int    `json:"rate_limit_per_min"`
}

// ErrorResponse represents error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status     string            `json:"status"`
	Version    string            `json:"version"`
	Components map[string]string `json:"components"`
	Timestamp  time.Time         `json:"timestamp"`
}

// StatusResponse represents service status
type StatusResponse struct {
	Service   string            `json:"service"`
	Status    string            `json:"status"`
	Health    map[string]bool   `json:"health"`
	Timestamp time.Time         `json:"timestamp"`
}

// QueryRequest represents a RAG query request
type QueryRequest struct {
	Query   string                 `json:"query"`
	Options map[string]interface{} `json:"options,omitempty"`
}

// Plan types
const (
	PlanFree         = "free"
	PlanProfessional = "professional"
	PlanTeam         = "team"
)

// PlanLimit represents limits for a subscription plan
type PlanLimit struct {
	RequestsPerHour int
	MaxCitations    int
	QueriesPerMonth int // For backward compatibility with existing middleware
	RateLimitPerMin int // For backward compatibility with existing middleware
}

// Plan limits
var PlanLimits = map[string]PlanLimit{
	PlanFree: {
		RequestsPerHour: 20,
		MaxCitations:    3,
		QueriesPerMonth: 100,
		RateLimitPerMin: 5,
	},
	PlanProfessional: {
		RequestsPerHour: 500,
		MaxCitations:    -1,
		QueriesPerMonth: 1000,
		RateLimitPerMin: 20,
	},
	PlanTeam: {
		RequestsPerHour: 5000,
		MaxCitations:    -1,
		QueriesPerMonth: -1,
		RateLimitPerMin: 100,
	},
}

// =============================================
// DPO COPILOT MODELS
// =============================================

// Client represents a DPO's client organization
type Client struct {
	ID                 string    `json:"id" db:"id"`
	UserID             string    `json:"user_id" db:"user_id"`
	Name               string    `json:"name" db:"name"`
	Description        string    `json:"description,omitempty" db:"description"`
	Sector             string    `json:"sector,omitempty" db:"sector"`
	Country            string    `json:"country,omitempty" db:"country"`
	EmployeeCount      int       `json:"employee_count,omitempty" db:"employee_count"`
	TechStack          []string  `json:"tech_stack" db:"tech_stack"`
	DataSubjects       []string  `json:"data_subjects" db:"data_subjects"`
	ProcessingPurposes []string  `json:"processing_purposes" db:"processing_purposes"`
	Status             string    `json:"status" db:"status"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`
}

// Client statuses
const (
	ClientStatusActive   = "active"
	ClientStatusArchived = "archived"
)

// CreateClientRequest represents the request to create a client
type CreateClientRequest struct {
	Name               string   `json:"name"`
	Description        string   `json:"description,omitempty"`
	Sector             string   `json:"sector,omitempty"`
	Country            string   `json:"country,omitempty"`
	EmployeeCount      int      `json:"employee_count,omitempty"`
	TechStack          []string `json:"tech_stack,omitempty"`
	DataSubjects       []string `json:"data_subjects,omitempty"`
	ProcessingPurposes []string `json:"processing_purposes,omitempty"`
}

// UpdateClientRequest represents the request to update a client
type UpdateClientRequest struct {
	Name               *string  `json:"name,omitempty"`
	Description        *string  `json:"description,omitempty"`
	Sector             *string  `json:"sector,omitempty"`
	Country            *string  `json:"country,omitempty"`
	EmployeeCount      *int     `json:"employee_count,omitempty"`
	TechStack          []string `json:"tech_stack,omitempty"`
	DataSubjects       []string `json:"data_subjects,omitempty"`
	ProcessingPurposes []string `json:"processing_purposes,omitempty"`
}

// ClientListResponse represents a paginated list of clients
type ClientListResponse struct {
	Clients    []Client `json:"clients"`
	Total      int      `json:"total"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
	TotalPages int      `json:"total_pages"`
}

// Artifact represents a generated compliance artifact
type Artifact struct {
	ID               string          `json:"id" db:"id"`
	ClientID         string          `json:"client_id" db:"client_id"`
	UserID           string          `json:"user_id" db:"user_id"`
	Type             string          `json:"type" db:"type"`
	Status           string          `json:"status" db:"status"`
	Title            string          `json:"title,omitempty" db:"title"`
	InputContext     string          `json:"input_context" db:"input_context"`
	GeneratedContent json.RawMessage `json:"generated_content" db:"generated_content"`
	EditedContent    json.RawMessage `json:"edited_content,omitempty" db:"edited_content"`
	Citations        json.RawMessage `json:"citations" db:"citations"`
	GenerationMeta   json.RawMessage `json:"generation_meta" db:"generation_meta"`
	Version          int             `json:"version" db:"version"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at" db:"updated_at"`
}

// Artifact types
const (
	ArtifactTypeRoPA              = "ropa"
	ArtifactTypeDPIAScreening     = "dpia_screening"
	ArtifactTypeDPAGap            = "dpa_gap"
	ArtifactTypeLawfulBasis       = "lawful_basis"
	ArtifactTypeAIActClassification = "ai_act_classification"
)

// Artifact statuses
const (
	ArtifactStatusDraft    = "draft"
	ArtifactStatusReviewed = "reviewed"
	ArtifactStatusApproved = "approved"
	ArtifactStatusExported = "exported"
)

// GenerateArtifactRequest represents the request to generate an artifact
type GenerateArtifactRequest struct {
	Type           string `json:"type"`
	AdditionalContext string `json:"additional_context,omitempty"`
}

// UpdateArtifactRequest represents the request to update an artifact
type UpdateArtifactRequest struct {
	Title         *string         `json:"title,omitempty"`
	EditedContent json.RawMessage `json:"edited_content,omitempty"`
}

// UpdateArtifactStatusRequest represents the request to update artifact status
type UpdateArtifactStatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// ExportArtifactRequest represents the request to export an artifact
type ExportArtifactRequest struct {
	Format string `json:"format"` // "pdf" or "docx"
}

// ArtifactListResponse represents a paginated list of artifacts
type ArtifactListResponse struct {
	Artifacts  []Artifact `json:"artifacts"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
	TotalPages int        `json:"total_pages"`
}

// GenerationMeta stores metadata about artifact generation
type GenerationMeta struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	TokensUsed    int    `json:"tokens_used"`
	LatencyMs     int64  `json:"latency_ms"`
	CorpusVersion string `json:"corpus_version"`
}

// Citation represents a regulatory source citation
type Citation struct {
	Index     int    `json:"index"`
	SourceURL string `json:"source_url"`
	Title     string `json:"title"`
	Section   string `json:"section,omitempty"`
	ChunkText string `json:"chunk_text,omitempty"`
}

// ProcessorProfile represents a SaaS processor profile
type ProcessorProfile struct {
	ID                 string    `json:"id" db:"id"`
	Name               string    `json:"name" db:"name"`
	Slug               string    `json:"slug" db:"slug"`
	Category           string    `json:"category,omitempty" db:"category"`
	Description        string    `json:"description,omitempty" db:"description"`
	Headquarters       string    `json:"headquarters,omitempty" db:"headquarters"`
	DataCategories     []string  `json:"data_categories" db:"data_categories"`
	ProcessingPurposes []string  `json:"processing_purposes" db:"processing_purposes"`
	DataLocations      []string  `json:"data_locations" db:"data_locations"`
	TransferMechanism  string    `json:"transfer_mechanism,omitempty" db:"transfer_mechanism"`
	DPAURL             string    `json:"dpa_url,omitempty" db:"dpa_url"`
	SubprocessorsURL   string    `json:"subprocessors_url,omitempty" db:"subprocessors_url"`
	GDPRPageURL        string    `json:"gdpr_page_url,omitempty" db:"gdpr_page_url"`
	Verified           bool      `json:"verified" db:"verified"`
	LastVerified       *time.Time `json:"last_verified,omitempty" db:"last_verified"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`
}

// ProcessorListResponse represents a paginated list of processor profiles
type ProcessorListResponse struct {
	Processors []ProcessorProfile `json:"processors"`
	Total      int                `json:"total"`
	Page       int                `json:"page"`
	PageSize   int                `json:"page_size"`
	TotalPages int                `json:"total_pages"`
}

// ArtifactAuditEntry represents an audit log entry for artifact operations
type ArtifactAuditEntry struct {
	ID            string          `json:"id" db:"id"`
	ArtifactID    string          `json:"artifact_id" db:"artifact_id"`
	UserID        string          `json:"user_id" db:"user_id"`
	Action        string          `json:"action" db:"action"`
	PreviousState json.RawMessage `json:"previous_state,omitempty" db:"previous_state"`
	NewState      json.RawMessage `json:"new_state,omitempty" db:"new_state"`
	Metadata      json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
}

// Audit actions
const (
	AuditActionGenerated     = "generated"
	AuditActionEdited        = "edited"
	AuditActionStatusChanged = "status_changed"
	AuditActionExported      = "exported"
	AuditActionDeleted       = "deleted"
)

// AuditMetadata stores additional information about audit entries
type AuditMetadata struct {
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// AuditListResponse represents a paginated list of audit entries
type AuditListResponse struct {
	Entries    []ArtifactAuditEntry `json:"entries"`
	Total      int                  `json:"total"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"page_size"`
	TotalPages int                  `json:"total_pages"`
}

// ArtifactVersion represents a version of an artifact
type ArtifactVersion struct {
	ID         string          `json:"id" db:"id"`
	ArtifactID string          `json:"artifact_id" db:"artifact_id"`
	Version    int             `json:"version" db:"version"`
	Content    json.RawMessage `json:"content" db:"content"`
	EditedBy   string          `json:"edited_by" db:"edited_by"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
}

// DPO Copilot Plan Limits
type DPOCopilotLimits struct {
	MaxClients          int  // Max client workspaces
	MaxArtifactsPerMonth int  // Max artifact generations per month
	ProcessorAccess     string // "limited" (top 10) or "full"
	AuditTrailEnabled   bool
	AuditRetentionMonths int // 0 = unlimited
	ExportEnabled       bool
	AIActModuleEnabled  bool
	TeamMembers         int
}

// DPO Copilot plan limits by tier
var DPOCopilotPlanLimits = map[string]DPOCopilotLimits{
	PlanFree: {
		MaxClients:           0,
		MaxArtifactsPerMonth: 0,
		ProcessorAccess:      "limited",
		AuditTrailEnabled:    false,
		AuditRetentionMonths: 0,
		ExportEnabled:        false,
		AIActModuleEnabled:   false,
		TeamMembers:          1,
	},
	PlanProfessional: {
		MaxClients:           20,
		MaxArtifactsPerMonth: 50,
		ProcessorAccess:      "full",
		AuditTrailEnabled:    true,
		AuditRetentionMonths: 12,
		ExportEnabled:        true,
		AIActModuleEnabled:   true,
		TeamMembers:          1,
	},
	PlanTeam: {
		MaxClients:           50,
		MaxArtifactsPerMonth: 200,
		ProcessorAccess:      "full",
		AuditTrailEnabled:    true,
		AuditRetentionMonths: 0, // unlimited
		ExportEnabled:        true,
		AIActModuleEnabled:   true,
		TeamMembers:          5,
	},
}

// =============================================
// SME SELF-ASSESSMENT MODELS
// =============================================

// BusinessProfile represents a user's company profile for compliance assessment
type BusinessProfile struct {
	ID                      string    `json:"id" db:"id"`
	UserID                  string    `json:"user_id" db:"user_id"`
	CompanyName             string    `json:"company_name,omitempty" db:"company_name"`
	Country                 string    `json:"country,omitempty" db:"country"`
	Industry                string    `json:"industry,omitempty" db:"industry"`
	EmployeeCount           string    `json:"employee_count,omitempty" db:"employee_count"`
	ProcessesPersonalData   bool      `json:"processes_personal_data" db:"processes_personal_data"`
	DataTypes               []string  `json:"data_types" db:"data_types"`
	UsesAISystems           bool      `json:"uses_ai_systems" db:"uses_ai_systems"`
	AISystemDescriptions    string    `json:"ai_system_descriptions,omitempty" db:"ai_system_descriptions"`
	ThirdPartyProcessors    []string  `json:"third_party_processors" db:"third_party_processors"`
	TransfersDataOutsideEU  bool      `json:"transfers_data_outside_eu" db:"transfers_data_outside_eu"`
	HasDPO                  bool      `json:"has_dpo" db:"has_dpo"`
	HasPrivacyPolicy        bool      `json:"has_privacy_policy" db:"has_privacy_policy"`
	HasCookieConsent        bool      `json:"has_cookie_consent" db:"has_cookie_consent"`
	HasBreachNotification   bool      `json:"has_breach_notification" db:"has_breach_notification"`
	HasDSRProcess           bool      `json:"has_dsr_process" db:"has_dsr_process"`
	CreatedAt               time.Time `json:"created_at" db:"created_at"`
	UpdatedAt               time.Time `json:"updated_at" db:"updated_at"`
}

// CreateBusinessProfileRequest represents the request to create/update a business profile
type CreateBusinessProfileRequest struct {
	CompanyName            string   `json:"company_name,omitempty"`
	Country                string   `json:"country,omitempty"`
	Industry               string   `json:"industry,omitempty"`
	EmployeeCount          string   `json:"employee_count,omitempty"`
	ProcessesPersonalData  bool     `json:"processes_personal_data"`
	DataTypes              []string `json:"data_types,omitempty"`
	UsesAISystems          bool     `json:"uses_ai_systems"`
	AISystemDescriptions   string   `json:"ai_system_descriptions,omitempty"`
	ThirdPartyProcessors   []string `json:"third_party_processors,omitempty"`
	TransfersDataOutsideEU bool     `json:"transfers_data_outside_eu"`
	HasDPO                 bool     `json:"has_dpo"`
	HasPrivacyPolicy       bool     `json:"has_privacy_policy"`
	HasCookieConsent       bool     `json:"has_cookie_consent"`
	HasBreachNotification  bool     `json:"has_breach_notification"`
	HasDSRProcess          bool     `json:"has_dsr_process"`
}

// Assessment represents a compliance assessment result
type Assessment struct {
	ID           string          `json:"id" db:"id"`
	UserID       string          `json:"user_id" db:"user_id"`
	ProfileID    string          `json:"profile_id,omitempty" db:"profile_id"`
	Type         string          `json:"type" db:"type"`
	Status       string          `json:"status" db:"status"`
	OverallScore *int            `json:"overall_score,omitempty" db:"overall_score"`
	RiskLevel    string          `json:"risk_level,omitempty" db:"risk_level"`
	Result       json.RawMessage `json:"result,omitempty" db:"result"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
}

// Assessment types
const (
	AssessmentTypeGDPR  = "gdpr"
	AssessmentTypeAIAct = "ai_act"
)

// Assessment statuses
const (
	AssessmentStatusPending    = "pending"
	AssessmentStatusProcessing = "processing"
	AssessmentStatusComplete   = "complete"
	AssessmentStatusError      = "error"
)

// CreateAssessmentRequest represents the request to create an assessment
type CreateAssessmentRequest struct {
	Type string `json:"type"`
}

// AssessmentListResponse represents a paginated list of assessments
type AssessmentListResponse struct {
	Assessments []Assessment `json:"assessments"`
	Total       int          `json:"total"`
	Page        int          `json:"page"`
	PageSize    int          `json:"page_size"`
	TotalPages  int          `json:"total_pages"`
}

// Finding represents a compliance finding from an assessment
type Finding struct {
	ID             string     `json:"id" db:"id"`
	AssessmentID   string     `json:"assessment_id" db:"assessment_id"`
	UserID         string     `json:"user_id" db:"user_id"`
	Category       string     `json:"category,omitempty" db:"category"`
	Severity       string     `json:"severity,omitempty" db:"severity"`
	Title          string     `json:"title" db:"title"`
	Description    string     `json:"description,omitempty" db:"description"`
	Recommendation string     `json:"recommendation,omitempty" db:"recommendation"`
	GDPRArticle    string     `json:"gdpr_article,omitempty" db:"gdpr_article"`
	AIActArticle   string     `json:"ai_act_article,omitempty" db:"ai_act_article"`
	IsResolved     bool       `json:"is_resolved" db:"is_resolved"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty" db:"resolved_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

// Finding severities
const (
	FindingSeverityCritical = "critical"
	FindingSeverityHigh     = "high"
	FindingSeverityMedium   = "medium"
	FindingSeverityLow      = "low"
	FindingSeverityInfo     = "info"
)

// UpdateFindingRequest represents the request to update a finding
type UpdateFindingRequest struct {
	IsResolved *bool `json:"is_resolved,omitempty"`
}

// FindingListResponse represents a list of findings
type FindingListResponse struct {
	Findings   []Finding `json:"findings"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	PageSize   int       `json:"page_size"`
	TotalPages int       `json:"total_pages"`
}
