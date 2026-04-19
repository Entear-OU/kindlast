# Rate Limiting and Freemium Enforcement

This document describes the rate limiting and freemium enforcement implementation in the Kindlast API Gateway.

## Overview

The gateway implements two main enforcement mechanisms:

1. **Rate Limiting**: Token bucket algorithm with hourly windows to prevent abuse
2. **Freemium Enforcement**: Citation limits for free plan users to enforce upgrade incentives

Both systems use Redis for distributed state management and are designed to fail open (allow requests) if Redis is unavailable.

## Rate Limiting

### Algorithm: Token Bucket with Hourly Windows

The rate limiter uses a token bucket algorithm implemented with Redis:

- Each user has a token bucket that refills every hour
- Tokens are consumed on each request
- When the bucket is empty, requests are denied with 429 status code
- Redis key: `ratelimit:{userID}:{hour_timestamp}` (auto-expires after 1h5m)

### Rate Limits by Plan

| Plan | Requests/Hour |
|------|---------------|
| Free | 20 |
| Professional | 500 |
| Team | 5,000 |
| Enterprise | 5,000 |

### Implementation Details

**Location**: `/internal/ratelimit/`

**Key Components**:

1. **redis.go** - Redis client wrapper
   - Connection pooling (10 max, 5 min idle)
   - Atomic operations with pipelines (INCR + EXPIRE)
   - Health checks and error handling

2. **ratelimiter.go** - Core rate limiting logic
   - `Allow(userID, plan)` - Check if request allowed
   - `GetUsage(userID, plan)` - Get current usage
   - `Reset(userID)` - Admin reset operation
   - Returns detailed result with limit, remaining, reset time

3. **middleware.go** - HTTP middleware adapter
   - Context-based user extraction
   - Standard rate limit headers
   - 429 response with retry-after header

### HTTP Headers

The rate limiter adds these headers to all responses:

```
X-RateLimit-Limit: 20           # Maximum requests per hour
X-RateLimit-Remaining: 15       # Remaining requests in current window
X-RateLimit-Reset: 1704067200   # Unix timestamp when window resets
```

When rate limit is exceeded:

```
Retry-After: 1800               # Seconds until window resets
```

### Error Response

```json
{
  "error": "Too many requests",
  "message": "Rate limit of 20 requests per hour exceeded. Please try again in 1800 seconds.",
  "code": "RATE_LIMIT_EXCEEDED"
}
```

### Redis Key Pattern

```
ratelimit:{userID}:{hour_timestamp}
```

Example:
```
ratelimit:user-123:1704063600    # For hour starting at 2024-01-01 00:00:00 UTC
TTL: 3900 seconds (1h5m)
```

## Freemium Enforcement

### Citation Limits

Free plan users are limited to 3 citations per RAG response. This encourages upgrades to paid plans while still providing value.

| Plan | Citations/Response |
|------|--------------------|
| Free | 3 |
| Professional | Unlimited |
| Team | Unlimited |
| Enterprise | Unlimited |

### Implementation Details

**Location**: `/internal/freemium/`

**Key Components**:

1. **enforcer.go** - Citation limit enforcement
   - `EnforceCitationLimit(userID, plan, count)` - Check limit
   - `TrackCitationUsage(userID, count)` - Track daily usage
   - `GetDailyCitationUsage(userID, date)` - Query usage
   - `GetCitationUsageRange(userID, start, end)` - Range query

2. **middleware.go** - HTTP middleware for citation enforcement
   - Response interception and buffering
   - JSON parsing to count citations
   - Streaming support (SSE) via headers
   - Usage tracking in background goroutine

### How It Works

#### Regular Responses

1. Request enters middleware
2. Response is buffered (not sent to client yet)
3. Response JSON is parsed to count citations
4. If citation count > limit:
   - Return 403 Forbidden with upgrade prompt
5. Else:
   - Track usage in background (best effort)
   - Send original response to client

#### Streaming Responses (SSE)

For streaming responses (`Accept: text/event-stream`):

1. Cannot buffer/inspect full response
2. Add plan headers to request for downstream service:
   - `X-User-Plan: free`
   - `X-Citation-Limit: 3`
3. RAG service enforces limit during generation

### Error Response

```json
{
  "error": "Citation limit exceeded",
  "message": "Your free plan is limited to 3 citations per response. Upgrade to Professional for unlimited citations.",
  "code": "CITATION_LIMIT_EXCEEDED"
}
```

### Usage Tracking

Citations are tracked daily for analytics:

**Redis Key Pattern**:
```
citations:{userID}:{YYYY-MM-DD}
```

Example:
```
citations:user-123:2024-01-01
Value: 45                      # Total citations used on this date
TTL: 172800 seconds (48h)      # Kept for 2 days for reporting
```

**Query Functions**:
- `GetDailyCitationUsage(userID, date)` - Single day usage
- `GetCitationUsageRange(userID, start, end)` - Multi-day usage map

## Integration with Chi Router

Both middleware components integrate seamlessly with Chi router:

```go
// Rate limiting (hourly windows)
r.Use(middleware.RateLimit(redisClient, logger))

// Freemium citation enforcement
r.Use(middleware.Freemium(redisClient, logger))
```

The middleware:
1. Extract user from context (set by Auth middleware)
2. Apply rate limit / citation limit check
3. Return error response if limit exceeded
4. Pass request to next handler if allowed

## Testing

### Unit Tests

Both packages include comprehensive test suites:

**Rate Limiting Tests** (`internal/ratelimit/*_test.go`):
- Redis client operations
- Rate limit enforcement for each plan
- Usage tracking
- Reset operations
- Default plan handling

**Freemium Tests** (`internal/freemium/*_test.go`):
- Citation limit enforcement
- Usage tracking
- Range queries
- Error handling
- Default plan handling

### Running Tests

```bash
# All tests
go test ./...

# Specific package
go test ./internal/ratelimit/...
go test ./internal/freemium/...

# With Redis (integration tests)
REDIS_URL=redis://localhost:6379/1 go test ./internal/ratelimit/...
REDIS_URL=redis://localhost:6379/1 go test ./internal/freemium/...
```

Note: Integration tests require a running Redis instance. Tests will be skipped if `REDIS_URL` is not set.

## Production Considerations

### Fail Open Strategy

Both systems are designed to fail open:

- If Redis is unavailable, requests are allowed
- Errors are logged but don't block requests
- This prevents Redis outages from taking down the API

### Performance

**Rate Limiting**:
- 1 Redis pipeline per request (INCR + EXPIRE)
- Minimal latency impact (~1-2ms)
- Connection pooling for efficiency

**Citation Tracking**:
- Tracking is async (goroutine)
- No blocking on tracking failures
- Best-effort tracking for analytics

### Monitoring

Key metrics to monitor:

1. **Rate Limit Rejections**:
   - Log level: WARN
   - Log message: "rate limit exceeded"
   - Fields: `user_id`, `plan`, `limit`, `current`

2. **Citation Limit Rejections**:
   - Log level: WARN
   - Log message: "citation limit exceeded"
   - Fields: `user_id`, `plan`, `limit`, `citations`

3. **Redis Errors**:
   - Log level: ERROR
   - Any Redis operation failures
   - Should alert if frequent

### Redis Scaling

For high traffic:

1. **Use Redis Cluster** for horizontal scaling
2. **Increase connection pool** size in config
3. **Monitor Redis CPU/memory** usage
4. **Consider Redis Sentinel** for high availability

### Rate Limit Tuning

To adjust rate limits, modify constants in:
- `/internal/ratelimit/ratelimiter.go` (hourly limits)
- `/internal/freemium/enforcer.go` (citation limits)

Or make them configurable via environment variables.

## Future Enhancements

Potential improvements:

1. **Dynamic Rate Limits**: Per-endpoint rate limits
2. **Burst Allowance**: Allow short bursts above limit
3. **Usage Analytics**: Dashboard for usage patterns
4. **A/B Testing**: Experiment with different limits
5. **Citation Soft Limits**: Warning before hard limit
6. **Rate Limit Bypass**: Whitelist for specific users
7. **Geographic Rate Limits**: Different limits by region

## Architecture Diagram

```
┌─────────────┐
│   Client    │
└─────┬───────┘
      │
      ▼
┌─────────────┐
│ Auth        │  Extract user from JWT
│ Middleware  │  Add to context
└─────┬───────┘
      │
      ▼
┌─────────────────────┐
│ RateLimit           │  Check hourly window
│ Middleware          │  Redis: ratelimit:{user}:{hour}
└─────┬───────────────┘
      │
      ▼ (if allowed)
┌─────────────────────┐
│ Freemium            │  Buffer response
│ Middleware          │  Count citations
│                     │  Track usage in background
└─────┬───────────────┘
      │
      ▼ (if within limit)
┌─────────────┐
│ RAG Service │  Generate response
│ Proxy       │  with citations
└─────────────┘
```

## Error Flow

```
Request → Auth → RateLimit → Freemium → Handler
                    ↓            ↓
                  429 Too      403 Citation
                  Many         Limit
                  Requests     Exceeded
```

## Configuration

Both systems are configured via environment variables:

```bash
# Redis connection
REDIS_URL=redis://localhost:6379/0

# Rate limiting toggle
RATE_LIMIT_ENABLED=true
```

Plan-specific limits are hardcoded in the source code but can be made configurable if needed.
