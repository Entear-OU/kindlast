# Kindlast — Master PRD

**Product**: Kindlast  
**Company**: Entear OÜ  
**Version**: 1.0.0  
**Status**: Implementation-ready  

---

## 1. Product summary

Kindlast is an AI-native GDPR and EU AI Act compliance platform for EU SMEs. It provides cited, grounded answers to regulatory compliance questions by maintaining a continuously-updated index of primary regulatory sources (EUR-Lex, EDPB, national DPAs) and running a hybrid RAG pipeline over them.

**Core value proposition**: answers, not checklists — every response is grounded in a specific regulatory source with an inline citation linking to the original document.

---

## 2. Business model

| Tier | Price | Capabilities |
|---|---|---|
| Free | €0 | Top 3 findings per query, GDPR only |
| Premium | €49/month | Full findings, EU AI Act coverage, document generation |
| API | Usage-based | Per-query access for B2B integrations |

---

## 3. Tech stack — final decisions

| Layer | Technology | Rationale |
|---|---|---|
| API Gateway | Go 1.23 | JWT auth, rate limiting, freemium enforcement |
| RAG Service | Go 1.23 | Provider abstraction, hybrid search, streaming |
| Ingestion Service | Python 3.12 | Firecrawl, Unstructured.io, embedding SDKs |
| Frontend | TypeScript / Next.js 15 | App Router, streaming, SSE |
| Vector DB | Qdrant (self-hosted) | Hybrid BM25 + dense, Kubernetes StatefulSet |
| Cache | Redis 7 (sentinel) | Query, embedding, retrieval caching |
| Database | PostgreSQL 16 | Users, subscriptions, audit logs, parent chunks |
| Container | Docker (multi-stage) | scratch base for Go, slim for Python |
| Orchestration | Kubernetes | Hetzner k3s or DigitalOcean DOKS |
| Secrets | External Secrets Operator + Vault | Never env-var API keys |
| Observability | Prometheus + Grafana + Loki | Structured logs, metrics, alerts |

---

## 4. AI provider stack

| Role | Primary | Fallback |
|---|---|---|
| Generation | Claude Sonnet (Anthropic) | GPT-4o (OpenAI) |
| Embedding | text-embedding-3-large (OpenAI) | embed-multilingual-v3 (Cohere) |
| Reranking | Cohere Rerank 3 | Jina Reranker v2 |
| Scraping | Firecrawl | Playwright (self-hosted fallback) |
| Parsing | Unstructured.io (self-hosted) | Unstructured API |

All AI providers sit behind Go interfaces. Switching providers requires a config change, not a code change. See `03-rag-service.md` for interface definitions.

---

## 5. Repository structure

```
kindlast/
├── services/
│   ├── gateway/          # Go — API gateway
│   ├── rag/              # Go — RAG + provider abstraction
│   └── ingestion/        # Python — scraping + embedding pipeline
├── frontend/             # TypeScript/Next.js
├── infrastructure/
│   ├── k8s/              # Kubernetes manifests
│   ├── docker/           # Dockerfiles per service
│   └── helm/             # Helm charts (optional)
├── .github/
│   ├── workflows/        # CI/CD + compliance scanner
│   └── scripts/          # PII detector, Slack notifier
├── docs/
│   └── prd/              # This directory
└── scripts/              # Local dev helpers
```

---

## 6. PRD document index

| File | Layer | Agent |
|---|---|---|
| `00-overview.md` | This file | — |
| `01-infrastructure.md` | K8s, Docker, databases | Infra agent |
| `02-ingestion-pipeline.md` | Python scraping + embedding | Ingestion agent |
| `03-rag-service.md` | Go RAG + provider abstraction | RAG agent |
| `04-api-gateway.md` | Go gateway + auth + freemium | Gateway agent |
| `05-frontend.md` | Next.js UI + streaming | Frontend agent |
| `06-cicd-compliance.md` | GitHub Actions + PII scanner | DevOps agent |

---

## 7. Implementation order for coding agent

Execute PRDs in this order. Each PRD has a `DEPENDS ON` section that must be complete before starting.

```
Step 1  →  01-infrastructure.md     (no dependencies)
Step 2  →  02-ingestion-pipeline.md (depends on: Step 1)
Step 3  →  03-rag-service.md        (depends on: Step 1, Step 2)
Step 4  →  04-api-gateway.md        (depends on: Step 1, Step 3)
Step 5  →  05-frontend.md           (depends on: Step 4)
Step 6  →  06-cicd-compliance.md    (depends on: Step 3, Step 4)
```

---

## 8. Environment variables — global

All services read from these. Managed by External Secrets Operator in production, `.env` locally.

```env
# AI Providers
ANTHROPIC_API_KEY=
OPENAI_API_KEY=
COHERE_API_KEY=
FIRECRAWL_API_KEY=

# Infrastructure
QDRANT_HOST=qdrant.kindlast-data.svc.cluster.local
QDRANT_PORT=6333
QDRANT_API_KEY=
REDIS_URL=redis://redis.kindlast-data.svc.cluster.local:6379
POSTGRES_DSN=postgres://kindlast:password@postgres.kindlast-data.svc.cluster.local/kindlast

# App
KINDLAST_ENV=production          # production | staging | development
KINDLAST_LOG_LEVEL=info
KINDLAST_API_URL=https://api.kindlast.com
FRONTEND_URL=https://kindlast.com

# Auth
JWT_SECRET=
JWT_EXPIRY_HOURS=24

# Stripe
STRIPE_SECRET_KEY=
STRIPE_WEBHOOK_SECRET=
STRIPE_PREMIUM_PRICE_ID=

# Slack (for compliance scanner)
SLACK_COMPLIANCE_WEBHOOK=
```

---

## 9. Acceptance criteria — system level

These must pass before any version ships:

- [ ] A fresh `kubectl apply -k infrastructure/k8s/` stands up the full cluster
- [ ] The ingestion CronJob completes without error on the 22 regulatory sources
- [ ] A query to the RAG service returns a response with ≥1 citation in <8s (p95)
- [ ] Redis cache hit rate >30% after 24h of operation
- [ ] Switching `GENERATION_PROVIDER=openai` in config routes generation to GPT-4o without code change
- [ ] Free-tier users receive exactly 3 citations regardless of API call parameters
- [ ] A BLOCK-level PII finding in a PR comment prevents merge and posts to Slack
- [ ] All containers pass `docker scout cves` with no CRITICAL vulnerabilities
- [ ] Qdrant pod failure triggers automatic failover to replica within 30s
- [ ] PostgreSQL backup restores successfully in staging environment

---

## 10. Definition of done

Each task in each PRD is "done" when:
1. Code is written and compiles / passes linter
2. Unit tests pass (≥80% coverage on business logic)
3. Docker image builds successfully
4. Feature works end-to-end in local `docker-compose` environment
5. K8s manifest applies without error to a test namespace
