# PRD 07 — DPO Copilot

**Agent**: DPO Copilot agent
**DEPENDS ON**: `01-infrastructure.md`, `02-ingestion-pipeline.md`, `03-rag-service.md`, `04-api-gateway.md`, `05-frontend.md`
**Produces**: Multi-client workspace, artifact generation engine, processor knowledge base, audit trail system

---

## Overview

This PRD extends Kindlast from a single-query compliance Q&A tool into a deliverable accelerator for DPO consultants. The core addition is an **artifact generation engine** — given a client business description, it produces structured first-draft compliance deliverables (RoPAs, DPIA screenings, DPA gap analyses, lawful basis assessments) grounded in the existing regulatory corpus. All outputs are cited, auditable, and scoped to a persistent client workspace.

### What changes from the existing PRDs

| Layer | Existing (PRDs 01-06) | Added by this PRD |
|---|---|---|
| PostgreSQL | users, subscriptions, audit_logs, parent_chunks | + clients, artifacts, processor_profiles, artifact_audit_log |
| Qdrant | regulatory corpus (1 collection per embedding provider) | + processor_profiles collection |
| RAG Service | single query → cited answer | + artifact pipelines (RoPA, DPIA, DPA, lawful basis, AI Act classification) |
| API Gateway | auth, rate limit, freemium, proxy to RAG | + client CRUD, artifact CRUD, audit log endpoints, plan enforcement per artifact type |
| Ingestion | 22 regulatory sources | + processor profile ingestion (top 200 SaaS tools) |
| Frontend | single query interface | + multi-client dashboard, artifact editor, audit trail viewer |

### What does NOT change

- Provider abstraction interfaces (`03-rag-service.md` Task 1) — reused as-is
- Hybrid search pipeline — reused, extended with processor collection
- Caching layer — reused, extended with artifact cache keys
- Infrastructure (K8s, Docker, secrets, observability) — all existing manifests stay
- CI/CD and PII scanner — extended with artifact-specific checks

---

## Data model

### Task 1 — PostgreSQL schema extension

Add to `infrastructure/k8s/data/postgres-init.sql` (after existing tables):

```sql
-- =============================================
-- DPO COPILOT TABLES
-- =============================================

-- DPO's client organizations
CREATE TABLE clients (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            VARCHAR(255) NOT NULL,
    description     TEXT,                          -- free-text business description
    sector          VARCHAR(100),                  -- "fintech", "healthtech", "saas", "ecommerce", etc.
    country         VARCHAR(2),                    -- ISO 3166-1 alpha-2
    employee_count  INTEGER,
    tech_stack      JSONB DEFAULT '[]'::jsonb,     -- ["stripe", "hubspot", "aws", "bamboohr"]
    data_subjects   JSONB DEFAULT '[]'::jsonb,     -- ["customers", "employees", "website_visitors"]
    processing_purposes JSONB DEFAULT '[]'::jsonb, -- ["email_marketing", "payment_processing", "hr"]
    status          VARCHAR(20) DEFAULT 'active',  -- active | archived
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now(),

    CONSTRAINT clients_user_name_unique UNIQUE (user_id, name)
);

CREATE INDEX idx_clients_user_id ON clients(user_id);
CREATE INDEX idx_clients_status ON clients(status);

-- Generated compliance artifacts
CREATE TABLE artifacts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id),
    type            VARCHAR(50) NOT NULL,          -- ropa | dpia_screening | dpa_gap | lawful_basis | ai_act_classification
    status          VARCHAR(20) DEFAULT 'draft',   -- draft | reviewed | approved | exported
    title           VARCHAR(500),
    input_context   TEXT NOT NULL,                  -- the business description that generated this
    generated_content JSONB NOT NULL,              -- structured artifact content (see schemas below)
    edited_content  JSONB,                         -- DPO's edited version (null until first edit)
    citations       JSONB DEFAULT '[]'::jsonb,     -- [{index, source_url, title, section, chunk_text}]
    generation_meta JSONB NOT NULL,                -- {provider, model, tokens_used, latency_ms, corpus_version}
    version         INTEGER DEFAULT 1,
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_artifacts_client_id ON artifacts(client_id);
CREATE INDEX idx_artifacts_type ON artifacts(type);
CREATE INDEX idx_artifacts_status ON artifacts(status);
CREATE INDEX idx_artifacts_user_id ON artifacts(user_id);

-- Immutable audit trail for every artifact operation
CREATE TABLE artifact_audit_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id     UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id),
    action          VARCHAR(50) NOT NULL,          -- generated | edited | status_changed | exported | deleted
    previous_state  JSONB,                         -- snapshot before change
    new_state       JSONB,                         -- snapshot after change
    metadata        JSONB DEFAULT '{}'::jsonb,     -- {ip, user_agent, reason}
    created_at      TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_audit_artifact_id ON artifact_audit_log(artifact_id);
CREATE INDEX idx_audit_user_id ON artifact_audit_log(user_id);
CREATE INDEX idx_audit_created_at ON artifact_audit_log(created_at);

-- Audit log is append-only — no UPDATE or DELETE allowed
CREATE OR REPLACE FUNCTION prevent_audit_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'artifact_audit_log is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER no_audit_update BEFORE UPDATE ON artifact_audit_log
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_mutation();
CREATE TRIGGER no_audit_delete BEFORE DELETE ON artifact_audit_log
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_mutation();

-- Common SaaS processor profiles
CREATE TABLE processor_profiles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(255) NOT NULL UNIQUE,   -- "Stripe", "HubSpot", "AWS"
    slug            VARCHAR(100) NOT NULL UNIQUE,    -- "stripe", "hubspot", "aws"
    category        VARCHAR(100),                    -- "payment", "crm", "cloud_infra", "hr", "analytics"
    description     TEXT,
    headquarters    VARCHAR(2),                      -- ISO country code
    data_categories JSONB DEFAULT '[]'::jsonb,       -- ["email", "name", "payment_card", "ip_address"]
    processing_purposes JSONB DEFAULT '[]'::jsonb,   -- ["payment_processing", "fraud_detection"]
    data_locations  JSONB DEFAULT '[]'::jsonb,       -- ["us", "eu", "global"]
    transfer_mechanism VARCHAR(50),                   -- "scc" | "dpf" | "adequacy" | "none_required"
    dpa_url         TEXT,                             -- link to their DPA if publicly available
    subprocessors_url TEXT,                           -- link to their subprocessor list
    gdpr_page_url   TEXT,                             -- link to their GDPR/privacy page
    verified        BOOLEAN DEFAULT false,
    last_verified   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_processor_slug ON processor_profiles(slug);
CREATE INDEX idx_processor_category ON processor_profiles(category);

-- Artifact version history (for tracking DPO edits over time)
CREATE TABLE artifact_versions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id     UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    version         INTEGER NOT NULL,
    content         JSONB NOT NULL,
    edited_by       UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ DEFAULT now(),

    CONSTRAINT artifact_version_unique UNIQUE (artifact_id, version)
);
```

### Acceptance criteria
- [ ] `psql` migration applies without error on existing database
- [ ] `INSERT INTO clients` with valid user_id succeeds
- [ ] `UPDATE artifact_audit_log` raises exception
- [ ] `DELETE FROM artifact_audit_log` raises exception
- [ ] Foreign key cascade: deleting a client deletes its artifacts and audit entries

---

## Task 2 — Artifact content schemas

Each artifact type has a defined JSON structure stored in `artifacts.generated_content`. These schemas are enforced in Go, not at the database level.

Create `services/rag/internal/artifact/schemas.go`:

```go
package artifact

// RoPA — Record of Processing Activities (Article 30)
type RoPA struct {
    OrganizationName string              `json:"organization_name"`
    DPOName          string              `json:"dpo_name,omitempty"`
    GeneratedDate    string              `json:"generated_date"`
    Activities       []ProcessingActivity `json:"activities"`
}

type ProcessingActivity struct {
    ID                 string            `json:"id"`                   // "PA-001"
    Name               string            `json:"name"`                 // "Email marketing via HubSpot"
    Purpose            string            `json:"purpose"`
    LawfulBasis        LawfulBasisEntry  `json:"lawful_basis"`
    DataCategories     []string          `json:"data_categories"`      // ["email", "name", "purchase_history"]
    DataSubjects       []string          `json:"data_subjects"`        // ["customers"]
    Recipients         []Recipient       `json:"recipients"`
    Transfers          []Transfer        `json:"transfers,omitempty"`
    RetentionPeriod    string            `json:"retention_period"`     // "24 months after last interaction"
    RetentionRationale string            `json:"retention_rationale"`
    SecurityMeasures   []string          `json:"security_measures"`
    DPIARequired       bool              `json:"dpia_required"`
    DPIARationale      string            `json:"dpia_rationale"`
    Notes              string            `json:"notes,omitempty"`
    Citations          []int             `json:"citations"`            // indices into artifact citations array
}

type LawfulBasisEntry struct {
    Basis       string `json:"basis"`       // "consent" | "contract" | "legal_obligation" | "vital_interests" | "public_task" | "legitimate_interests"
    Article     string `json:"article"`     // "Art. 6(1)(a)"
    Reasoning   string `json:"reasoning"`   // why this basis applies
    LIARequired bool   `json:"lia_required"` // true if legitimate interests → needs balancing test
}

type Recipient struct {
    Name     string `json:"name"`          // "HubSpot Inc."
    Role     string `json:"role"`          // "processor" | "controller" | "joint_controller"
    Purpose  string `json:"purpose"`
    DPAStatus string `json:"dpa_status"`   // "in_place" | "needed" | "unknown"
}

type Transfer struct {
    Destination string `json:"destination"`  // "US"
    Mechanism   string `json:"mechanism"`    // "scc" | "dpf" | "adequacy" | "derogation"
    Notes       string `json:"notes"`
}

// DPIA Screening — Pre-assessment (Article 35)
type DPIAScreening struct {
    ClientName       string             `json:"client_name"`
    GeneratedDate    string             `json:"generated_date"`
    ScreeningResult  string             `json:"screening_result"`  // "required" | "recommended" | "not_required"
    OverallRationale string             `json:"overall_rationale"`
    Activities       []DPIAActivityCheck `json:"activities"`
    EDPBCriteria     []CriterionCheck   `json:"edpb_criteria"`    // 9 EDPB criteria checked
    Recommendations  []string           `json:"recommendations"`
    Citations        []int              `json:"citations"`
}

type DPIAActivityCheck struct {
    ActivityName    string   `json:"activity_name"`
    RiskLevel       string   `json:"risk_level"`       // "high" | "medium" | "low"
    TriggerCriteria []string `json:"trigger_criteria"`  // which EDPB criteria triggered
    Rationale       string   `json:"rationale"`
    RequiresDPIA    bool     `json:"requires_dpia"`
}

type CriterionCheck struct {
    Number      int    `json:"number"`       // 1-9
    Name        string `json:"name"`         // "Evaluation or scoring"
    Triggered   bool   `json:"triggered"`
    Evidence    string `json:"evidence"`     // why it was triggered or not
}

// DPA Gap Analysis
type DPAGapAnalysis struct {
    ClientName    string       `json:"client_name"`
    GeneratedDate string       `json:"generated_date"`
    Processors    []DPACheck   `json:"processors"`
    Summary       DPAGapSummary `json:"summary"`
    Citations     []int        `json:"citations"`
}

type DPACheck struct {
    ProcessorName     string   `json:"processor_name"`
    Category          string   `json:"category"`
    DataCategories    []string `json:"data_categories"`
    Headquarters      string   `json:"headquarters"`
    DPAStatus         string   `json:"dpa_status"`          // "in_place" | "needed" | "unknown"
    DPAPublicURL      string   `json:"dpa_public_url,omitempty"`
    TransferRequired  bool     `json:"transfer_required"`
    TransferMechanism string   `json:"transfer_mechanism,omitempty"`
    TIARequired       bool     `json:"tia_required"`        // Transfer Impact Assessment
    SCCType           string   `json:"scc_type,omitempty"`  // "module_2" (controller-to-processor) etc.
    Actions           []string `json:"actions"`             // what DPO needs to do
}

type DPAGapSummary struct {
    TotalProcessors   int `json:"total_processors"`
    DPAsInPlace       int `json:"dpas_in_place"`
    DPAsNeeded        int `json:"dpas_needed"`
    TransfersRequired int `json:"transfers_required"`
    TIAsRequired      int `json:"tias_required"`
}

// AI Act Classification
type AIActClassification struct {
    ClientName    string           `json:"client_name"`
    GeneratedDate string           `json:"generated_date"`
    AIComponents  []AIComponent    `json:"ai_components"`
    Summary       string           `json:"summary"`
    Citations     []int            `json:"citations"`
}

type AIComponent struct {
    Name              string   `json:"name"`
    Description       string   `json:"description"`
    RiskCategory      string   `json:"risk_category"`       // "unacceptable" | "high" | "limited" | "minimal"
    ClassificationBasis string `json:"classification_basis"` // which Annex/Article
    Obligations       []string `json:"obligations"`
    Timeline          string   `json:"timeline"`             // when obligations apply
    TransparencyReqs  []string `json:"transparency_reqs"`
    Recommendations   []string `json:"recommendations"`
}
```

### Acceptance criteria
- [ ] All types compile without error
- [ ] `json.Marshal` / `json.Unmarshal` round-trips correctly for each type
- [ ] A sample RoPA with 5 activities serializes to valid JSON under 50KB

---

## Task 3 — Processor profile ingestion

Extend the ingestion pipeline to index common SaaS processor profiles.

Create `services/ingestion/src/processors/profiles.py`:

```python
"""
Seed data for the top 50 SaaS processor profiles (Phase 1).
Expand to 200 in Phase 2 based on customer tech stack frequency.

Each profile represents what a DPO needs to know when a client
says "we use Stripe" — data categories, locations, DPA status,
transfer mechanisms.
"""

from dataclasses import dataclass

@dataclass
class ProcessorProfile:
    name: str
    slug: str
    category: str
    headquarters: str                 # ISO country code
    data_categories: list[str]
    processing_purposes: list[str]
    data_locations: list[str]
    transfer_mechanism: str           # "scc" | "dpf" | "adequacy" | "none_required"
    dpa_url: str | None
    subprocessors_url: str | None
    gdpr_page_url: str | None

PROCESSOR_PROFILES: list[ProcessorProfile] = [
    ProcessorProfile(
        name="Stripe", slug="stripe", category="payment",
        headquarters="US",
        data_categories=["name", "email", "payment_card", "billing_address", "ip_address", "transaction_history"],
        processing_purposes=["payment_processing", "fraud_detection", "regulatory_compliance"],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://stripe.com/legal/dpa",
        subprocessors_url="https://stripe.com/legal/service-providers",
        gdpr_page_url="https://stripe.com/guides/general-data-protection-regulation"
    ),
    ProcessorProfile(
        name="HubSpot", slug="hubspot", category="crm",
        headquarters="US",
        data_categories=["name", "email", "phone", "company", "website_activity", "email_engagement"],
        processing_purposes=["crm", "email_marketing", "analytics", "customer_support"],
        data_locations=["us", "eu", "de"],
        transfer_mechanism="dpf",
        dpa_url="https://legal.hubspot.com/dpa",
        subprocessors_url="https://legal.hubspot.com/subprocessors",
        gdpr_page_url="https://legal.hubspot.com/product-privacy-policy"
    ),
    ProcessorProfile(
        name="Amazon Web Services", slug="aws", category="cloud_infrastructure",
        headquarters="US",
        data_categories=["varies_by_service"],
        processing_purposes=["hosting", "storage", "compute", "database"],
        data_locations=["global"],
        transfer_mechanism="dpf",
        dpa_url="https://d1.awsstatic.com/legal/aws-gdpr/AWS_GDPR_DPA.pdf",
        subprocessors_url="https://aws.amazon.com/compliance/sub-processors/",
        gdpr_page_url="https://aws.amazon.com/compliance/gdpr-center/"
    ),
    ProcessorProfile(
        name="Google Workspace", slug="google-workspace", category="productivity",
        headquarters="US",
        data_categories=["email_content", "documents", "calendar", "name", "email", "usage_data"],
        processing_purposes=["email", "document_collaboration", "calendar", "storage"],
        data_locations=["global", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://workspace.google.com/terms/dpa_terms.html",
        subprocessors_url="https://workspace.google.com/terms/subprocessors.html",
        gdpr_page_url="https://cloud.google.com/privacy/gdpr"
    ),
    ProcessorProfile(
        name="Intercom", slug="intercom", category="customer_support",
        headquarters="US",
        data_categories=["name", "email", "conversation_history", "usage_data", "ip_address"],
        processing_purposes=["customer_support", "product_messaging", "analytics"],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://www.intercom.com/legal/data-processing-agreement",
        subprocessors_url="https://www.intercom.com/legal/approved-sub-processors",
        gdpr_page_url="https://www.intercom.com/legal/privacy"
    ),
    # ... 45 more profiles to be added in Phase 1
    # Priority list for remaining profiles based on Upwork DPO interview data:
    # Salesforce, Microsoft 365, Slack, Zoom, Mailchimp, SendGrid, Twilio,
    # Shopify, BambooHR, Workday, Zendesk, Freshdesk, Notion, Airtable,
    # Cloudflare, Datadog, Segment, Amplitude, Mixpanel, Hotjar,
    # DocuSign, PandaDoc, Calendly, Typeform, Tally, Monday.com,
    # Asana, Jira, GitHub, GitLab, Vercel, Netlify, DigitalOcean,
    # Hetzner, OVHcloud, Brevo, ActiveCampaign, Pipedrive, Close,
    # Xero, QuickBooks, Wise, PayPal, Mollie, Adyen, Klarna,
    # Auth0, Okta, 1Password
]
```

Create `services/ingestion/src/processors/indexer.py`:

```python
"""
Indexes processor profiles into both PostgreSQL (structured lookup)
and Qdrant (semantic search for fuzzy matching).

When a DPO says "we use Stripe for payments", the RAG service needs to:
1. Exact-match "stripe" via processor_profiles table
2. Fuzzy-match "payment tool" via Qdrant processor collection
"""

import json
from qdrant_client import QdrantClient
from qdrant_client.models import PointStruct, VectorParams, Distance
import psycopg
from ..config import Config
from .profiles import PROCESSOR_PROFILES, ProcessorProfile

PROCESSOR_COLLECTION = "kindlast_processors"

async def create_processor_collection(client: QdrantClient, config: Config):
    """Create Qdrant collection for processor profile embeddings."""
    client.recreate_collection(
        collection_name=PROCESSOR_COLLECTION,
        vectors_config=VectorParams(
            size=config.openai_embedding_dims,
            distance=Distance.COSINE,
        ),
    )

async def index_processors(
    qdrant: QdrantClient,
    pg_conn: psycopg.AsyncConnection,
    embedder,    # EmbeddingProvider from pipeline
    config: Config,
):
    """Index all processor profiles into PostgreSQL and Qdrant."""

    for profile in PROCESSOR_PROFILES:
        # 1. Upsert into PostgreSQL
        await pg_conn.execute("""
            INSERT INTO processor_profiles
                (name, slug, category, headquarters, data_categories,
                 processing_purposes, data_locations, transfer_mechanism,
                 dpa_url, subprocessors_url, gdpr_page_url, verified)
            VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, true)
            ON CONFLICT (slug) DO UPDATE SET
                data_categories = EXCLUDED.data_categories,
                processing_purposes = EXCLUDED.processing_purposes,
                data_locations = EXCLUDED.data_locations,
                transfer_mechanism = EXCLUDED.transfer_mechanism,
                dpa_url = EXCLUDED.dpa_url,
                updated_at = now()
        """, (
            profile.name, profile.slug, profile.category, profile.headquarters,
            json.dumps(profile.data_categories),
            json.dumps(profile.processing_purposes),
            json.dumps(profile.data_locations),
            profile.transfer_mechanism,
            profile.dpa_url, profile.subprocessors_url, profile.gdpr_page_url,
        ))

        # 2. Embed description for Qdrant semantic search
        embed_text = (
            f"{profile.name} ({profile.category}). "
            f"Processes: {', '.join(profile.data_categories)}. "
            f"Purposes: {', '.join(profile.processing_purposes)}. "
            f"HQ: {profile.headquarters}. "
            f"Transfer: {profile.transfer_mechanism}."
        )
        vectors = await embedder.embed([embed_text])

        qdrant.upsert(
            collection_name=PROCESSOR_COLLECTION,
            points=[PointStruct(
                id=hash(profile.slug) & 0xFFFFFFFF,  # deterministic ID
                vector=vectors[0],
                payload={
                    "slug": profile.slug,
                    "name": profile.name,
                    "category": profile.category,
                    "text": embed_text,
                },
            )],
        )

    await pg_conn.commit()
```

Add to `services/ingestion/src/main.py`:

```python
# In the main() function, add a new mode:
if config.mode == "processors":
    from .processors.indexer import index_processors, create_processor_collection
    await create_processor_collection(qdrant_client, config)
    await index_processors(qdrant_client, pg_conn, embedder, config)
```

### Acceptance criteria
- [ ] `MODE=processors python -m src.main` populates processor_profiles table
- [ ] `SELECT count(*) FROM processor_profiles` returns ≥5 (seed data)
- [ ] Qdrant collection `kindlast_processors` contains matching points
- [ ] Searching "payment processing tool" in Qdrant returns Stripe as top result

---

## Task 4 — Artifact generation pipeline

Extend the RAG service with artifact-specific generation pipelines.

Create `services/rag/internal/artifact/service.go`:

```go
package artifact

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "kindlast/rag/internal/provider"
    "kindlast/rag/internal/retrieval"
    "kindlast/rag/internal/cache"
)

type Service struct {
    genRouter     *provider.ProviderRouter[provider.GenerationProvider]
    embedRouter   *provider.ProviderRouter[provider.EmbeddingProvider]
    rerankRouter  *provider.ProviderRouter[provider.RerankProvider]
    retriever     *retrieval.QdrantRetriever
    parentFetch   *retrieval.ParentFetcher
    processorRepo *ProcessorRepository
    cache         *cache.RedisCache
}

type GenerateRequest struct {
    ArtifactType   string          // "ropa" | "dpia_screening" | "dpa_gap" | "lawful_basis" | "ai_act_classification"
    ClientContext  ClientContext
    UserPlan       string          // "free" | "professional" | "team"
}

type ClientContext struct {
    Name             string   `json:"name"`
    Description      string   `json:"description"`
    Sector           string   `json:"sector"`
    Country          string   `json:"country"`
    EmployeeCount    int      `json:"employee_count"`
    TechStack        []string `json:"tech_stack"`
    DataSubjects     []string `json:"data_subjects"`
    Purposes         []string `json:"processing_purposes"`
    // Optional — populated from persistent client record if available
    ExistingRoPA     *RoPA    `json:"existing_ropa,omitempty"`
}

type GenerateResult struct {
    Content       json.RawMessage          // artifact JSON matching the type schema
    Citations     []provider.Document
    Provider      string
    Model         string
    TokensUsed    int
    LatencyMs     int64
    CorpusVersion string
}

func (s *Service) Generate(ctx context.Context, req GenerateRequest) (*GenerateResult, error) {
    startTime := time.Now()

    // 1. Resolve processor profiles from tech stack
    processors, err := s.resolveProcessors(ctx, req.ClientContext.TechStack)
    if err != nil {
        return nil, fmt.Errorf("processor resolution: %w", err)
    }

    // 2. Build artifact-specific query for regulatory corpus retrieval
    retrievalQuery := s.buildRetrievalQuery(req.ArtifactType, req.ClientContext)

    // 3. Embed and search regulatory corpus
    embedder, err := s.embedRouter.Primary()
    if err != nil {
        return nil, err
    }

    vectors, err := embedder.Embed(ctx, []string{retrievalQuery})
    if err != nil {
        return nil, err
    }

    // 4. Topic filter based on artifact type
    topicFilter := s.topicFilterForType(req.ArtifactType)

    docs, err := s.retriever.HybridSearch(
        ctx, retrievalQuery, vectors[0],
        embedder.CollectionName(), 30, // more docs for artifact generation
        topicFilter,
    )
    if err != nil {
        return nil, err
    }

    // 5. Rerank
    reranker, err := s.rerankRouter.Primary()
    if err != nil {
        return nil, err
    }

    ranked, err := reranker.Rerank(ctx, retrievalQuery, docs, 15)
    if err != nil {
        return nil, err
    }

    // 6. Fetch parent chunks for top results
    parentDocs, err := s.parentFetch.Fetch(ctx, ranked[:min(len(ranked), 10)])
    if err != nil {
        return nil, err
    }

    // 7. Build generation prompt
    prompt := s.buildArtifactPrompt(req.ArtifactType, req.ClientContext, processors, parentDocs)

    // 8. Generate — use Opus for complex artifacts, Sonnet for simple ones
    gen, err := s.genRouter.Primary()
    if err != nil {
        return nil, err
    }

    genReq := provider.GenerationRequest{
        SystemPrompt: prompt.System,
        Messages:     prompt.Messages,
        MaxTokens:    s.maxTokensForType(req.ArtifactType),
        Stream:       false, // artifacts generated in full, not streamed
    }

    ch, err := gen.Generate(ctx, genReq)
    if err != nil {
        return nil, err
    }

    // 9. Collect full response
    var fullResponse string
    for chunk := range ch {
        fullResponse += chunk.Text
    }

    // 10. Parse and validate artifact JSON
    content, err := s.parseAndValidate(req.ArtifactType, fullResponse)
    if err != nil {
        return nil, fmt.Errorf("artifact parse error: %w", err)
    }

    return &GenerateResult{
        Content:       content,
        Citations:     docsToProviderDocs(parentDocs),
        Provider:      gen.ProviderID(),
        TokensUsed:    estimateTokens(fullResponse),
        LatencyMs:     time.Since(startTime).Milliseconds(),
        CorpusVersion: s.retriever.CorpusVersion(),
    }, nil
}

func (s *Service) topicFilterForType(artifactType string) []string {
    switch artifactType {
    case "ai_act_classification":
        return []string{"ai_act"}
    case "ropa", "dpia_screening", "dpa_gap", "lawful_basis":
        return []string{"gdpr"}
    default:
        return nil // search both
    }
}

func (s *Service) maxTokensForType(artifactType string) int {
    switch artifactType {
    case "ropa":
        return 8000 // RoPAs can be lengthy
    case "dpia_screening":
        return 6000
    case "dpa_gap":
        return 4000
    case "ai_act_classification":
        return 4000
    default:
        return 4000
    }
}
```

### Acceptance criteria
- [ ] `Generate()` with type "ropa" returns valid RoPA JSON
- [ ] `Generate()` with type "dpia_screening" returns valid DPIAScreening JSON
- [ ] Citations array is populated with regulatory source URLs
- [ ] Processor profiles are resolved from tech stack names
- [ ] Generation uses correct topic filter per artifact type
- [ ] Latency <15s (p95) for RoPA generation with 5 processing activities

---

## Task 5 — Artifact prompt templates

Create `services/rag/internal/artifact/prompts.go`:

```go
package artifact

import (
    "fmt"
    "strings"
    "kindlast/rag/internal/provider"
)

type PromptPair struct {
    System   string
    Messages []provider.Message
}

func (s *Service) buildArtifactPrompt(
    artifactType string,
    client ClientContext,
    processors []ProcessorProfileData,
    regulatoryContext []provider.Document,
) PromptPair {

    // Format regulatory context as numbered sources
    var sources strings.Builder
    for i, doc := range regulatoryContext {
        fmt.Fprintf(&sources, "[%d] %s — %s\n%s\n\n", i+1, doc.Title, doc.SourceURL, doc.Text)
    }

    // Format processor profiles
    var procContext strings.Builder
    for _, p := range processors {
        fmt.Fprintf(&procContext, "- %s (%s): HQ %s, processes %s, transfer: %s, DPA: %s\n",
            p.Name, p.Category, p.Headquarters,
            strings.Join(p.DataCategories, ", "),
            p.TransferMechanism,
            p.DPAStatus,
        )
    }

    switch artifactType {
    case "ropa":
        return s.ropaPrompt(client, procContext.String(), sources.String())
    case "dpia_screening":
        return s.dpiaPrompt(client, procContext.String(), sources.String())
    case "dpa_gap":
        return s.dpaGapPrompt(client, procContext.String(), sources.String())
    case "ai_act_classification":
        return s.aiActPrompt(client, sources.String())
    default:
        return s.ropaPrompt(client, procContext.String(), sources.String())
    }
}

func (s *Service) ropaPrompt(client ClientContext, processors, sources string) PromptPair {
    system := `You are a GDPR compliance expert generating a Record of Processing Activities (RoPA) under Article 30 GDPR.

RULES:
- Output ONLY valid JSON matching the RoPA schema. No markdown, no commentary outside JSON.
- Every claim must reference a source using [N] notation matching the numbered sources provided.
- For each processing activity, identify: purpose, lawful basis with article reference, data categories, data subjects, recipients, transfers, retention period, security measures, and whether a DPIA is required.
- When identifying processors from the tech stack, use the processor profile data provided. If a tool is not in the profiles, flag it as "unknown — requires manual review".
- Lawful basis reasoning must reference specific EDPB guidelines or DPA decisions from the sources.
- Be conservative: when uncertain, flag for DPO review rather than guessing.
- Retention periods should cite sector-specific guidance where available.
- Flag any processing that involves special category data (Art. 9) or criminal data (Art. 10).`

    userMsg := fmt.Sprintf(`Generate a RoPA for this organization:

ORGANIZATION:
Name: %s
Sector: %s
Country: %s
Employees: %d
Description: %s

TECH STACK & PROCESSOR PROFILES:
%s

DATA SUBJECTS: %s
PROCESSING PURPOSES: %s

REGULATORY SOURCES:
%s

Generate a complete RoPA as JSON. Include all identifiable processing activities based on the tech stack and description.`,
        client.Name, client.Sector, client.Country, client.EmployeeCount, client.Description,
        processors,
        strings.Join(client.DataSubjects, ", "),
        strings.Join(client.Purposes, ", "),
        sources,
    )

    return PromptPair{
        System: system,
        Messages: []provider.Message{
            {Role: "user", Content: userMsg},
        },
    }
}

func (s *Service) dpiaPrompt(client ClientContext, processors, sources string) PromptPair {
    system := `You are a GDPR compliance expert conducting a DPIA pre-screening assessment per Article 35 GDPR and the EDPB Guidelines on DPIA (wp248rev.01).

RULES:
- Output ONLY valid JSON matching the DPIAScreening schema.
- Evaluate ALL 9 EDPB criteria for DPIA requirement.
- The 9 criteria are: (1) evaluation/scoring, (2) automated decision-making with legal effects, (3) systematic monitoring, (4) sensitive data or highly personal data, (5) large scale processing, (6) matching/combining datasets, (7) vulnerable data subjects, (8) innovative use of technology, (9) processing that prevents data subjects from exercising rights.
- If 2 or more criteria are triggered, DPIA is required (per EDPB guidance).
- Reference specific sources using [N] notation.
- Be conservative: flag borderline cases as "recommended" rather than "not_required".`

    userMsg := fmt.Sprintf(`Conduct a DPIA screening for:

ORGANIZATION:
Name: %s | Sector: %s | Country: %s | Employees: %d
Description: %s

TECH STACK:
%s

DATA SUBJECTS: %s
PROCESSING PURPOSES: %s

REGULATORY SOURCES:
%s

Evaluate each processing activity against the 9 EDPB criteria and output as JSON.`,
        client.Name, client.Sector, client.Country, client.EmployeeCount, client.Description,
        processors, strings.Join(client.DataSubjects, ", "), strings.Join(client.Purposes, ", "), sources,
    )

    return PromptPair{
        System: system,
        Messages: []provider.Message{{Role: "user", Content: userMsg}},
    }
}

// dpaGapPrompt and aiActPrompt follow the same pattern — omitted for brevity.
// Each has artifact-specific system instructions referencing the correct
// GDPR articles, EDPB guidelines, and output schema.
```

### Acceptance criteria
- [ ] Each prompt builder produces a valid PromptPair with non-empty system and messages
- [ ] System prompts include explicit JSON-only output instruction
- [ ] Source references use [N] notation matching the numbered regulatory context
- [ ] RoPA prompt includes all required Article 30 fields
- [ ] DPIA prompt references all 9 EDPB criteria

---

## Task 6 — API gateway extensions

Add to `services/gateway/internal/server/routes.go`:

```go
// Client management
r.Route("/api/v1/clients", func(r chi.Router) {
    r.Use(auth.RequireAuth)
    r.Use(freemium.RequirePlan("professional", "team"))

    r.Get("/", handlers.ListClients)           // paginated
    r.Post("/", handlers.CreateClient)
    r.Get("/{clientID}", handlers.GetClient)
    r.Put("/{clientID}", handlers.UpdateClient)
    r.Delete("/{clientID}", handlers.ArchiveClient)  // soft delete
})

// Artifact generation and management
r.Route("/api/v1/clients/{clientID}/artifacts", func(r chi.Router) {
    r.Use(auth.RequireAuth)
    r.Use(freemium.RequirePlan("professional", "team"))
    r.Use(handlers.RequireClientOwnership)     // verify user owns client

    r.Get("/", handlers.ListArtifacts)                     // filter by type, status
    r.Post("/generate", handlers.GenerateArtifact)         // triggers artifact generation
    r.Get("/{artifactID}", handlers.GetArtifact)
    r.Put("/{artifactID}", handlers.UpdateArtifact)        // DPO edits
    r.Put("/{artifactID}/status", handlers.UpdateStatus)   // draft → reviewed → approved
    r.Get("/{artifactID}/audit", handlers.GetAuditTrail)
    r.Post("/{artifactID}/export", handlers.ExportArtifact) // PDF/DOCX export
    r.Get("/{artifactID}/versions", handlers.ListVersions)
})

// Processor profiles (read-only for users)
r.Route("/api/v1/processors", func(r chi.Router) {
    r.Use(auth.RequireAuth)
    r.Get("/", handlers.ListProcessors)               // search by name/category
    r.Get("/{slug}", handlers.GetProcessor)
})

// Audit trail (account-level)
r.Route("/api/v1/audit", func(r chi.Router) {
    r.Use(auth.RequireAuth)
    r.Use(freemium.RequirePlan("professional", "team"))
    r.Get("/", handlers.ListAuditEntries)       // filter by client, artifact, date range
    r.Get("/export", handlers.ExportAuditLog)   // CSV export for regulators
})
```

### Plan enforcement rules

| Feature | Free | Professional (€299/mo) | Team (€499/mo) |
|---|---|---|---|
| Compliance Q&A | 10 queries/day, 3 citations | Unlimited, full citations | Unlimited, full citations |
| Client workspaces | 0 | 20 clients | 50 clients |
| Artifact generation | 0 | 50/month | 200/month |
| Processor profiles | Read top 10 | Full access | Full access |
| Audit trail | No | Yes, 12-month retention | Yes, unlimited retention |
| Export (PDF/DOCX) | No | Yes | Yes |
| Team members | 1 | 1 | 5 |
| EU AI Act module | No | Yes | Yes |

### Acceptance criteria
- [ ] Free-tier user receives 403 on `/api/v1/clients`
- [ ] Professional-tier user can create up to 20 clients
- [ ] 21st client creation returns 429 with plan limit message
- [ ] Artifact generation increments monthly counter in Redis
- [ ] Every artifact mutation writes to artifact_audit_log
- [ ] Client ownership middleware prevents cross-user access
- [ ] Export endpoint generates valid PDF with citations

---

## Task 7 — Audit trail middleware

Create `services/gateway/internal/audit/middleware.go`:

```go
package audit

import (
    "context"
    "encoding/json"
    "net/http"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type AuditEntry struct {
    ArtifactID    string          `json:"artifact_id"`
    UserID        string          `json:"user_id"`
    Action        string          `json:"action"`
    PreviousState json.RawMessage `json:"previous_state,omitempty"`
    NewState      json.RawMessage `json:"new_state,omitempty"`
    Metadata      AuditMetadata   `json:"metadata"`
}

type AuditMetadata struct {
    IP        string `json:"ip"`
    UserAgent string `json:"user_agent"`
    Reason    string `json:"reason,omitempty"`
}

type AuditLogger struct {
    db *pgxpool.Pool
}

func NewAuditLogger(db *pgxpool.Pool) *AuditLogger {
    return &AuditLogger{db: db}
}

func (a *AuditLogger) Log(ctx context.Context, entry AuditEntry) error {
    _, err := a.db.Exec(ctx, `
        INSERT INTO artifact_audit_log
            (artifact_id, user_id, action, previous_state, new_state, metadata)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, entry.ArtifactID, entry.UserID, entry.Action,
       entry.PreviousState, entry.NewState,
       mustJSON(entry.Metadata))
    return err
}

// AuditWrap wraps a handler to automatically log artifact mutations
func (a *AuditLogger) AuditWrap(action string, next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Capture state before
        // ... handler-specific logic to snapshot previous state ...

        next(w, r)

        // Log after successful mutation
        // ... extract artifact ID, user ID from context ...
    }
}

func mustJSON(v interface{}) json.RawMessage {
    b, _ := json.Marshal(v)
    return b
}
```

### Acceptance criteria
- [ ] Every `POST /generate`, `PUT` and `DELETE` on artifacts writes audit entry
- [ ] Audit entries include IP and user agent
- [ ] `GET /api/v1/audit?client_id=X` returns entries for that client only
- [ ] `GET /api/v1/audit/export` produces valid CSV with all columns
- [ ] Audit entries cannot be modified via any API endpoint

---

## Task 8 — Qdrant collection setup extension

Update `scripts/seed-qdrant.sh`:

```bash
#!/bin/bash

# Existing regulatory corpus collections (from PRD 01)
curl -X PUT http://localhost:6333/collections/kindlast_openai_prod \
  -H 'Content-Type: application/json' \
  -d '{
    "vectors": {"size": 3072, "distance": "Cosine"},
    "sparse_vectors": {"bm25": {"modifier": "idf"}},
    "replication_factor": 1
  }'

curl -X PUT http://localhost:6333/collections/kindlast_cohere_prod \
  -H 'Content-Type: application/json' \
  -d '{
    "vectors": {"size": 1024, "distance": "Cosine"},
    "sparse_vectors": {"bm25": {"modifier": "idf"}},
    "replication_factor": 1
  }'

# NEW: Processor profiles collection
curl -X PUT http://localhost:6333/collections/kindlast_processors \
  -H 'Content-Type: application/json' \
  -d '{
    "vectors": {"size": 3072, "distance": "Cosine"},
    "replication_factor": 1
  }'

echo "All Qdrant collections created"
```

### Acceptance criteria
- [ ] `curl http://localhost:6333/collections` shows 3 collections
- [ ] `kindlast_processors` accepts point upserts with 3072-dim vectors

---

## Task 9 — Docker-compose extension

Add to `docker-compose.yml`:

```yaml
  # Add processor ingestion as a separate profile
  processor-ingestion:
    build:
      context: .
      dockerfile: infrastructure/docker/ingestion.Dockerfile
    env_file: .env.local
    environment:
      MODE: processors
    depends_on: [qdrant, postgres]
    profiles: ["processors"]
```

Update `scripts/dev-up.sh`:

```bash
#!/bin/bash
set -e
echo "Starting Kindlast local dev environment..."
cp .env.example .env.local
docker compose up -d qdrant redis postgres
echo "Waiting for databases..."
sleep 5
bash scripts/seed-qdrant.sh

# Seed processor profiles
echo "Seeding processor profiles..."
docker compose --profile processors run --rm processor-ingestion

docker compose up -d gateway rag frontend
echo "Ready at http://localhost:3000"
```

### Acceptance criteria
- [ ] `bash scripts/dev-up.sh` seeds processor profiles before starting app services
- [ ] `curl http://localhost:8080/api/v1/processors` returns seeded processors
- [ ] Full local stack operational with all DPO copilot features

---

## Implementation order

Execute tasks in this order within this PRD:

```
Step 1  →  Task 1 (PostgreSQL schema)
Step 2  →  Task 2 (Artifact schemas in Go)
Step 3  →  Task 3 (Processor profile ingestion)
Step 4  →  Task 8 (Qdrant collection setup)
Step 5  →  Task 5 (Prompt templates)
Step 6  →  Task 4 (Artifact generation pipeline)
Step 7  →  Task 7 (Audit trail middleware)
Step 8  →  Task 6 (API gateway extensions)
Step 9  →  Task 9 (Docker-compose + dev script)
```

---

## System-level acceptance criteria

All must pass before DPO Copilot ships:

- [ ] A DPO can create a client, describe their business, and generate a RoPA in <20s
- [ ] Generated RoPA contains ≥1 citation per processing activity
- [ ] Processor profiles resolve correctly from tech stack names (exact + fuzzy match)
- [ ] DPA gap analysis correctly identifies US-based processors requiring SCCs/DPF
- [ ] DPIA screening evaluates all 9 EDPB criteria and flags ≥2 as "required"
- [ ] Every artifact operation is logged in the immutable audit trail
- [ ] Exported PDF contains the full artifact with inline citation references
- [ ] Plan limits enforced: Professional tier capped at 20 clients, 50 artifacts/month
- [ ] Client data is isolated: user A cannot access user B's clients or artifacts
- [ ] Switching generation provider via config still produces valid artifact JSON
