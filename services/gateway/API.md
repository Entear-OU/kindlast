# Gateway API Documentation

## Base URL

```
http://localhost:8080
```

## Authentication

Most endpoints require a JWT access token in the Authorization header:

```
Authorization: Bearer <access_token>
```

## Endpoints

### Health Check

#### GET /health

Check service health status.

**Response:**

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

---

### Authentication

#### POST /api/v1/auth/register

Register a new user account.

**Request:**

```json
{
  "email": "user@example.com",
  "password": "securepassword123",
  "full_name": "John Doe"
}
```

**Response:** `201 Created`

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "uuid-here",
    "email": "user@example.com",
    "full_name": "John Doe",
    "plan": "free",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

**Errors:**

- `400 Bad Request` - Invalid input
- `409 Conflict` - Email already exists

---

#### POST /api/v1/auth/login

Login to get access tokens.

**Request:**

```json
{
  "email": "user@example.com",
  "password": "securepassword123"
}
```

**Response:** `200 OK`

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "uuid-here",
    "email": "user@example.com",
    "full_name": "John Doe",
    "plan": "free",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

**Errors:**

- `400 Bad Request` - Invalid input
- `401 Unauthorized` - Invalid credentials

---

#### POST /api/v1/auth/refresh

Refresh access token using refresh token.

**Request:**

```json
{
  "refresh_token": "eyJhbGciOiJIUzI1NiIs..."
}
```

**Response:** `200 OK`

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "uuid-here",
    "email": "user@example.com",
    "full_name": "John Doe",
    "plan": "free",
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

**Errors:**

- `401 Unauthorized` - Invalid or expired refresh token

---

#### GET /api/v1/auth/me

Get current authenticated user.

**Headers:**

```
Authorization: Bearer <access_token>
```

**Response:** `200 OK`

```json
{
  "id": "uuid-here",
  "email": "user@example.com",
  "full_name": "John Doe",
  "plan": "free",
  "created_at": "2024-01-01T00:00:00Z"
}
```

**Errors:**

- `401 Unauthorized` - Invalid or missing token

---

### User Management

#### GET /api/v1/users/me

Get user profile.

**Headers:**

```
Authorization: Bearer <access_token>
```

**Response:** `200 OK`

```json
{
  "id": "uuid-here",
  "email": "user@example.com",
  "full_name": "John Doe",
  "plan": "free",
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

#### PATCH /api/v1/users/me

Update user profile.

**Headers:**

```
Authorization: Bearer <access_token>
```

**Request:**

```json
{
  "full_name": "Jane Doe"
}
```

**Response:** `200 OK`

```json
{
  "id": "uuid-here",
  "email": "user@example.com",
  "full_name": "Jane Doe",
  "plan": "free",
  "created_at": "2024-01-01T00:00:00Z"
}
```

---

#### GET /api/v1/users/me/plan

Get subscription plan details and usage.

**Headers:**

```
Authorization: Bearer <access_token>
```

**Response:** `200 OK`

```json
{
  "plan": "free",
  "queries_per_month": 100,
  "queries_used": 45,
  "rate_limit_per_min": 5
}
```

---

### RAG Query

#### POST /api/v1/query

Send a query to the RAG service.

**Headers:**

```
Authorization: Bearer <access_token>
Accept: application/json
```

For streaming responses:

```
Accept: text/event-stream
```

**Request:**

```json
{
  "query": "What are the GDPR requirements for data processors?",
  "options": {
    "max_results": 5,
    "include_sources": true
  }
}
```

**Response:** `200 OK`

Response format depends on RAG service implementation.

**Headers in Response:**

```
X-RateLimit-Limit: 5
X-RateLimit-Remaining: 4
X-Usage-Limit: 100
X-Usage-Remaining: 55
```

**Errors:**

- `401 Unauthorized` - Invalid or missing token
- `402 Payment Required` - Monthly quota exceeded
- `429 Too Many Requests` - Rate limit exceeded
- `503 Service Unavailable` - RAG service unavailable (circuit breaker open)

---

### Service Status

#### GET /api/v1/status

Get detailed service status.

**Response:** `200 OK`

```json
{
  "service": "gateway",
  "status": "operational",
  "health": {
    "database": true,
    "redis": true,
    "rag_service": true
  },
  "timestamp": "2024-01-01T00:00:00Z"
}
```

---

## Error Response Format

All errors follow this format:

```json
{
  "error": "Bad Request",
  "message": "Detailed error message",
  "code": "ERROR_CODE"
}
```

### Common Error Codes

- `BAD_REQUEST` - Invalid request data
- `UNAUTHORIZED` - Authentication required or failed
- `USER_NOT_FOUND` - User does not exist
- `EMAIL_EXISTS` - Email already registered
- `INVALID_TOKEN` - JWT token is invalid or expired
- `INVALID_CREDENTIALS` - Wrong email or password
- `RATE_LIMIT_EXCEEDED` - Too many requests
- `QUOTA_EXCEEDED` - Monthly query limit reached
- `SERVICE_UNAVAILABLE` - Downstream service unavailable
- `INTERNAL_ERROR` - Server error

---

## Rate Limiting

Rate limits are enforced per user and vary by subscription plan:

| Plan          | Rate Limit        |
| ------------- | ----------------- |
| Free          | 5 req/min         |
| Professional  | 20 req/min        |
| Team          | 100 req/min       |

Rate limit headers are included in responses:

```
X-RateLimit-Limit: 5
X-RateLimit-Remaining: 4
```

When limit is exceeded, you'll receive:

```
429 Too Many Requests
Retry-After: 60
```

---

## Usage Quotas

Monthly query quotas by plan:

| Plan          | Queries/Month     |
| ------------- | ----------------- |
| Free          | 100               |
| Professional  | 1,000             |
| Team          | Unlimited         |

Usage headers are included in query responses:

```
X-Usage-Limit: 100
X-Usage-Remaining: 55
```

---

## Request ID

All requests are assigned a unique ID for tracing:

**Request Header (optional):**

```
X-Request-ID: client-generated-id
```

**Response Header:**

```
X-Request-ID: request-unique-id
```

Use this ID when reporting issues or debugging.
