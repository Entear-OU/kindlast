# Kindlast DPO Copilot — Product Requirements Document

**Version:** 0.1 (Draft — Pre-Validation)
**Author:** Eddie Ogola, Entear OÜ
**Date:** April 2026
**Status:** Hypothesis stage. Core assumptions require validation through customer discovery.

---

## 1. Vision

We're building the AI compliance operating system for EU professional services, starting with DPO consultants as our beachhead, expanding to their SME clients, then into adjacent compliance roles.

Kindlast is a deliverable accelerator for Data Protection Officers and privacy consultants. Instead of filling in templates from scratch, the DPO describes a client's business context and Kindlast generates first-draft compliance artifacts — RoPAs, DPIA screenings, DPA gap analyses, lawful basis assessments — grounded in current EU regulatory guidance. The DPO reviews, applies judgment, and delivers.

---

## 2. Problem Statement

### What DPOs actually do (validated from practitioner sources)

- Build Records of Processing Activities (RoPA) for each client — the first and hardest slog
- Facilitate Data Protection Impact Assessments (DPIAs)
- Draft and review Data Processing Agreements (DPAs) and Standard Contractual Clauses (SCCs)
- Determine lawful basis for processing activities
- Handle Data Subject Access Requests (DSARs)
- Advise on cookie compliance, privacy notices, breach response
- Manage stakeholders who don't care about compliance

### Where the pain actually lives

**Information extraction, not form-filling.** The hardest part of RoPA creation is getting employees to describe their processing activities accurately. DPOs, as non-experts in the client's business, often don't know the right questions to ask about specific cloud platforms, data flows, or retention practices.

**Repetitive first-draft work.** Every new client engagement starts with similar documentation tasks — mapping processing activities, screening for DPIA requirements, identifying missing DPAs. Experienced consultants estimate 4-8 hours of intake documentation per client before substantive advisory work begins.

**Expanding scope without more hours.** 80%+ of privacy teams have gained responsibilities beyond privacy (AI governance, data governance, cybersecurity compliance). DPOs are being asked about EU AI Act obligations with no tooling or framework.

**The political/human problem is unsolvable by software.** 40%+ of DPOs report insufficient management support. Stakeholder resistance, organizational politics, and risk appetite misalignment are the most frustrating parts of the job. Kindlast does not attempt to solve these.

### What DPOs currently use

| Tool | What it does | Price | Gap |
|------|-------------|-------|-----|
| OneTrust | Full enterprise privacy platform | $50K-$500K+/yr | Way too expensive for consultants/SMEs |
| PrivacyEngine | RoPA, DPIA, DSAR workflows | ~€4,999/yr | Workflow tool, not AI-native |
| Dastra | Collaborative DPO platform, multi-client | Custom pricing | Best positioned incumbent; AI is bolted on |
| Wired Relations | GRC for privacy + infosec | Custom pricing | Strong for in-house teams, less for consultants |
| Privasee | SME self-serve compliance | €49.99/mo | Targets SMEs directly, not DPO workflow |
| ChatGPT/Claude | Ad-hoc first drafts | $20/mo | No regulatory grounding, no audit trail, no client context persistence |
| Word templates + spreadsheets | Manual everything | Free | The status quo for most solo/small DPO practices |

### The gap

No tool combines: (a) AI-generated first-draft deliverables, (b) grounded in structured EU regulatory corpus, (c) with multi-client context persistence, (d) at a price point accessible to solo/boutique DPO consultants, (e) with an audit trail suitable for regulatory accountability.

---

## 3. Target User

### Primary: Outsourced/consultant DPO

- Solo practitioner or boutique firm (1-5 people)
- Manages 5-30 SME clients simultaneously
- Bills €100-200/hour or fixed-fee per engagement
- CIPP/E or equivalent certified
- Services include GDPR compliance, increasingly EU AI Act, sometimes SOC 2/ISO 27001
- Currently uses templates + ChatGPT + spreadsheets
- Found on: Upwork, LinkedIn, IAPP community, EFDPO national associations, DPO conferences
- Willingness to pay: €200-500/month if it demonstrably saves 4-8 hours per client engagement

### Secondary (expansion): SME client of the DPO

- 10-250 employee company in the EU
- Needs compliance but doesn't want to think about it
- Currently relies on their DPO consultant for everything
- Would use a read-only portal to see compliance status and outstanding items
- Willingness to pay: €49-99/month for ongoing compliance maintenance view

### Explicitly not targeting (for now)

- Enterprise privacy teams (OneTrust's market)
- In-house DPOs at large corporations (Dastra/Wired Relations territory)
- SMEs buying self-serve compliance without a DPO (Privasee's market)

---

## 4. Core Assumptions (Must Validate)

| # | Assumption | Validation method | Kill signal |
|---|-----------|------------------|-------------|
| A1 | DPO consultants spend 4-8 hours on repetitive first-draft documentation per new client | Discovery interviews (target: 10 DPOs) | Most say <2 hours or "it's not the bottleneck" |
| A2 | AI-generated first drafts grounded in regulatory corpus are meaningfully better than generic ChatGPT output | Build prototype, test with 3 DPOs on real scenarios | DPOs can't distinguish output quality from ChatGPT |
| A3 | DPO consultants would pay €200-500/month for this | Direct pricing question in discovery | Consistent answer below €100/month or "I'd just use ChatGPT" |
| A4 | EU AI Act gap analysis is a differentiator incumbents don't have yet | Competitive audit of Dastra, PrivacyEngine, Wired Relations AI Act features | Incumbents ship AI Act modules before Kindlast launches |
| A5 | Multi-client context persistence creates meaningful switching cost | Usage data post-launch | Clients churn after single engagement rather than retaining |

---

## 5. Product Scope

### Phase 1: Deliverable Accelerator (MVP — Months 1-3)

The DPO describes a client's business context in natural language. Kindlast generates first-draft compliance artifacts.

**Input:** "My client is a 50-person SaaS company. They process customer data via Stripe, HubSpot, AWS EU-West, and Intercom. They sell to EU customers. 3 employees handle HR data in BambooHR. They're launching an AI chatbot feature that uses customer conversation history."

**Outputs:**

1. **RoPA draft** — Pre-populated processing activities with: purposes, lawful basis suggestions (with reasoning), data categories, data subject categories, recipients/processors identified, suggested retention periods, transfer mechanism flags (e.g., Stripe US entity → SCC required).

2. **DPIA screening** — Based on EDPB criteria, flags which processing activities likely require a full DPIA (e.g., the AI chatbot using conversation history = automated processing + potential profiling). Generates a pre-DPIA assessment with identified risks and suggested mitigations.

3. **DPA gap analysis** — Lists all identified processors, flags which ones likely need DPAs, identifies transfer mechanisms needed (SCCs for US-based processors), and generates a checklist of contractual requirements.

4. **Lawful basis reasoning** — For each processing activity, provides structured analysis of applicable lawful basis with references to relevant EDPB guidelines and DPA decisions.

5. **EU AI Act preliminary classification** — For any AI/ML components described, flags risk category under the AI Act, identifies applicable obligations, and notes timeline for compliance.

**What Phase 1 is NOT:**

- Not a workflow/project management tool (Dastra does this)
- Not a cookie consent manager (Cookiebot, Usercentrics do this)
- Not a DSAR automation tool (requires system integration)
- Not a replacement for DPO judgment — explicitly positioned as first-draft generation requiring professional review

### Phase 2: Client Workspace (Months 4-6)

- Multi-client dashboard — DPO sees all clients, compliance status, outstanding items, upcoming deadlines
- Client context persistence — system remembers each client's tech stack, processing activities, vendor list; new queries cross-reference existing data
- Audit trail — every AI generation logged, every DPO edit tracked, exportable for regulators
- Version history on all artifacts

### Phase 3: SME Client Portal (Months 7-12)

- Read-only compliance status view for the DPO's SME clients
- DPO shares specific artifacts with client stakeholders
- Client can flag changes (new vendor, new processing activity) that surface in DPO's dashboard
- Billing: DPO pays for generation tools; SME pays for portal access (€49-99/month)

### Phase 4: Adjacent Compliance (Year 2+)

- SOC 2 readiness assessment generation
- ISO 27001/27701 gap analysis
- NIS2 compliance mapping
- Cross-framework control mapping (GDPR control → SOC 2 control → ISO control)

---

## 6. The Regulatory Knowledge Base (Core Differentiator)

The single most important technical investment. This is what separates Kindlast from "ChatGPT with a good prompt."

### Corpus

| Source | Content | Update frequency |
|--------|---------|-----------------|
| GDPR | Articles 1-99 + Recitals | Static (but interpretations evolve) |
| EDPB Guidelines | All adopted guidelines (legitimate interest, DPIAs, DPOs, transfers, consent, etc.) | As published (~5-10/year) |
| National DPA decisions | Key enforcement decisions from CNIL, BfDI, Irish DPC, Spanish AEPD, Italian Garante | Monthly scan |
| CJEU case law | Schrems I/II, Meta v. Bundeskartellamt, etc. | As ruled |
| EU AI Act | Full regulation text + annexes | Static (implementing acts evolving) |
| AI Act guidance | European AI Office guidance, standards requests | As published |
| Common processor profiles | Pre-mapped data processing profiles for 200+ common SaaS tools (Stripe, HubSpot, AWS, etc.) — data categories, transfer locations, typical DPA status | Quarterly refresh |

### How it's used

The LLM generates drafts using RAG (retrieval-augmented generation) against this corpus. Every claim in a generated artifact includes a citation to the relevant article, guideline, or decision. The DPO can click through to the source.

This is the moat. A general-purpose LLM doesn't know that the Irish DPC ruled differently from CNIL on a specific legitimate interest question. Kindlast does.

### Build vs. buy

- GDPR text, EDPB guidelines, AI Act text: publicly available, can be ingested and chunked
- DPA decisions: partially available via EDPB case law search engine, GDPRhub; requires curation
- Processor profiles: must be built and maintained manually (start with top 50 most common SaaS tools)
- CJEU case law: available via CURIA and EUR-Lex

Estimated effort for V1 corpus: 4-6 weeks of curation and ingestion work.

---

## 7. Technical Architecture

Leverages Entear's existing stack where possible.

| Layer | Technology | Notes |
|-------|-----------|-------|
| Frontend | Next.js / TypeScript | Existing Kindlast frontend, adapted |
| API Gateway | Go | Existing from Kindlast/Hidden Gems skeleton |
| AI Orchestration | Go (provider abstraction layer) | Existing — supports Claude API, OpenAI; vendor-swappable |
| RAG Service | Go + Qdrant | Existing — hybrid BM25 + vector search |
| Embeddings | OpenAI text-embedding-3-large | Existing |
| Reranking | Cohere Rerank | Existing |
| Generation | Claude API (Sonnet for speed, Opus for complex reasoning) | Existing provider abstraction |
| Ingestion Pipeline | Python (Firecrawl + Unstructured.io) | Existing — content-hash diffing, parent-child chunking |
| Database | PostgreSQL | Client data, user accounts, audit trail |
| Vector DB | Qdrant | Regulatory corpus embeddings |
| Cache | Redis | Session state, rate limiting |
| Infrastructure | Kubernetes / Docker | Existing setup |
| Hosting | EU-based cloud (AWS EU-West or Hetzner) | GDPR compliance for the compliance tool |

### Key technical decisions

- **EU data residency mandatory.** A compliance tool that stores client data outside the EU is a non-starter.
- **Audit trail is first-class.** Every API call to the LLM, every retrieval result, every user edit is logged immutably. This is a regulatory requirement for the tool's own accountability.
- **Citation architecture.** Every generated paragraph links to source chunks in the regulatory corpus. The DPO can verify any claim against the original guideline or decision.
- **Client data isolation.** Multi-tenant architecture with strict data isolation between clients. A DPO's client A data never leaks into client B context.

---

## 8. Business Model

### Pricing

| Tier | Price | Target | Includes |
|------|-------|--------|----------|
| Free | €0 | Trial / evaluation | 2 client assessments, basic RoPA generation, no audit trail |
| Professional | €299/month | Solo DPO consultant | Unlimited clients, full artifact generation, regulatory corpus access, audit trail, EU AI Act module |
| Team | €499/month | Boutique DPO firm (2-5 people) | Everything in Pro + team collaboration, shared client workspace, priority support |
| Client Portal | €49/month per SME client | Billed to DPO or directly to SME | Read-only compliance status, artifact sharing, change flagging |

### Unit economics target

- Professional tier: €299/month × 2,000 DPOs = €7.2M ARR
- Client Portal: €49/month × 10,000 SMEs (5 per DPO average) = €5.9M ARR
- Blended target at scale: €13M+ ARR

### Comparison to alternatives

- PrivacyEngine: ~€4,999/year (~€416/month) — Kindlast Professional is cheaper with AI-native generation
- Dastra: Custom pricing but positioned mid-market — Kindlast differentiates on AI-first approach and EU AI Act
- ChatGPT Pro: €20/month — Kindlast is 15x the price but offers regulatory grounding, audit trail, client persistence
- Privasee: €49.99/month — different buyer (SME self-serve vs. DPO professional tool)

---

## 9. Go-to-Market

### Phase 1: Validation (Now — 8 weeks)

- Send outreach to 10-15 DPO consultants (Upwork, LinkedIn)
- Conduct 5-10 discovery interviews focused on: current workflow, time spent on documentation, tools used, willingness to pay
- Kill/proceed decision based on assumption validation (see Section 4)

### Phase 2: Design Partner Program (Months 1-3)

- Recruit 3-5 DPO consultants as design partners
- Free access in exchange for weekly feedback sessions
- Co-build the first 50 processor profiles with their input
- Generate case studies from real engagement time savings

### Phase 3: Launch (Months 3-6)

- Channels: IAPP community, EFDPO national associations, LinkedIn content (Eddie + Mercy), DPO conferences (IAPP Europe Congress, national DPO association events)
- Content strategy: publish the regulatory knowledge base insights as free content — EDPB guideline summaries, AI Act compliance checklists — to build authority and drive inbound
- Pricing: launch at €199/month introductory, increase to €299 after first 100 customers

### Phase 4: Expansion (Months 6-12)

- Launch SME client portal
- Begin SOC 2 / ISO 27001 module development
- Pursue Tehnopol accelerator milestones if accepted
- Prepare for seed raise with validated metrics

---

## 10. Competitive Response Plan

| If... | Then... |
|-------|---------|
| Dastra ships AI generation features | Compete on regulatory corpus depth (DPA decisions, CJEU case law) and EU AI Act coverage — they're a workflow tool adding AI; we're AI-native adding workflow |
| PrivacyEngine adds AI | Same as above; plus compete on price (€299/mo vs. ~€416/mo) |
| ChatGPT/Claude improves regulatory accuracy | Double down on corpus curation, citation architecture, audit trail, and client context persistence — general-purpose LLMs won't maintain structured processor profiles |
| A new AI-native competitor emerges | Speed. First-mover in EU AI Act compliance tooling + design partner relationships create switching cost |

---

## 11. Risks and Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| DPOs don't pay — current tools + ChatGPT are "good enough" | Critical | Validate before building. Discovery interviews are the gate. |
| AI liability — a generated artifact contains an error that leads to regulatory penalty for DPO's client | High | Explicit disclaimers ("draft for professional review"), citation architecture so DPO can verify every claim, professional indemnity insurance research |
| Regulatory corpus maintenance is unsustainably expensive | Medium | Start with top 5 DPAs (CNIL, BfDI, Irish DPC, AEPD, Garante) + EDPB only. Expand based on customer geography. Automate monitoring where possible. |
| Incumbents copy AI features faster than expected | Medium | The corpus + processor profiles + client context = compound data advantage that grows with usage |
| EU AI Act itself classifies Kindlast as high-risk AI | Medium | Conduct self-assessment under AI Act. Legal advice tool providing recommendations may fall under specific transparency obligations. Address proactively. |
| Small addressable market limits venture-scale outcome | Medium | The DPO beachhead is the entry point, not the ceiling. SME portal expansion + adjacent compliance roles expand TAM significantly. |

---

## 12. Success Metrics

### Validation phase (next 8 weeks)

- 10+ discovery conversations completed
- 3+ DPOs express willingness to pay €200+/month
- 1+ clear "I would use this tomorrow" signal

### MVP phase (months 1-3)

- 3-5 design partners actively using the tool
- Measurable time savings: target 50%+ reduction in first-draft documentation time
- Net Promoter Score > 40 from design partners

### Launch phase (months 3-6)

- 50 paying customers
- €10K+ MRR
- <5% monthly churn

### Scale phase (months 6-12)

- 200+ paying DPO customers
- 500+ SME client portal users
- €50K+ MRR
- Ready for seed raise with validated unit economics

---

## 13. Open Questions

1. **Does the DPO buyer actually exist at this price point?** The profiles we found on Upwork suggest senior consultants billing €100-200/hr. Would they pay €299/month, or do they view documentation as a low-value task they'd rather delegate to a junior?

2. **Is the regulatory corpus defensible?** EDPB guidelines and GDPR text are public. Can we build a structured, curated, cited corpus that's genuinely better than what a skilled prompt engineer can achieve with vanilla Claude?

3. **What's the real competitive timeline?** How fast can Dastra or PrivacyEngine ship comparable AI features? Is the window 6 months or 18 months?

4. **Should Kindlast itself be classified under the EU AI Act?** A tool providing compliance recommendations may have transparency obligations. Need legal assessment.

5. **Is usage-based pricing better than subscription?** A DPO who onboards 3 clients per month uses the tool very differently from one who onboards 1 per quarter. Per-assessment pricing might align better with value delivered.

---

*This PRD is a working hypothesis. Every section above "Phase 2" should be treated as provisional until validated by customer discovery. The single most important next step is completing 10 discovery interviews with DPO consultants.*
