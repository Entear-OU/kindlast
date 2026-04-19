package models

import "time"

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
