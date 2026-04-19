# Gateway Service

API Gateway for Kindlast - handles authentication, rate limiting, and proxying to RAG service.

## Features

- **Authentication**: JWT-based authentication with access and refresh tokens
- **Rate Limiting**: Redis-backed per-user rate limiting based on subscription plan
- **Freemium Enforcement**: Monthly query quota tracking
- **RAG Proxy**: Reverse proxy to RAG service with SSE streaming support
- **Circuit Breaker**: Fault tolerance for RAG service calls
- **Health Checks**: Database, Redis, and RAG service health monitoring

## Architecture

```
├── cmd/server/          # Main entry point
├── internal/
│   ├── config/          # Configuration management
│   ├── handlers/        # HTTP request handlers
│   ├── middleware/      # HTTP middleware (auth, rate limit, etc.)
│   ├── models/          # Data models
│   └── proxy/           # RAG service proxy
```

## API Endpoints

### Public Endpoints

- `GET /health` - Health check
- `POST /api/v1/auth/register` - User registration
- `POST /api/v1/auth/login` - User login
- `POST /api/v1/auth/refresh` - Refresh access token

### Protected Endpoints (Require Authentication)

- `GET /api/v1/auth/me` - Get current user
- `GET /api/v1/users/me` - Get user profile
- `PATCH /api/v1/users/me` - Update user profile
- `GET /api/v1/users/me/plan` - Get subscription plan details
- `GET /api/v1/status` - Service status

### RAG Query Endpoint (Auth + Rate Limit + Freemium)

- `POST /api/v1/query` - Proxy to RAG service

## Configuration

Configuration is loaded from environment variables. See `.env.example` for all available options.

Required variables:
- `POSTGRES_DSN` - PostgreSQL connection string
- `JWT_SECRET` - Secret key for JWT signing

## Database Schema

The gateway expects a PostgreSQL database with the following schema:

```sql
CREATE TABLE users (
    id VARCHAR(255) PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    full_name VARCHAR(255),
    plan VARCHAR(50) DEFAULT 'free',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX idx_users_email ON users(email);
```

## Running the Service

### Development

```bash
# Install dependencies
go mod download

# Set up environment variables
cp .env.example .env
# Edit .env with your configuration

# Run the service
go run cmd/server/main.go
```

### Production

```bash
# Build binary
go build -o gateway cmd/server/main.go

# Run binary
./gateway
```

## Subscription Plans

### Free Plan
- 20 requests per hour (token bucket with hourly windows)
- Maximum 3 citations per response
- Citation usage tracked daily

### Professional Plan
- 500 requests per hour
- Unlimited citations
- Citation usage tracked daily

### Team Plan
- 5,000 requests per hour
- Unlimited citations
- Citation usage tracked daily

### Enterprise Plan
- 5,000 requests per hour
- Unlimited citations
- Citation usage tracked daily

## Rate Limiting Implementation

The gateway implements a **token bucket algorithm** with Redis for rate limiting:

- **Hourly Windows**: Rate limits are enforced per hour (e.g., 20 requests from 2:00 PM to 3:00 PM)
- **Redis Keys**: `ratelimit:{userID}:{hour_timestamp}` with automatic expiry
- **Atomic Operations**: Uses Redis pipelines for INCR + EXPIRE operations
- **Fail Open**: If Redis is unavailable, requests are allowed to proceed
- **Headers**: Returns `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`, and `Retry-After` (when exceeded)

Rate limits by plan:
```go
Free:         20 requests/hour
Professional: 500 requests/hour
Team:         5000 requests/hour
Enterprise:   5000 requests/hour
```

## Freemium Enforcement

The gateway enforces citation limits for free plan users:

- **Citation Counting**: Intercepts RAG responses and counts citations in JSON responses
- **Free Plan Limit**: Maximum 3 citations per response
- **Paid Plans**: Unlimited citations
- **Usage Tracking**: Tracks daily citation usage in Redis (`citations:{userID}:{date}`)
- **Streaming Support**: For streaming responses (SSE), plan information is passed via headers to downstream services
- **Response Interception**: Buffers responses to count citations before returning to client

When citation limit is exceeded, returns 403 Forbidden:
```json
{
  "error": "Citation limit exceeded",
  "message": "Your free plan is limited to 3 citations per response. Upgrade to Professional for unlimited citations.",
  "code": "CITATION_LIMIT_EXCEEDED"
}
```

## Middleware Stack

Requests flow through the following middleware:

1. **RequestID** - Injects X-Request-ID header
2. **Logger** - Structured request logging
3. **Recovery** - Panic recovery with stack traces
4. **CORS** - Cross-origin resource sharing
5. **Auth** - JWT validation (protected routes only)
6. **RateLimit** - Token bucket rate limiting with hourly windows (query endpoint only)
7. **Freemium** - Citation limit enforcement and usage tracking (query endpoint only)

## RAG Proxy Features

- **SSE Streaming**: Detects `Accept: text/event-stream` header and handles streaming responses
- **User Context Injection**: Automatically injects `user_id` and `user_plan` into requests
- **Circuit Breaker**: Prevents cascading failures with configurable thresholds
- **Connection Pooling**: Reuses HTTP connections for better performance
- **Proper Flushing**: Ensures SSE events are flushed immediately

## Health Checks

The `/health` endpoint returns:

```json
{
  "status": "healthy",
  "version": "1.0.0",
  "components": {
    "database": "healthy",
    "redis": "healthy",
    "rag_service": "healthy"
  },
  "timestamp": "2024-01-01T00:00:00Z"
}
```

## Error Responses

All errors follow this format:

```json
{
  "error": "Unauthorized",
  "message": "Invalid or expired token",
  "code": "INVALID_TOKEN"
}
```

## Internal Packages

### `/internal/ratelimit`

Provides rate limiting functionality with Redis backend:

- **`redis.go`**: Redis client wrapper with connection pooling
  - `Increment(key, ttl)`: Atomic increment with TTL
  - `Get(key)`: Get current count
  - `TTL(key)`: Get remaining TTL
  - `Delete(key)`: Remove key
  - `Ping()`: Health check

- **`ratelimiter.go`**: Token bucket rate limiter
  - `Allow(userID, plan)`: Check if request is allowed
  - `GetUsage(userID, plan)`: Get current usage
  - `Reset(userID)`: Clear rate limit (admin operation)

- **`middleware.go`**: HTTP middleware adapter
  - `RateLimitMiddleware(limiter)`: Returns chi-compatible middleware
  - `WithUser(ctx, userID, plan)`: Add user to context
  - `GetUserFromContext(ctx)`: Extract user from context

### `/internal/freemium`

Provides freemium enforcement for citation limits:

- **`enforcer.go`**: Citation limit enforcement
  - `EnforceCitationLimit(userID, plan, count)`: Check citation limit
  - `TrackCitationUsage(userID, count)`: Track daily usage
  - `GetDailyCitationUsage(userID, date)`: Get usage for date
  - `GetCitationUsageRange(userID, start, end)`: Get usage range

- **`middleware.go`**: HTTP middleware for citation enforcement
  - `FreemiumMiddleware(enforcer)`: Returns chi-compatible middleware
  - `ValidateResponseBeforeReturn()`: Direct validation function
  - `StreamingFreemiumMiddleware()`: For SSE/streaming responses

### `/internal/middleware`

HTTP middleware integrations that use the internal packages:

- **`ratelimit.go`**: Integrates `/internal/ratelimit` with Chi router
- **`freemium.go`**: Integrates `/internal/freemium` with Chi router
- **`auth.go`**: JWT authentication
- **`logger.go`**: Structured logging
- **`recovery.go`**: Panic recovery
- **`cors.go`**: CORS handling

## Development

### Testing

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/ratelimit/...
go test ./internal/freemium/...

# Run integration tests (requires Redis)
REDIS_URL=redis://localhost:6379/1 go test ./internal/ratelimit/...
REDIS_URL=redis://localhost:6379/1 go test ./internal/freemium/...
```

### Linting

```bash
go fmt ./...
go vet ./...
```

## Redis Key Patterns

The gateway uses the following Redis key patterns:

```
ratelimit:{userID}:{hour_timestamp}     # Rate limiting counters (TTL: 1h5m)
citations:{userID}:{YYYY-MM-DD}         # Daily citation tracking (TTL: 48h)
```

## HTTP Headers

### Rate Limit Headers (all authenticated requests)
- `X-RateLimit-Limit`: Maximum requests allowed per hour
- `X-RateLimit-Remaining`: Remaining requests in current window
- `X-RateLimit-Reset`: Unix timestamp when window resets
- `Retry-After`: Seconds to wait before retrying (when rate limited)

### Plan Headers (streaming RAG requests)
- `X-User-Plan`: User's subscription plan
- `X-Citation-Limit`: Maximum citations allowed per response

## License

Proprietary - Kindlast
