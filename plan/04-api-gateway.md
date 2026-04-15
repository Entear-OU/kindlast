# PRD 04 — API Gateway

**Agent**: Gateway agent  
**DEPENDS ON**: `01-infrastructure.md`, `03-rag-service.md`  
**Produces**: Go API gateway with JWT auth, rate limiting, freemium enforcement, Stripe webhook  

---

## Overview

The API Gateway is the single entry point for all client traffic. It handles JWT authentication, per-user rate limiting, freemium plan enforcement, Stripe webhook processing, and proxies to the RAG service. It is intentionally thin — business logic lives in the RAG service.

---

## Service structure

```
services/gateway/
├── cmd/gateway/
│   └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── auth/
│   │   ├── jwt.go              # JWT issue + validate
│   │   └── middleware.go       # auth middleware
│   ├── ratelimit/
│   │   └── redis_limiter.go    # token bucket per user in Redis
│   ├── freemium/
│   │   └── enforcer.go         # injects plan limits into request context
│   ├── billing/
│   │   ├── stripe.go           # Stripe client wrapper
│   │   └── webhook.go          # Stripe webhook handler
│   ├── proxy/
│   │   └── rag_proxy.go        # reverse proxy to RAG service (SSE-aware)
│   └── server/
│       ├── server.go
│       ├── routes.go
│       └── handlers.go
├── go.mod
└── go.sum
```

---

## Task 1 — Config

Create `services/gateway/internal/config/config.go`:

```go
package config

import (
    "os"
    "strconv"
    "time"
)

type Config struct {
    Port           string
    JWTSecret      string
    JWTExpiry      time.Duration
    RAGServiceURL  string
    RedisURL       string
    PostgresDSN    string
    StripeSecretKey     string
    StripeWebhookSecret string
    StripePremiumPriceID string

    // Rate limits (requests per hour)
    FreeTierRPH    int
    PremiumRPH     int
    APITierRPH     int
}

func Load() *Config {
    rph := func(env string, def int) int {
        if v := os.Getenv(env); v != "" {
            n, _ := strconv.Atoi(v)
            return n
        }
        return def
    }

    return &Config{
        Port:                os.Getenv("PORT"),
        JWTSecret:           os.Getenv("JWT_SECRET"),
        JWTExpiry:           24 * time.Hour,
        RAGServiceURL:       os.Getenv("RAG_SERVICE_URL"),
        RedisURL:            os.Getenv("REDIS_URL"),
        PostgresDSN:         os.Getenv("POSTGRES_DSN"),
        StripeSecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
        StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
        StripePremiumPriceID: os.Getenv("STRIPE_PREMIUM_PRICE_ID"),
        FreeTierRPH:         rph("FREE_TIER_RPH", 20),
        PremiumRPH:          rph("PREMIUM_TIER_RPH", 500),
        APITierRPH:          rph("API_TIER_RPH", 5000),
    }
}
```

---

## Task 2 — JWT auth

Create `services/gateway/internal/auth/jwt.go`:

```go
package auth

import (
    "errors"
    "time"
    "github.com/golang-jwt/jwt/v5"
)

type Claims struct {
    UserID string `json:"user_id"`
    Email  string `json:"email"`
    Plan   string `json:"plan"`   // "free" | "premium" | "api"
    jwt.RegisteredClaims
}

type JWTService struct {
    secret []byte
    expiry time.Duration
}

func NewJWTService(secret string, expiry time.Duration) *JWTService {
    return &JWTService{secret: []byte(secret), expiry: expiry}
}

func (j *JWTService) Issue(userID, email, plan string) (string, error) {
    claims := Claims{
        UserID: userID,
        Email:  email,
        Plan:   plan,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(j.expiry)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
            Issuer:    "kindlast",
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(j.secret)
}

func (j *JWTService) Validate(tokenString string) (*Claims, error) {
    token, err := jwt.ParseWithClaims(tokenString, &Claims{},
        func(t *jwt.Token) (interface{}, error) {
            if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
                return nil, errors.New("unexpected signing method")
            }
            return j.secret, nil
        },
    )
    if err != nil {
        return nil, err
    }
    claims, ok := token.Claims.(*Claims)
    if !ok || !token.Valid {
        return nil, errors.New("invalid token")
    }
    return claims, nil
}
```

Create `services/gateway/internal/auth/middleware.go`:

```go
package auth

import (
    "context"
    "net/http"
    "strings"
)

type contextKey string

const (
    ContextKeyUserID contextKey = "user_id"
    ContextKeyPlan   contextKey = "plan"
    ContextKeyEmail  contextKey = "email"
)

func (j *JWTService) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // extract Bearer token
        authHeader := r.Header.Get("Authorization")
        if !strings.HasPrefix(authHeader, "Bearer ") {
            http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
            return
        }
        tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

        claims, err := j.Validate(tokenStr)
        if err != nil {
            http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
            return
        }

        // inject user context for downstream handlers
        ctx := context.WithValue(r.Context(), ContextKeyUserID, claims.UserID)
        ctx = context.WithValue(ctx, ContextKeyPlan, claims.Plan)
        ctx = context.WithValue(ctx, ContextKeyEmail, claims.Email)

        // forward plan to RAG service via header
        r.Header.Set("X-User-ID", claims.UserID)
        r.Header.Set("X-User-Plan", claims.Plan)

        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

### Acceptance criteria
- [ ] `JWTService.Issue()` produces a valid JWT
- [ ] `JWTService.Validate()` returns correct claims for a valid token
- [ ] Expired tokens return 401
- [ ] Request without Authorization header returns 401
- [ ] Valid token injects `X-User-Plan` header into proxied request

---

## Task 3 — Rate limiter (Redis token bucket)

Create `services/gateway/internal/ratelimit/redis_limiter.go`:

```go
package ratelimit

import (
    "context"
    "fmt"
    "net/http"
    "strconv"
    "time"
    "github.com/redis/go-redis/v9"
)

type RateLimiter struct {
    redis  *redis.Client
    limits map[string]int  // plan -> requests per hour
}

func New(redisClient *redis.Client, limits map[string]int) *RateLimiter {
    return &RateLimiter{redis: redisClient, limits: limits}
}

// Allow checks if user has remaining quota. Returns remaining, reset time, allowed.
func (r *RateLimiter) Allow(ctx context.Context, userID, plan string) (int, time.Time, bool) {
    limit, ok := r.limits[plan]
    if !ok {
        limit = r.limits["free"]
    }

    key := fmt.Sprintf("ratelimit:%s", userID)
    now := time.Now()
    windowStart := now.Truncate(time.Hour)
    reset := windowStart.Add(time.Hour)

    // atomic increment + expiry
    pipe := r.redis.Pipeline()
    incr := pipe.Incr(ctx, key)
    pipe.ExpireAt(ctx, key, reset)
    _, err := pipe.Exec(ctx)
    if err != nil {
        // on Redis error, allow the request (fail open)
        return limit, reset, true
    }

    count := int(incr.Val())
    remaining := max(0, limit-count)
    return remaining, reset, count <= limit
}

func (r *RateLimiter) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        userID, _ := r.Context().Value("user_id").(string)
        plan, _ := r.Context().Value("plan").(string)
        if plan == "" {
            plan = "free"
        }

        remaining, reset, allowed := r.Allow(r.Context(), userID, plan)

        w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
        w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))

        if !allowed {
            w.Header().Set("Retry-After", strconv.FormatInt(time.Until(reset).Seconds(), 10))
            http.Error(w, `{"error":"rate limit exceeded","upgrade_url":"https://kindlast.com/upgrade"}`,
                http.StatusTooManyRequests)
            return
        }

        next.ServeHTTP(w, r)
    })
}
```

### Acceptance criteria
- [ ] 21st request in an hour from a free-tier user returns 429
- [ ] Response includes `X-RateLimit-Remaining` and `Retry-After` headers
- [ ] Redis failure allows request through (fail open)
- [ ] Rate limit resets at the top of the next hour

---

## Task 4 — Freemium enforcer

Create `services/gateway/internal/freemium/enforcer.go`:

```go
package freemium

import "net/http"

// Enforcer injects plan constraints into request headers before proxying.
// The RAG service reads these headers and enforces limits server-side.
// This must run server-side — never trust client-supplied limits.
func Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        plan := r.Header.Get("X-User-Plan")

        switch plan {
        case "premium", "api":
            r.Header.Set("X-Max-Citations", "10")
            r.Header.Set("X-AI-Act-Enabled", "true")
            r.Header.Set("X-Document-Gen-Enabled", "true")
        default: // "free" and unknown
            r.Header.Set("X-Max-Citations", "3")
            r.Header.Set("X-AI-Act-Enabled", "false")
            r.Header.Set("X-Document-Gen-Enabled", "false")
            // strip any client-supplied overrides
            r.Header.Del("X-Max-Citations-Override")
        }

        next.ServeHTTP(w, r)
    })
}
```

### Acceptance criteria
- [ ] Free plan: `X-Max-Citations: 3`, `X-AI-Act-Enabled: false`
- [ ] Premium plan: `X-Max-Citations: 10`, `X-AI-Act-Enabled: true`
- [ ] Client cannot override limits by sending custom headers

---

## Task 5 — Auth handlers (register, login, refresh)

Create `services/gateway/internal/server/handlers.go` with these endpoints:

```go
// POST /auth/register
// Body: {"email": "...", "password": "..."}
// Creates user in PostgreSQL, issues JWT
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
    var body struct {
        Email    string `json:"email"`
        Password string `json:"password"`
    }
    json.NewDecoder(r.Body).Decode(&body)

    // validate email format
    if !isValidEmail(body.Email) {
        writeError(w, "invalid email", http.StatusBadRequest)
        return
    }

    // hash password with bcrypt (cost 12)
    hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), 12)
    if err != nil {
        writeError(w, "server error", http.StatusInternalServerError)
        return
    }

    // create user — returns 409 if email already exists
    user, err := s.db.CreateUser(r.Context(), body.Email, string(hash))
    if err != nil {
        if isUniqueViolation(err) {
            writeError(w, "email already registered", http.StatusConflict)
            return
        }
        writeError(w, "server error", http.StatusInternalServerError)
        return
    }

    token, _ := s.jwt.Issue(user.ID, user.Email, user.Plan)
    writeJSON(w, map[string]any{"token": token, "plan": user.Plan}, http.StatusCreated)
}

// POST /auth/login
// Body: {"email": "...", "password": "..."}
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
    // lookup user, verify bcrypt, issue JWT
    // return 401 for invalid credentials (same message for both wrong email and wrong password)
}

// POST /auth/refresh
// Header: Authorization: Bearer <token>
// Issues a new token extending expiry — no password required
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
    // validate existing token, issue new one with fresh expiry
}
```

### Acceptance criteria
- [ ] POST `/auth/register` with valid email creates user and returns JWT
- [ ] POST `/auth/register` with duplicate email returns 409
- [ ] POST `/auth/login` with correct credentials returns JWT
- [ ] POST `/auth/login` with wrong password returns 401 (same message as wrong email)
- [ ] Password never stored in plaintext — only bcrypt hash in PostgreSQL

---

## Task 6 — Stripe billing

Create `services/gateway/internal/billing/stripe.go`:

```go
package billing

import (
    "github.com/stripe/stripe-go/v76"
    "github.com/stripe/stripe-go/v76/checkout/session"
    "github.com/stripe/stripe-go/v76/customer"
)

type StripeService struct {
    premiumPriceID string
    successURL     string
    cancelURL      string
}

// CreateCheckoutSession creates a Stripe Checkout session for premium upgrade.
func (s *StripeService) CreateCheckoutSession(userID, email string) (string, error) {
    // create or retrieve Stripe customer
    cus, err := customer.New(&stripe.CustomerParams{
        Email: stripe.String(email),
        Metadata: map[string]string{"kindlast_user_id": userID},
    })
    if err != nil {
        return "", err
    }

    params := &stripe.CheckoutSessionParams{
        Customer: stripe.String(cus.ID),
        Mode:     stripe.String(string(stripe.CheckoutSessionModeSubscription)),
        LineItems: []*stripe.CheckoutSessionLineItemParams{
            {
                Price:    stripe.String(s.premiumPriceID),
                Quantity: stripe.Int64(1),
            },
        },
        SuccessURL: stripe.String(s.successURL + "?session_id={CHECKOUT_SESSION_ID}"),
        CancelURL:  stripe.String(s.cancelURL),
    }
    sess, err := session.New(params)
    if err != nil {
        return "", err
    }
    return sess.URL, nil
}
```

Create `services/gateway/internal/billing/webhook.go`:

```go
// POST /billing/webhook (Stripe webhook — no auth, signature verification required)
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
    payload, err := io.ReadAll(r.Body)
    if err != nil {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    // MUST verify Stripe signature — do not skip
    sig := r.Header.Get("Stripe-Signature")
    event, err := webhook.ConstructEvent(payload, sig, s.config.StripeWebhookSecret)
    if err != nil {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    switch event.Type {
    case "checkout.session.completed":
        // upgrade user to premium
        var sess stripe.CheckoutSession
        json.Unmarshal(event.Data.Raw, &sess)
        userID := sess.Customer.Metadata["kindlast_user_id"]
        s.db.UpdateUserPlan(r.Context(), userID, "premium", sess.ID)

    case "customer.subscription.deleted",
         "customer.subscription.updated":
        // handle cancellation or downgrade
        var sub stripe.Subscription
        json.Unmarshal(event.Data.Raw, &sub)
        if sub.Status == stripe.SubscriptionStatusCanceled {
            s.db.UpdateUserPlan(r.Context(), sub.Customer.Metadata["kindlast_user_id"], "free", "")
        }
    }

    w.WriteHeader(http.StatusOK)
}
```

### Acceptance criteria
- [ ] POST `/billing/checkout` returns a valid Stripe Checkout URL
- [ ] Stripe webhook with valid signature upgrades user plan in PostgreSQL
- [ ] Stripe webhook with invalid signature returns 400
- [ ] User plan reflected in next JWT issued after upgrade

---

## Task 7 — SSE-aware reverse proxy

The gateway must proxy SSE streams from the RAG service to the client without buffering. Standard `httputil.ReverseProxy` buffers — use a custom transport:

Create `services/gateway/internal/proxy/rag_proxy.go`:

```go
package proxy

import (
    "io"
    "net/http"
    "net/url"
)

type RAGProxy struct {
    target *url.URL
}

func New(ragURL string) (*RAGProxy, error) {
    u, err := url.Parse(ragURL)
    if err != nil {
        return nil, err
    }
    return &RAGProxy{target: u}, nil
}

func (p *RAGProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // build upstream request
    upstreamURL := *p.target
    upstreamURL.Path = r.URL.Path
    upstreamURL.RawQuery = r.URL.RawQuery

    req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), r.Body)
    if err != nil {
        http.Error(w, "proxy error", http.StatusBadGateway)
        return
    }

    // copy headers (including auth headers set by middleware)
    for k, vals := range r.Header {
        for _, v := range vals {
            req.Header.Add(k, v)
        }
    }

    // use default client — no response buffering
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        http.Error(w, "upstream unavailable", http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()

    // copy response headers
    for k, vals := range resp.Header {
        for _, v := range vals {
            w.Header().Add(k, v)
        }
    }
    w.WriteHeader(resp.StatusCode)

    // stream body directly — no buffering
    flusher, ok := w.(http.Flusher)
    buf := make([]byte, 4096)
    for {
        n, err := resp.Body.Read(buf)
        if n > 0 {
            w.Write(buf[:n])
            if ok {
                flusher.Flush()
            }
        }
        if err == io.EOF {
            break
        }
        if err != nil {
            break
        }
    }
}
```

### Acceptance criteria
- [ ] SSE stream from RAG service arrives at client without buffering delay
- [ ] Large responses (>4KB) arrive in multiple chunks, not all at once
- [ ] RAG service 502 surfaces as gateway 502 to client

---

## Task 8 — Routes and server wiring

Create `services/gateway/internal/server/routes.go`:

```go
func (s *Server) registerRoutes(mux *http.ServeMux) {
    // Public — no auth
    mux.HandleFunc("GET /healthz", s.handleHealthz)
    mux.HandleFunc("GET /readyz", s.handleReadyz)
    mux.HandleFunc("POST /auth/register", s.handleRegister)
    mux.HandleFunc("POST /auth/login", s.handleLogin)
    mux.HandleFunc("POST /billing/webhook", s.handleStripeWebhook) // Stripe signature auth

    // Authenticated routes — JWT middleware + rate limiter + freemium enforcer
    protected := chain(
        s.jwt.Middleware,
        s.rateLimiter.Middleware,
        freemium.Middleware,
    )

    mux.Handle("POST /auth/refresh", protected(http.HandlerFunc(s.handleRefresh)))
    mux.Handle("POST /billing/checkout", protected(http.HandlerFunc(s.handleCheckout)))
    mux.Handle("POST /v1/query", protected(s.ragProxy))      // proxied to RAG service
    mux.Handle("POST /v1/feedback", protected(http.HandlerFunc(s.handleFeedback)))
    mux.Handle("GET /v1/user", protected(http.HandlerFunc(s.handleGetUser)))
}

// chain applies middleware in order (first applied = outermost)
func chain(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
    return func(h http.Handler) http.Handler {
        for i := len(middlewares) - 1; i >= 0; i-- {
            h = middlewares[i](h)
        }
        return h
    }
}
```

---

## Task 9 — K8s Deployment

Create `infrastructure/k8s/app/api-gateway-deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway
  namespace: kindlast-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: api-gateway
  template:
    spec:
      containers:
      - name: gateway
        image: kindlast/gateway:latest
        ports:
        - containerPort: 8080
          name: http
        env:
        - name: PORT
          value: "8080"
        - name: RAG_SERVICE_URL
          value: "http://rag-service.kindlast-app.svc.cluster.local:8081"
        - name: REDIS_URL
          value: "redis://redis.kindlast-data.svc.cluster.local:6379"
        - name: JWT_SECRET
          valueFrom:
            secretKeyRef:
              name: jwt-secret
              key: jwt_secret
        - name: STRIPE_SECRET_KEY
          valueFrom:
            secretKeyRef:
              name: stripe-keys
              key: secret_key
        - name: STRIPE_WEBHOOK_SECRET
          valueFrom:
            secretKeyRef:
              name: stripe-keys
              key: webhook_secret
        resources:
          requests:
            memory: "64Mi"
            cpu: "100m"
          limits:
            memory: "128Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: api-gateway
  namespace: kindlast-app
spec:
  selector:
    app: api-gateway
  ports:
  - port: 80
    targetPort: 8080
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: api-gateway-ingress
  namespace: kindlast-app
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - api.kindlast.com
    secretName: api-kindlast-tls
  rules:
  - host: api.kindlast.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: api-gateway
            port:
              number: 80
```

### Final acceptance criteria
- [ ] Full auth flow: register → login → query → returns streamed cited response
- [ ] Free user query returns exactly 3 citations
- [ ] 21st request returns 429 with `Retry-After` header
- [ ] Stripe webhook upgrades user plan — next query returns 10 citations
- [ ] HTTPS only on `api.kindlast.com` with valid Let's Encrypt cert
