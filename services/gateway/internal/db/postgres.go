package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	"github.com/entear/kindlast/services/gateway/internal/models"
)

// Client represents a PostgreSQL database client
type Client struct {
	db *sql.DB
}

// NewClient creates a new PostgreSQL client with connection pooling
func NewClient(dsn string) (*Client, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pooling
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Client{db: db}, nil
}

// Close closes the database connection
func (c *Client) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// GetUserByEmail retrieves a user by email address
func (c *Client) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
		SELECT id, email, password_hash, full_name, plan, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	var user models.User
	err := c.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FullName,
		&user.Plan,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	return &user, nil
}

// GetUserByID retrieves a user by ID
func (c *Client) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	query := `
		SELECT id, email, password_hash, full_name, plan, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	var user models.User
	err := c.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FullName,
		&user.Plan,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}

	return &user, nil
}

// CreateUser inserts a new user with default free plan
func (c *Client) CreateUser(ctx context.Context, email, passwordHash string) (*models.User, error) {
	query := `
		INSERT INTO users (id, email, password_hash, plan, created_at, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW(), NOW())
		RETURNING id, email, password_hash, full_name, plan, created_at, updated_at
	`

	var user models.User
	err := c.db.QueryRowContext(ctx, query, email, passwordHash, models.PlanFree).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FullName,
		&user.Plan,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &user, nil
}

// UpdateUserPlan updates a user's subscription plan
func (c *Client) UpdateUserPlan(ctx context.Context, userID, plan string) error {
	query := `
		UPDATE users
		SET plan = $1, updated_at = NOW()
		WHERE id = $2
	`

	result, err := c.db.ExecContext(ctx, query, plan, userID)
	if err != nil {
		return fmt.Errorf("failed to update user plan: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// GetUserPlan retrieves the current plan and limits for a user
func (c *Client) GetUserPlan(ctx context.Context, userID string) (*models.PlanLimit, error) {
	query := `
		SELECT plan
		FROM users
		WHERE id = $1
	`

	var plan string
	err := c.db.QueryRowContext(ctx, query, userID).Scan(&plan)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user plan: %w", err)
	}

	// Look up plan limits
	limits, exists := models.PlanLimits[plan]
	if !exists {
		return nil, fmt.Errorf("invalid plan: %s", plan)
	}

	return &limits, nil
}

// Health checks the database connection health
func (c *Client) Health(ctx context.Context) error {
	return c.db.PingContext(ctx)
}

// UpdateUserStripeCustomerID sets the Stripe customer ID for a user
func (c *Client) UpdateUserStripeCustomerID(ctx context.Context, userID, stripeCustomerID string) error {
	query := `
		UPDATE users
		SET stripe_customer_id = $1, updated_at = NOW()
		WHERE id = $2
	`

	result, err := c.db.ExecContext(ctx, query, stripeCustomerID, userID)
	if err != nil {
		return fmt.Errorf("failed to update user stripe customer id: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// GetUserByStripeCustomerID retrieves a user by their Stripe customer ID
func (c *Client) GetUserByStripeCustomerID(ctx context.Context, stripeCustomerID string) (*models.User, error) {
	query := `
		SELECT id, email, password_hash, full_name, plan, created_at, updated_at
		FROM users
		WHERE stripe_customer_id = $1
	`

	var user models.User
	err := c.db.QueryRowContext(ctx, query, stripeCustomerID).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.FullName,
		&user.Plan,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found for stripe customer: %s", stripeCustomerID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by stripe customer id: %w", err)
	}

	return &user, nil
}
