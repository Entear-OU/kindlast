# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kindlast is the **AI compliance operating system for EU professional services**. We're starting with DPOs (Data Protection Officers) as our beachhead, expanding to their SME clients, then into adjacent compliance roles (legal ops, risk, ESG).

The platform provides cited, grounded answers to regulatory questions via a hybrid RAG pipeline over primary regulatory sources (EUR-Lex, EDPB, national DPAs).

**Core value proposition**: Answers, not checklists — every response is grounded in a specific regulatory source with inline citations.

**Go-to-market sequence**:
1. **DPO Copilot** — Deliverable accelerator for privacy consultants (current focus)
2. **SME Compliance** — Self-serve GDPR/AI Act compliance for DPO clients
3. **Adjacent Roles** — Legal ops, risk management, ESG compliance

## Repository Structure

```
kindlast/
├── client/              # Next.js 16 frontend (TypeScript)
├── services/            # Backend services (to be built)
│   ├── gateway/         # Go — API gateway, auth, rate limiting
│   ├── rag/             # Go — RAG service, provider abstraction
│   └── ingestion/       # Python — scraping, embedding pipeline
├── infrastructure/      # K8s manifests, Dockerfiles, Helm charts
├── plan/                # PRD documents (implementation specs)
└── scripts/             # Local dev helpers
```

## Package Managers

- **JavaScript/TypeScript**: Always use **pnpm** — never npm or yarn
- **Python**: Always use **uv** — never pip or poetry

## Development Commands

### Client (Next.js)

```bash
cd client
pnpm install
pnpm dev          # Development server
pnpm test         # Run all tests
pnpm test:watch   # Watch mode
pnpm test:coverage
pnpm lint
pnpm build
```

### Server (when implemented)

```bash
# Go services
go build ./...
go test ./...

# Python ingestion
cd services/ingestion
uv sync
uv run pytest
```

## Development Approach

Follow **test-driven development (TDD)**:
1. Write failing tests first
2. Implement the minimum code to make tests pass
3. Refactor while keeping tests green

Testing frameworks:
- **JavaScript/TypeScript**: Vitest + React Testing Library
- **Python**: pytest
- **Go**: Standard `testing` package

## Architecture

### Current Stack (Client)

| Layer | Technology |
|-------|-----------|
| Framework | Next.js 16 (App Router) |
| UI | shadcn/ui + Tailwind CSS v4 |
| AI | Gateway RAG Service (hybrid search + reranking) |
| Auth | Gateway JWT (email/password) |
| Payments | Stripe (Checkout + Customer Portal) |
| Validation | Zod |

### Planned Stack (Server)

| Layer | Technology |
|-------|-----------|
| API Gateway | Go 1.23 — JWT auth, rate limiting, freemium enforcement |
| RAG Service | Go 1.23 — Provider abstraction, hybrid search, streaming |
| Ingestion | Python 3.12 — Firecrawl, Unstructured.io, embeddings |
| Vector DB | Qdrant (hybrid BM25 + dense vectors) |
| Cache | Redis 7 |
| Database | PostgreSQL 16 |

### AI Provider Stack

| Role | Primary | Fallback |
|------|---------|----------|
| Generation | Claude Sonnet | GPT-4o |
| Embedding | text-embedding-3-large (OpenAI) | embed-multilingual-v3 (Cohere) |
| Reranking | Cohere Rerank 3 | Jina Reranker v2 |

All AI providers sit behind Go interfaces — switching providers requires config change, not code change.

## Key Architectural Patterns

### Client Patterns

**Route Groups**:
- `app/(public)/` — Public pages (landing, login, pricing)
- `app/(dashboard)/` — Protected dashboard pages requiring auth

**Server Actions**: Form submissions use Next.js server actions in colocated `actions.ts` files.

**AI Integration**: Uses Gateway RAG service with Zod schemas:
- `lib/ai/assess-gdpr.ts` — GDPR compliance assessment
- `lib/ai/classify-ai-risk.ts` — EU AI Act risk classification

**API Access**: Gateway API calls via `lib/api/gateway.ts` and `lib/api/config.ts`.

### Server Patterns (Planned)

**Provider Abstraction**: Go interfaces for generation, embedding, reranking — vendor-swappable via config.

**Hybrid RAG Pipeline**: BM25 sparse + dense vector search in Qdrant, with Cohere reranking.

**Parent-Child Chunking**: Small chunks for retrieval, parent chunks for context.

**Artifact Generation**: DPO Copilot generates compliance artifacts (RoPA, DPIA screening, DPA gap analysis) with citations.

## DPO Copilot Module

The DPO Copilot extends Kindlast into a deliverable accelerator for privacy consultants:

- **Multi-client workspaces** — Persistent client context
- **Artifact generation** — RoPA, DPIA screening, DPA gap analysis, lawful basis assessment
- **Processor profiles** — Pre-mapped data for 200+ common SaaS tools
- **Audit trail** — Immutable logging for regulatory accountability
- **Citations** — Every claim links to regulatory source

See `plan/07-dpo-copilot.md` for full implementation spec.

## Data Models

### Client Types (`lib/types/database.ts`)
- `BusinessProfile` — Company info and compliance status
- `Assessment` — GDPR/AI Act assessment results
- `Finding` — Individual compliance findings with severity
- `Subscription` — Stripe subscription state

### Server Types (Planned)
- `Client` — DPO's client organizations
- `Artifact` — Generated compliance documents
- `ProcessorProfile` — SaaS tool compliance data
- `ArtifactAuditLog` — Immutable audit trail

## Testing Structure

Tests mirror source structure under `__tests__/`:
- `__tests__/app/` — Page and API route tests
- `__tests__/components/` — Component tests
- `__tests__/lib/` — Utility and business logic tests

Run a single test file:
```bash
pnpm test path/to/file.test.ts
```

## Environment Variables

Key variables (see `.env.example`):
- `NEXT_PUBLIC_API_URL`, `API_URL_INTERNAL` — Gateway API URLs
- `GOOGLE_GENERATIVE_AI_API_KEY`
- `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `COHERE_API_KEY`
- `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`
- `QDRANT_HOST`, `REDIS_URL`, `POSTGRES_DSN`

## Implementation Order

Server services should be built in this order (see `plan/00-overview.md`):
1. Infrastructure (K8s, Docker, databases)
2. Ingestion pipeline (Python)
3. RAG service (Go)
4. API gateway (Go)
5. Frontend integration
6. CI/CD + compliance scanning
7. DPO Copilot features
