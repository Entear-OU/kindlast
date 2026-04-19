# Rate Limiting & Freemium Quick Start

Get started with the rate limiting and freemium enforcement implementation in 5 minutes.

## Prerequisites

- Go 1.24+
- Redis server (local or remote)
- PostgreSQL (for user authentication)

## Installation

The implementation is already integrated into the gateway service. No additional installation needed.

## Configuration

Set environment variables in `.env`:

```bash
# Redis connection
REDIS_URL=redis://localhost:6379/0

# Enable rate limiting
RATE_LIMIT_ENABLED=true
```

## Quick Test

### 1. Start Redis

```bash
# Using Docker
docker run -d -p 6379:6379 redis:7-alpine

# Or native Redis
redis-server
```

### 2. Run Tests

```bash
cd /Users/eddieogola/dev/entear/kindlast/services/gateway

# Run all tests
go test ./internal/ratelimit/... ./internal/freemium/...

# Run integration tests (requires Redis)
REDIS_URL=redis://localhost:6379/1 go test -v ./internal/ratelimit/...
REDIS_URL=redis://localhost:6379/1 go test -v ./internal/freemium/...
```

### 3. Start Gateway

```bash
# Build
go build -o gateway cmd/server/main.go

# Run
./gateway
```

## API Examples

### Test Rate Limiting

```bash
# Get JWT token (assuming you have a user)
TOKEN="your-jwt-token"

# Make multiple requests
for i in {1..25}; do
  curl -H "Authorization: Bearer $TOKEN" \
       http://localhost:8080/api/v1/query \
       -d '{"query": "test"}' \
       -i
  echo "Request $i"
done

# After 20 requests (free plan), you'll see:
# HTTP/1.1 429 Too Many Requests
# X-RateLimit-Limit: 20
# X-RateLimit-Remaining: 0
# Retry-After: 1800
```

### Test Citation Limits

```bash
# Free plan user - response with 4 citations will be blocked
curl -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     http://localhost:8080/api/v1/query \
     -d '{"query": "Tell me about GDPR Article 5"}' \
     -i

# If response has > 3 citations:
# HTTP/1.1 403 Forbidden
# {"error": "Citation limit exceeded", ...}
```

## Monitoring

### Check Redis Keys

```bash
# Connect to Redis
redis-cli

# View rate limit keys
KEYS ratelimit:*

# View citation tracking keys
KEYS citations:*

# Get specific user's rate limit
GET ratelimit:user-123:1704063600

# Get specific user's daily citations
GET citations:user-123:2024-01-01
```

### View Logs

```bash
# Rate limit exceeded
grep "rate limit exceeded" gateway.log

# Citation limit exceeded
grep "citation limit exceeded" gateway.log

# Redis errors
grep "redis error" gateway.log
```

## Rate Limits by Plan

| Plan | Requests/Hour | Citations/Response |
|------|---------------|-------------------|
| Free | 20 | 3 |
| Professional | 500 | Unlimited |
| Team | 5,000 | Unlimited |
| Enterprise | 5,000 | Unlimited |

## Common Commands

```bash
# Check Redis connection
redis-cli ping

# Monitor Redis operations in real-time
redis-cli monitor

# Clear all rate limits (admin)
redis-cli --scan --pattern "ratelimit:*" | xargs redis-cli del

# Clear all citation tracking
redis-cli --scan --pattern "citations:*" | xargs redis-cli del

# Get all keys for a user
redis-cli --scan --pattern "*user-123*"
```

## Troubleshooting

### Rate limiting not working

1. Check Redis connection:
   ```bash
   redis-cli ping
   # Should return: PONG
   ```

2. Check environment variable:
   ```bash
   echo $REDIS_URL
   # Should output: redis://localhost:6379/0
   ```

3. Check logs for Redis errors:
   ```bash
   grep "redis" gateway.log
   ```

### Citation enforcement not working

1. Verify middleware order in `cmd/server/main.go`:
   ```go
   r.Use(middleware.Auth(...))        // Must be first
   r.Use(middleware.RateLimit(...))   // Then rate limit
   r.Use(middleware.Freemium(...))    // Then freemium
   ```

2. Check response format (must be JSON with citations array)

3. Verify plan in database:
   ```sql
   SELECT id, email, plan FROM users WHERE id = 'user-123';
   ```

## Testing in Development

### Disable Rate Limiting

```bash
# In .env
RATE_LIMIT_ENABLED=false
```

### Adjust Limits for Testing

Edit `/internal/ratelimit/ratelimiter.go`:
```go
const (
    FreePlanLimit = 5  // Reduced for testing
    // ...
)
```

Edit `/internal/freemium/enforcer.go`:
```go
const (
    FreePlanCitationLimit = 1  // Reduced for testing
    // ...
)
```

### Reset User's Rate Limit

```go
// In code
limiter := ratelimit.NewRateLimiter(redisClient)
limiter.Reset(ctx, "user-123")

// Or in Redis CLI
redis-cli --scan --pattern "ratelimit:user-123:*" | xargs redis-cli del
```

## Production Checklist

- [ ] Redis cluster configured
- [ ] Connection pool sized appropriately
- [ ] Rate limits tuned based on traffic
- [ ] Monitoring and alerts set up
- [ ] Load testing completed
- [ ] Backup Redis configured (Redis Sentinel)
- [ ] Error tracking enabled (Sentry, etc.)
- [ ] Usage analytics dashboard created

## Next Steps

1. Read [RATE_LIMITING.md](./RATE_LIMITING.md) for detailed documentation
2. Read [IMPLEMENTATION_SUMMARY.md](./IMPLEMENTATION_SUMMARY.md) for architecture
3. Review [README.md](./README.md) for API documentation
4. Set up monitoring and alerts
5. Plan rollout strategy

## Support

For issues or questions:
- Check logs: `grep "rate limit\|citation" gateway.log`
- Check Redis: `redis-cli monitor`
- Review documentation in this directory
- Contact platform team

---

**Quick Reference Card**

```
Rate Limiting:     Hourly windows, fail-open
Citation Limits:   Free=3, Paid=unlimited  
Redis Keys:        ratelimit:{user}:{hour}
                   citations:{user}:{date}
Headers:           X-RateLimit-*
Error Codes:       429 (rate limit), 403 (citation limit)
```

Last Updated: April 19, 2024
