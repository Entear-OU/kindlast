package artifact

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// ProcessorRepository defines the interface for processor profile data access.
// Implementations may use PostgreSQL for structured queries or Qdrant for fuzzy matching.
type ProcessorRepository interface {
	// GetBySlug retrieves a processor profile by its slug (e.g., "stripe", "hubspot").
	GetBySlug(ctx context.Context, slug string) (*ProcessorProfileData, error)

	// GetByName retrieves a processor profile by its display name (e.g., "Stripe", "HubSpot").
	GetByName(ctx context.Context, name string) (*ProcessorProfileData, error)

	// GetByCategory retrieves all processor profiles in a category (e.g., "payment", "crm").
	GetByCategory(ctx context.Context, category string) ([]ProcessorProfileData, error)

	// Search performs a case-insensitive search across processor names and categories.
	Search(ctx context.Context, query string, limit int) ([]ProcessorProfileData, error)

	// List retrieves all processor profiles with pagination.
	List(ctx context.Context, offset, limit int) ([]ProcessorProfileData, error)

	// Count returns the total number of processor profiles.
	Count(ctx context.Context) (int, error)

	// Close closes any underlying connections.
	Close() error
}

// PostgresProcessorRepository implements ProcessorRepository using PostgreSQL.
type PostgresProcessorRepository struct {
	db *sql.DB
}

// NewPostgresProcessorRepository creates a new PostgreSQL-backed processor repository.
func NewPostgresProcessorRepository(dsn string) (*PostgresProcessorRepository, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &PostgresProcessorRepository{db: db}, nil
}

// NewPostgresProcessorRepositoryFromDB creates a repository from an existing database connection.
func NewPostgresProcessorRepositoryFromDB(db *sql.DB) *PostgresProcessorRepository {
	return &PostgresProcessorRepository{db: db}
}

// Close closes the database connection.
func (r *PostgresProcessorRepository) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

// GetBySlug retrieves a processor profile by its slug.
func (r *PostgresProcessorRepository) GetBySlug(ctx context.Context, slug string) (*ProcessorProfileData, error) {
	query := `
		SELECT
			name, slug, category, headquarters,
			data_categories, processing_purposes, data_locations,
			transfer_mechanism, dpa_url, subprocessors_url, gdpr_page_url,
			verified, last_verified
		FROM processor_profiles
		WHERE slug = $1
	`

	return r.scanProcessor(ctx, query, slug)
}

// GetByName retrieves a processor profile by its display name (case-insensitive).
func (r *PostgresProcessorRepository) GetByName(ctx context.Context, name string) (*ProcessorProfileData, error) {
	query := `
		SELECT
			name, slug, category, headquarters,
			data_categories, processing_purposes, data_locations,
			transfer_mechanism, dpa_url, subprocessors_url, gdpr_page_url,
			verified, last_verified
		FROM processor_profiles
		WHERE LOWER(name) = LOWER($1)
	`

	return r.scanProcessor(ctx, query, name)
}

// GetByCategory retrieves all processor profiles in a category.
func (r *PostgresProcessorRepository) GetByCategory(ctx context.Context, category string) ([]ProcessorProfileData, error) {
	query := `
		SELECT
			name, slug, category, headquarters,
			data_categories, processing_purposes, data_locations,
			transfer_mechanism, dpa_url, subprocessors_url, gdpr_page_url,
			verified, last_verified
		FROM processor_profiles
		WHERE category = $1
		ORDER BY name ASC
	`

	return r.scanProcessors(ctx, query, category)
}

// Search performs a case-insensitive search across processor names and categories.
func (r *PostgresProcessorRepository) Search(ctx context.Context, query string, limit int) ([]ProcessorProfileData, error) {
	if limit <= 0 {
		limit = 20
	}

	sqlQuery := `
		SELECT
			name, slug, category, headquarters,
			data_categories, processing_purposes, data_locations,
			transfer_mechanism, dpa_url, subprocessors_url, gdpr_page_url,
			verified, last_verified
		FROM processor_profiles
		WHERE
			LOWER(name) LIKE LOWER($1) OR
			LOWER(category) LIKE LOWER($1) OR
			LOWER(slug) LIKE LOWER($1)
		ORDER BY
			CASE WHEN LOWER(name) = LOWER($2) THEN 0
			     WHEN LOWER(slug) = LOWER($2) THEN 1
			     ELSE 2 END,
			name ASC
		LIMIT $3
	`

	searchPattern := "%" + query + "%"
	return r.scanProcessors(ctx, sqlQuery, searchPattern, query, limit)
}

// List retrieves all processor profiles with pagination.
func (r *PostgresProcessorRepository) List(ctx context.Context, offset, limit int) ([]ProcessorProfileData, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT
			name, slug, category, headquarters,
			data_categories, processing_purposes, data_locations,
			transfer_mechanism, dpa_url, subprocessors_url, gdpr_page_url,
			verified, last_verified
		FROM processor_profiles
		ORDER BY name ASC
		LIMIT $1 OFFSET $2
	`

	return r.scanProcessors(ctx, query, limit, offset)
}

// Count returns the total number of processor profiles.
func (r *PostgresProcessorRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM processor_profiles").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count processors: %w", err)
	}
	return count, nil
}

// scanProcessor scans a single processor profile from a query.
func (r *PostgresProcessorRepository) scanProcessor(ctx context.Context, query string, args ...interface{}) (*ProcessorProfileData, error) {
	var p ProcessorProfileData
	var dataCategoriesJSON, processingPurposesJSON, dataLocationsJSON []byte
	var dpaURL, subprocessorsURL, gdprPageURL sql.NullString
	var verified sql.NullBool
	var lastVerified sql.NullTime

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&p.Name,
		&p.Slug,
		&p.Category,
		&p.Headquarters,
		&dataCategoriesJSON,
		&processingPurposesJSON,
		&dataLocationsJSON,
		&p.TransferMechanism,
		&dpaURL,
		&subprocessorsURL,
		&gdprPageURL,
		&verified,
		&lastVerified,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan processor: %w", err)
	}

	// Parse JSON arrays
	if len(dataCategoriesJSON) > 0 {
		if err := json.Unmarshal(dataCategoriesJSON, &p.DataCategories); err != nil {
			p.DataCategories = []string{}
		}
	}
	if len(processingPurposesJSON) > 0 {
		if err := json.Unmarshal(processingPurposesJSON, &p.ProcessingPurposes); err != nil {
			p.ProcessingPurposes = []string{}
		}
	}
	if len(dataLocationsJSON) > 0 {
		if err := json.Unmarshal(dataLocationsJSON, &p.DataLocations); err != nil {
			p.DataLocations = []string{}
		}
	}

	// Handle nullable fields
	if dpaURL.Valid {
		p.DPAURL = dpaURL.String
	}

	// Determine DPA status based on whether DPA URL exists
	if p.DPAURL != "" {
		p.DPAStatus = "in_place"
	} else {
		p.DPAStatus = "unknown"
	}

	return &p, nil
}

// scanProcessors scans multiple processor profiles from a query.
func (r *PostgresProcessorRepository) scanProcessors(ctx context.Context, query string, args ...interface{}) ([]ProcessorProfileData, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query processors: %w", err)
	}
	defer rows.Close()

	var processors []ProcessorProfileData
	for rows.Next() {
		var p ProcessorProfileData
		var dataCategoriesJSON, processingPurposesJSON, dataLocationsJSON []byte
		var dpaURL, subprocessorsURL, gdprPageURL sql.NullString
		var verified sql.NullBool
		var lastVerified sql.NullTime

		err := rows.Scan(
			&p.Name,
			&p.Slug,
			&p.Category,
			&p.Headquarters,
			&dataCategoriesJSON,
			&processingPurposesJSON,
			&dataLocationsJSON,
			&p.TransferMechanism,
			&dpaURL,
			&subprocessorsURL,
			&gdprPageURL,
			&verified,
			&lastVerified,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan processor: %w", err)
		}

		// Parse JSON arrays
		if len(dataCategoriesJSON) > 0 {
			if err := json.Unmarshal(dataCategoriesJSON, &p.DataCategories); err != nil {
				p.DataCategories = []string{}
			}
		}
		if len(processingPurposesJSON) > 0 {
			if err := json.Unmarshal(processingPurposesJSON, &p.ProcessingPurposes); err != nil {
				p.ProcessingPurposes = []string{}
			}
		}
		if len(dataLocationsJSON) > 0 {
			if err := json.Unmarshal(dataLocationsJSON, &p.DataLocations); err != nil {
				p.DataLocations = []string{}
			}
		}

		// Handle nullable fields
		if dpaURL.Valid {
			p.DPAURL = dpaURL.String
		}

		// Determine DPA status based on whether DPA URL exists
		if p.DPAURL != "" {
			p.DPAStatus = "in_place"
		} else {
			p.DPAStatus = "unknown"
		}

		processors = append(processors, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating processors: %w", err)
	}

	return processors, nil
}

// InMemoryProcessorRepository is a test implementation of ProcessorRepository
// that stores processor profiles in memory.
type InMemoryProcessorRepository struct {
	processors map[string]ProcessorProfileData
}

// NewInMemoryProcessorRepository creates a new in-memory processor repository.
func NewInMemoryProcessorRepository() *InMemoryProcessorRepository {
	return &InMemoryProcessorRepository{
		processors: make(map[string]ProcessorProfileData),
	}
}

// Add adds a processor profile to the repository.
func (r *InMemoryProcessorRepository) Add(p ProcessorProfileData) {
	r.processors[p.Slug] = p
}

// GetBySlug retrieves a processor profile by its slug.
func (r *InMemoryProcessorRepository) GetBySlug(ctx context.Context, slug string) (*ProcessorProfileData, error) {
	if p, ok := r.processors[slug]; ok {
		return &p, nil
	}
	return nil, nil
}

// GetByName retrieves a processor profile by its display name.
func (r *InMemoryProcessorRepository) GetByName(ctx context.Context, name string) (*ProcessorProfileData, error) {
	lowerName := strings.ToLower(name)
	for _, p := range r.processors {
		if strings.ToLower(p.Name) == lowerName {
			return &p, nil
		}
	}
	return nil, nil
}

// GetByCategory retrieves all processor profiles in a category.
func (r *InMemoryProcessorRepository) GetByCategory(ctx context.Context, category string) ([]ProcessorProfileData, error) {
	var result []ProcessorProfileData
	for _, p := range r.processors {
		if p.Category == category {
			result = append(result, p)
		}
	}
	return result, nil
}

// Search performs a case-insensitive search across processor names and categories.
func (r *InMemoryProcessorRepository) Search(ctx context.Context, query string, limit int) ([]ProcessorProfileData, error) {
	lowerQuery := strings.ToLower(query)
	var result []ProcessorProfileData
	for _, p := range r.processors {
		if strings.Contains(strings.ToLower(p.Name), lowerQuery) ||
			strings.Contains(strings.ToLower(p.Category), lowerQuery) ||
			strings.Contains(strings.ToLower(p.Slug), lowerQuery) {
			result = append(result, p)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

// List retrieves all processor profiles with pagination.
func (r *InMemoryProcessorRepository) List(ctx context.Context, offset, limit int) ([]ProcessorProfileData, error) {
	var all []ProcessorProfileData
	for _, p := range r.processors {
		all = append(all, p)
	}

	if offset >= len(all) {
		return []ProcessorProfileData{}, nil
	}

	end := offset + limit
	if end > len(all) {
		end = len(all)
	}

	return all[offset:end], nil
}

// Count returns the total number of processor profiles.
func (r *InMemoryProcessorRepository) Count(ctx context.Context) (int, error) {
	return len(r.processors), nil
}

// Close is a no-op for the in-memory repository.
func (r *InMemoryProcessorRepository) Close() error {
	return nil
}

// SeedCommonProcessors seeds the repository with common SaaS processor profiles.
// This is useful for testing and development.
func (r *InMemoryProcessorRepository) SeedCommonProcessors() {
	processors := []ProcessorProfileData{
		{
			Name:              "Stripe",
			Slug:              "stripe",
			Category:          "payment",
			Headquarters:      "US",
			DataCategories:    []string{"name", "email", "payment_card", "billing_address", "ip_address", "transaction_history"},
			ProcessingPurposes: []string{"payment_processing", "fraud_detection", "regulatory_compliance"},
			DataLocations:     []string{"us", "eu"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://stripe.com/legal/dpa",
		},
		{
			Name:              "HubSpot",
			Slug:              "hubspot",
			Category:          "crm",
			Headquarters:      "US",
			DataCategories:    []string{"name", "email", "phone", "company", "website_activity", "email_engagement"},
			ProcessingPurposes: []string{"crm", "email_marketing", "analytics", "customer_support"},
			DataLocations:     []string{"us", "eu", "de"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://legal.hubspot.com/dpa",
		},
		{
			Name:              "Amazon Web Services",
			Slug:              "aws",
			Category:          "cloud_infrastructure",
			Headquarters:      "US",
			DataCategories:    []string{"varies_by_service"},
			ProcessingPurposes: []string{"hosting", "storage", "compute", "database"},
			DataLocations:     []string{"global"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://d1.awsstatic.com/legal/aws-gdpr/AWS_GDPR_DPA.pdf",
		},
		{
			Name:              "Google Workspace",
			Slug:              "google-workspace",
			Category:          "productivity",
			Headquarters:      "US",
			DataCategories:    []string{"email_content", "documents", "calendar", "name", "email", "usage_data"},
			ProcessingPurposes: []string{"email", "document_collaboration", "calendar", "storage"},
			DataLocations:     []string{"global", "eu"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://workspace.google.com/terms/dpa_terms.html",
		},
		{
			Name:              "Intercom",
			Slug:              "intercom",
			Category:          "customer_support",
			Headquarters:      "US",
			DataCategories:    []string{"name", "email", "conversation_history", "usage_data", "ip_address"},
			ProcessingPurposes: []string{"customer_support", "product_messaging", "analytics"},
			DataLocations:     []string{"us", "eu"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://www.intercom.com/legal/data-processing-agreement",
		},
		{
			Name:              "Salesforce",
			Slug:              "salesforce",
			Category:          "crm",
			Headquarters:      "US",
			DataCategories:    []string{"name", "email", "phone", "company", "sales_data", "interaction_history"},
			ProcessingPurposes: []string{"crm", "sales_automation", "marketing", "analytics"},
			DataLocations:     []string{"us", "eu", "de", "fr"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://www.salesforce.com/content/dam/web/en_us/www/documents/legal/Agreements/data-processing-addendum.pdf",
		},
		{
			Name:              "Slack",
			Slug:              "slack",
			Category:          "communication",
			Headquarters:      "US",
			DataCategories:    []string{"name", "email", "messages", "files", "usage_data"},
			ProcessingPurposes: []string{"team_communication", "collaboration", "file_sharing"},
			DataLocations:     []string{"us"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://slack.com/trust/data-processing-addendum",
		},
		{
			Name:              "Zoom",
			Slug:              "zoom",
			Category:          "communication",
			Headquarters:      "US",
			DataCategories:    []string{"name", "email", "meeting_content", "usage_data", "ip_address"},
			ProcessingPurposes: []string{"video_conferencing", "webinars", "team_chat"},
			DataLocations:     []string{"us", "eu"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://zoom.us/docs/doc/Zoom_GLOBAL_DPA.pdf",
		},
		{
			Name:              "Mailchimp",
			Slug:              "mailchimp",
			Category:          "email_marketing",
			Headquarters:      "US",
			DataCategories:    []string{"name", "email", "email_engagement", "demographics"},
			ProcessingPurposes: []string{"email_marketing", "marketing_automation", "audience_management"},
			DataLocations:     []string{"us"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://mailchimp.com/legal/data-processing-addendum/",
		},
		{
			Name:              "Twilio",
			Slug:              "twilio",
			Category:          "communication",
			Headquarters:      "US",
			DataCategories:    []string{"phone_number", "message_content", "call_recordings", "usage_data"},
			ProcessingPurposes: []string{"sms", "voice", "video", "email"},
			DataLocations:     []string{"us", "eu", "ie"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://www.twilio.com/legal/data-protection-addendum",
		},
	}

	for _, p := range processors {
		r.Add(p)
	}
}
