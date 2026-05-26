# Kindlast — Product Requirements Document

**Product:** Kindlast  
**Company:** Entear OÜ  
**Version:** 1.0 — Agentic MVP  
**Author:** Eddie Ogola  
**Status:** Ready for product development  
**Last Updated:** May 2026

---

## Table of Contents

1. [Vision & Problem Statement](#1-vision--problem-statement)
2. [Target Users](#2-target-users)
3. [Market Context](#3-market-context)
4. [Product Overview](#4-product-overview)
5. [The Four Agents](#5-the-four-agents)
6. [User Journeys](#6-user-journeys)
7. [Features by Screen](#7-features-by-screen)
8. [Notification & Outreach System](#8-notification--outreach-system)
9. [Regulatory Knowledge Base](#9-regulatory-knowledge-base)
10. [Human-in-the-Loop Model](#10-human-in-the-loop-model)
11. [Freemium & Pricing Model](#11-freemium--pricing-model)
12. [MVP Scope](#12-mvp-scope)
13. [Success Metrics](#13-success-metrics)
14. [Open Questions](#14-open-questions)

---

## 1. Vision & Problem Statement

### Vision

Kindlast is the compliance team for EU companies that don't have one.

It is an AI-native compliance operating system that runs continuously on behalf of an organisation — monitoring their regulatory environment, detecting compliance gaps as they emerge, proposing remediation actions, and only escalating to a human when a real decision is needed.

The product is not a dashboard the user logs into. It is an agent that works in the background and surfaces findings through the channels the user already lives in — WhatsApp, Slack, and email.

### The Problem

Every EU company that collects personal data or uses AI tools is subject to GDPR and increasingly the EU AI Act. Compliance is not a one-time setup — it is an ongoing operational responsibility.

The problem is that the tools built for this work are either:

- **Too expensive** — enterprise platforms like OneTrust start at $200,000 per year and require a dedicated compliance team to operate them.
- **Too shallow** — affordable tools like Enzuzo and Privasee generate documents but have no continuous monitoring capability. They are not agents. They do nothing unless a human logs in.
- **Too narrow** — most tools cover GDPR or the EU AI Act, never both.

Meanwhile the regulatory pressure on SMEs is intensifying. The EU AI Act's Article 4 (AI literacy for all staff) has been enforceable since February 2025. High-risk AI system obligations under Annex III are approaching. More than 60% of European SMEs have not started their compliance process.

There is no agentic, proactive, SME-priced compliance product at the intersection of GDPR and the EU AI Act. Kindlast fills that gap.

### What Makes Kindlast Different

1. **It runs without being asked.** The agent works on a continuous schedule — not waiting for a user to log in and run a scan.
2. **It pushes findings to the user.** Notifications arrive via WhatsApp and Slack. The app is a log and override interface, not the primary surface.
3. **It covers both regulations.** GDPR and EU AI Act in a single knowledge base and a single workflow.
4. **It is priced for SMEs.** Starting at €49/month — accessible to the 3 million EU SMEs currently priced out of enterprise tooling.
5. **It never acts without approval.** Every action the agent proposes requires explicit human sign-off before it is executed or logged.

---

## 2. Target Users

### Primary User — The SME Founder or Operations Lead

- Runs a company of 5–100 people in the EU, or serves EU customers from outside it
- Uses multiple SaaS tools, many of which process personal data or include AI features
- Has no dedicated DPO or compliance team
- Knows they need to be compliant but finds the regulations overwhelming
- Does not have time to log into a compliance dashboard regularly
- Will engage with WhatsApp messages and short, clear action requests

### Secondary User — The Part-Time or Outsourced DPO

- A consultant or lawyer acting as DPO for multiple SME clients
- Needs to manage compliance posture across a portfolio without logging into each client's tool individually
- Values documentation trails and audit logs above all else
- Would use Kindlast as a monitoring layer across their client base

### Out of Scope (for MVP)

- Enterprises with dedicated compliance teams — they have OneTrust
- Legal professionals who need to draft bespoke legal documents
- Companies outside the EU with no EU data subjects

---

## 3. Market Context

### Regulatory Landscape

- **GDPR** has been enforceable since 2018. Cumulative fines have passed €7.1 billion. Enforcement is intensifying.
- **EU AI Act Article 4** (AI literacy obligations) has been in force since February 2025 and applies to every company that uses AI tools in a work context — regardless of size.
- **EU AI Act Annex III** (high-risk AI system obligations) deadline is August 2, 2026. This deadline may shift due to the Digital Omnibus proposal, but organisations should not treat a pending proposal as a current rule.
- The regulatory burden is explicitly dual: GDPR and AI Act obligations overlap but are not identical. Most SMEs are navigating both without understanding where one ends and the other begins.

### The SME Compliance Gap — Key Facts

- 60%+ of European SMEs have not started their EU AI Act compliance process
- 78% of organisations broadly have not taken meaningful steps toward AI governance
- A Deloitte survey (November 2025) found 53.8% of German enterprises — some of the most compliance-aware in Europe — had implemented no concrete measures
- Enterprise tools (OneTrust: $200K+/year) are structurally inaccessible to SMEs
- Document-generation tools (Enzuzo: $9–79/month) have no continuous monitoring or agentic capability
- No SME-priced product exists that covers both GDPR and the EU AI Act with a proactive agent layer

### Market Size

- Approximately 3 million EU SMEs are subject to GDPR and increasingly the EU AI Act
- The GDPR services market is valued at $5.4 billion in 2025, projected to reach $23.6 billion by 2033 at 20.2% CAGR
- DPO-as-a-service is a growing model, but human DPOs cost €70,000–€120,000/year for in-house and still charge meaningful monthly retainers as consultants

---

## 4. Product Overview

### What Kindlast Is

Kindlast is an agentic compliance operating system. At its core, it is a set of four specialised AI agents that work continuously on behalf of a client organisation:

1. **The Watcher** — monitors the regulatory environment and the client's own product environment for compliance-relevant signals
2. **The Analyst** — interprets signals into structured findings with a severity rating and a proposed action
3. **The Comms Agent** — delivers findings to the right person via the right channel at the right time
4. **The Executor** — carries out approved actions, logs them with a timestamp, and maintains the audit trail

Together, these agents replace the ongoing monitoring work that would otherwise require a dedicated compliance professional or an expensive enterprise platform.

### What Kindlast Is Not

- It is not a legal advice service. It provides regulatory guidance grounded in primary sources, not legal opinions.
- It is not a document generator. It generates compliance records as a by-product of the agent's work — not as a primary output.
- It is not a passive dashboard. The dashboard exists, but it is a log and override interface — not the primary interaction surface.

### The Core Loop

```
Client onboards via conversational intake
         ↓
Agent builds compliance profile from onboarding answers
         ↓
Watcher monitors regulatory environment + client environment
         ↓
Analyst produces finding: what changed, what it means, what to do
         ↓
Comms Agent pushes finding to right person via WhatsApp or Slack
         ↓
Human reviews, approves or rejects proposed action (one tap)
         ↓
Executor acts on approval + logs with timestamp
         ↓
Watcher continues
```

---

## 5. The Four Agents

### 5.1 The Watcher Agent

The Watcher runs continuously. It monitors two environments simultaneously.

**Regulatory environment monitoring:**
- Detects new publications from primary regulatory sources: EDPB opinions, EU AI Act implementing acts, supervisory authority guidance from national DPAs (ICO, CNIL, AEPD, etc.), and the EUR-Lex official register
- Identifies when new guidance is relevant to the client's processing activities or AI systems
- Flags approaching deadlines on known regulatory obligations

**Client environment monitoring:**
- Detects changes in the client's tech stack — new SaaS tools added, new AI features enabled
- Monitors for signals that a compliance-relevant event has occurred: a new data processing activity, a new vendor integration, a new AI system deployed
- In later phases: reads signals from connected tools (Slack, GitHub commits, Jira tickets) that may indicate a compliance-relevant change

**Trigger logic:**
The Watcher triggers the Analyst when:
- A new regulatory document is published that affects the client's known obligations
- A new AI tool or vendor is detected in the client's environment
- A compliance deadline is within a defined threshold (e.g. 30 days)
- A client has had no compliance activity logged for a defined period
- A DSAR deadline is approaching with no logged response

### 5.2 The Analyst Agent

The Analyst interprets signals from the Watcher into structured, actionable findings. It never acts — it only analyses and proposes.

**For each finding, the Analyst produces:**
- A plain-language description of what was detected
- The specific regulatory obligation it maps to (with article reference)
- A severity rating: Critical, High, Medium, or Low
- A proposed action — specific, not generic (e.g. "Draft a Data Processing Agreement with Vendor X" not "Review your vendor agreements")
- Supporting context from the regulatory knowledge base
- An estimated effort rating for the proposed action

**Key analysis capabilities:**
- Maps detected AI systems against EU AI Act Annex III risk classifications
- Identifies gaps between the client's current documentation and their known processing activities
- Checks whether detected vendors have AI Act compliance documentation on file
- Assesses whether a new processing activity requires a DPIA
- Identifies whether Article 4 AI literacy obligations are documented for the client's staff

**What the Analyst explicitly does not do:**
- Issue legal opinions
- Send notifications (that is the Comms Agent's role)
- Take any action on compliance records (that is the Executor's role)

### 5.3 The Comms Agent

The Comms Agent is the delivery layer. Its job is to get the right finding to the right person through the right channel at the right moment.

**Channel priority:**
1. **WhatsApp Business** — highest open rate, no app to install, ideal for time-sensitive findings
2. **Slack** — for engineering and product teams already working there
3. **Email** — fallback and for formal notifications

**Routing logic:**
- The founder or DPO receives all findings
- Engineering-specific findings (e.g. new AI feature detected in codebase) are routed to the technical lead
- HR-specific findings (e.g. AI literacy obligation for staff) are routed to the HR or operations lead
- DSAR findings are routed to whoever is designated as the data rights handler

**Message format:**
Every Comms Agent message is structured as:
- What happened (one sentence)
- Why it matters (regulatory context, one sentence)
- What to do (the proposed action)
- A single tap: Approve / Reject / Remind me later

**Scheduled communications:**
- **Weekly compliance briefing** — every Monday, a summary of the client's current posture: open items, recent actions taken, upcoming deadlines
- **Monthly posture report** — a fuller summary suitable for sharing with investors or a board
- **Deadline escalation** — if an approved action has not been completed within a defined window, the Comms Agent escalates to a secondary contact

### 5.4 The Executor Agent

The Executor acts — but only on explicit human approval. It is the only agent that writes to compliance records.

**What the Executor can do after approval:**
- Create or update a ROPA entry with the details surfaced by the Analyst
- Create a DPIA task with a pre-filled template based on the processing activity
- Log a risk classification entry in the EU AI Act register
- Create an AI literacy training record for a staff member
- Mark a DSAR as received and start the response countdown
- Draft a data processor agreement template for a newly detected vendor
- Log any approved action with a timestamp, the approving user's identity, and the finding it responds to

**The Executor's golden rule:**
No entry is ever created, modified, or deleted without an explicit approval event from a named human user. The Executor queues, pre-fills, and proposes — but the human is always the final step before any record is written.

**Audit trail:**
Every Executor action generates an immutable audit log entry. This log is the product's compliance evidence — it is what a supervisory authority would inspect in the event of an investigation.

---

## 6. User Journeys

### Journey 1 — New Client Onboarding

A founder signs up and is greeted not by a form but by a conversational intake flow. The agent asks plain-language questions:

- What does your product or service do?
- What kinds of personal data do you collect, and from whom?
- What countries are your users in?
- What AI tools does your company use — internally and in your product?
- Do you currently have a Data Protection Officer?
- Do you have a Record of Processing Activities?

The agent maps the answers to a compliance profile — a structured internal representation of the client's obligations. This profile is the foundation for all subsequent agent work. The conversation is designed to feel like talking to a knowledgeable colleague, not filling in a legal questionnaire. Where the founder uses non-technical language, the agent interprets and maps — it does not ask them to speak in GDPR terms.

At the end of onboarding, the agent produces an initial compliance posture assessment: what is in place, what is missing, and what is the highest-priority action to take first.

### Journey 2 — Proactive Finding (Regulatory Change)

The EDPB publishes new guidance on AI profiling.

1. The Watcher ingests the document and identifies it as relevant to two of the client's known processing activities
2. The Analyst maps the guidance against those activities and determines that one of them may now require a DPIA where none was previously required
3. The Comms Agent sends a WhatsApp message: "New EDPB guidance published on AI profiling. One of your current activities may now require a DPIA. Review and decide."
4. The founder taps "Approve" — triggering the Executor to create a DPIA task with a pre-filled template
5. The action is logged with a timestamp

The founder spent 30 seconds. A compliance professional would have spent hours monitoring regulatory publications, reading the guidance, mapping it to the client's activities, and drafting the DPIA brief.

### Journey 3 — Shadow AI Detection

A member of the team starts using an AI-powered tool in their work.

1. The Watcher detects a new AI tool in the client's environment
2. The Analyst checks whether a data processor agreement exists for that vendor, whether the tool processes personal data, and whether it would qualify as a high-risk AI system under EU AI Act Annex III
3. The Analyst produces a finding: "New AI tool detected. Processes personal data. No DPA on file. Annex III classification: minimal risk."
4. The Comms Agent routes the finding to the founder via Slack: "New AI vendor detected: [Tool Name]. A data processor agreement is required. Draft ready for your review."
5. The founder reviews and approves the draft DPA template
6. The Executor logs the vendor, the DPA status, and the approval event

### Journey 4 — EU AI Act Article 4 Compliance

The client has 12 staff members. Article 4 requires documented AI literacy for all staff.

1. The Watcher identifies that the client's staff AI literacy obligations are undocumented
2. The Analyst flags this as a High severity finding — Article 4 is already in force
3. The Comms Agent sends a message: "Your team of 12 has no documented AI literacy training. This obligation has been in force since February 2025. A training record template is attached."
4. The founder approves — the Executor creates a training record template and logs the compliance action
5. When training is completed, the founder marks it as done — the Executor updates the record and generates an attestation log

### Journey 5 — DSAR Received

A user submits a data subject access request via the company's support inbox.

1. The client connects their support inbox (or manually logs the DSAR in the app)
2. The Watcher detects the DSAR and starts a 30-day countdown
3. The Comms Agent immediately assigns the DSAR to the designated handler with a message: "New DSAR received. 30 days to respond. Steps to follow attached."
4. At day 20, if no response has been logged, the Comms Agent escalates: "DSAR response due in 10 days. No action logged yet."
5. When the response is sent, the founder logs it as complete — the Executor records the response date and closes the countdown

---

## 7. Features by Screen

### Screen 1 — Onboarding (Conversational Intake)

The first experience a new user has with Kindlast. This is a chat interface — not a form.

**What it does:**
- Asks plain-language questions about the company's data practices and AI usage
- Maps answers to a structured compliance profile in the background
- Detects ambiguity and asks follow-up questions without requiring legal knowledge from the user
- At completion, presents an initial posture summary: what is covered, what is missing, what to do first
- Estimated time to complete: 5–10 minutes

**Key design principle:** The user should never feel like they are filling in a form. They are talking to a knowledgeable colleague. The agent interprets and maps — the user just answers honestly.

### Screen 2 — Compliance Dashboard

The overview of the client's current compliance posture. This is a monitoring and override surface — not the primary interaction point.

**What it shows:**
- Overall posture status: Green (low risk), Amber (medium risk), Red (critical gaps)
- Open items count, by severity
- Last agent run timestamp
- Upcoming deadlines (date, obligation, days remaining)
- Recent actions taken (with timestamp and approving user)

**Key design principle:** A founder should be able to understand their posture in under 10 seconds. No compliance jargon. No walls of text.

### Screen 3 — Agent Feed

The core interaction surface. A reverse-chronological timeline of everything the agents have found and proposed.

**Each item in the feed shows:**
- What was detected (plain language)
- The regulatory obligation it maps to
- Severity rating (colour-coded: Critical / High / Medium / Low)
- The proposed action
- Status: Pending approval / Approved / Rejected / Completed

**Actions on each item:**
- Approve — triggers the Executor
- Reject — archives the finding with a note
- Remind me later — snoozes for a configurable period
- View details — expands the full Analyst output including regulatory context and source references

**Key design principle:** The feed should feel like a to-do list for compliance, not a compliance audit report. Every item has one clear next action.

### Screen 4 — Compliance Records

The auto-populated compliance record library. Organised by record type:

- **ROPA** — Record of Processing Activities. Each entry shows: processing activity name, purpose, legal basis, data categories, recipients, retention period, last updated
- **DPIA Register** — Data Protection Impact Assessments. Each entry shows: activity, trigger date, status, completion date
- **AI Systems Register** — EU AI Act registry. Each entry shows: system name, vendor, risk classification, last reviewed, documentation status
- **Staff AI Literacy** — Training records. Each entry shows: staff member, training completed, attestation date
- **Vendor Register** — Third-party processors. Each entry shows: vendor name, type of processing, DPA status, last reviewed
- **DSAR Log** — Data subject requests. Each entry shows: request date, type, handler, response date, status

All records are pre-populated by the Executor from agent findings. Humans can edit any field. All edits are logged.

**Key design principle:** No record should ever need to be created from scratch. The agent fills in what it knows. The human confirms, corrects, or adds what the agent doesn't.

### Screen 5 — Settings & Notifications

Where the user configures how Kindlast reaches them and how the agents behave.

**Notification channels:**
- WhatsApp number (for proactive findings and urgent escalations)
- Slack workspace connection (for team routing)
- Email address (fallback)

**Notification preferences:**
- Which severity levels trigger an immediate notification vs. a batched weekly briefing
- Who receives which types of findings (founder, technical lead, HR lead, DPO)
- Quiet hours configuration

**Agent behaviour settings:**
- How frequently the Watcher runs (daily, twice daily)
- Snooze duration for "Remind me later" on findings
- Which integrations are active (Slack workspace, GitHub repository — later phases)

---

## 8. Notification & Outreach System

### Guiding Principle

The product lives in the user's existing channels. The app is where you go to review history and manage records — not where you go to find out something needs doing. The Comms Agent brings compliance to the user, not the other way around.

### WhatsApp Business

The primary notification channel for proactive findings and urgent escalations.

Every WhatsApp message follows the same structure:
- **What:** One sentence describing what was detected
- **Why it matters:** One sentence on the regulatory obligation
- **What to do:** The proposed action
- **Action buttons:** Approve / Reject / Remind me later (deep link back to the app for any action requiring more context)

Messages are sent only when there is something actionable. Kindlast does not send informational messages. Every message requires a response.

### Slack

Used for team-level routing. When a finding is relevant to a specific function (engineering, HR, operations), it is routed to the appropriate Slack channel or direct message.

Slack messages mirror the WhatsApp format but may include richer formatting — a short summary block with a link to the full finding in the app.

### Email

Fallback channel for users without WhatsApp Business connected. Also used for the monthly posture report — a PDF-ready summary of the client's compliance posture over the past 30 days, suitable for sharing with investors, a board, or an auditor.

### Scheduled Communications

| Communication | Frequency | Channel | Content |
|---|---|---|---|
| Compliance briefing | Weekly (Monday) | WhatsApp / Slack | Open items, upcoming deadlines, recent actions |
| Posture report | Monthly | Email | Full posture summary, exportable |
| Deadline alert | 30 days before | WhatsApp | Specific obligation, days remaining, proposed action |
| Escalation | When overdue | WhatsApp + Email | Overdue item, secondary contact copied |
| Inactivity nudge | After 14 days no action | WhatsApp | Summary of open items |

---

## 9. Regulatory Knowledge Base

### What It Contains

The agent's knowledge base is a curated, continuously-updated corpus of primary regulatory sources. It is not built from secondary summaries or vendor marketing content — only official sources.

**At launch, the knowledge base includes:**
- Full text of the General Data Protection Regulation (GDPR / EU 2016/679)
- Full text of the EU AI Act (EU 2024/1689), with particular depth on Articles 4, 6, 9–17, 26, 50, and Annex III
- The top 20 EDPB guidelines most relevant to SME processing activities (including consent, data transfers, DPO obligations, AI profiling, DSARs)
- Key enforcement decisions from the major national DPAs (ICO, CNIL, DPC, BfDI, AEPD) — illustrating how obligations translate into real-world enforcement
- EDPB's GDPR enforcement tracker summaries

**Ongoing maintenance:**
- The Watcher monitors primary regulatory sources for new publications
- New documents are ingested, processed, and added to the knowledge base automatically
- When a new document is added that is relevant to existing client profiles, the Watcher triggers the Analyst

### What the Knowledge Base Does Not Include

- National implementing legislation across all 27 member states (out of scope for MVP — focus is on EU-level obligations)
- Sector-specific regulations (healthcare, financial services, etc.)
- Non-EU regulations (CCPA, UK GDPR, etc.) — these are later-phase additions

---

## 10. Human-in-the-Loop Model

### The Principle

Kindlast agents observe, analyse, and propose. They never commit. Every action that changes a compliance record, generates a document, or logs an attestation requires an explicit approval from a named human user.

This is not a safety disclaimer — it is a core product design principle. Compliance records must be defensible. An audit trail that shows "approved by [User] on [Date]" is evidence. An audit trail that shows "auto-generated by AI" is not.

### Approval Levels

**One-tap approval** — for actions that are low-risk and reversible:
- Creating a draft ROPA entry
- Adding a vendor to the register
- Creating a DSAR tracking task

**Reviewed approval** — for actions that are higher-stakes or require human judgment:
- Classifying an AI system as high-risk under Annex III
- Marking a processing activity as requiring a DPIA
- Closing a DSAR as complete

**Explicit confirmation** — for actions that are irreversible or externally facing:
- Sending a DSAR response to a data subject
- Submitting a breach notification
- Marking a conformity assessment as complete

### What Happens on Rejection

When a user rejects a proposed action:
- The finding is archived with the rejection noted
- The user is asked for a brief reason (optional but encouraged)
- If the Watcher subsequently detects the same condition again, it re-surfaces the finding rather than creating a duplicate
- Rejection reasons are used to improve the Analyst's future proposals

---

## 11. Freemium & Pricing Model

### Rationale

The pricing model must be accessible enough to acquire SMEs who have never paid for compliance tooling before, while making the agentic value clear enough to drive conversion to paid plans.

Conversion should be triggered by a compliance event — not by a paywall. When the agent finds something that requires the paid tier to address, that is the moment to show the upgrade prompt. The urgency is real, the value is immediate.

### Tiers

**Free**
- Onboarding intake and initial posture assessment
- Up to 3 findings stored in the feed (older findings locked)
- ROPA with up to 3 processing activities
- Email notifications only
- No Executor access — the human must manually take action based on findings

**Pro — €49/month**
- Unlimited findings and full agent feed history
- Full ROPA, DPIA register, AI Systems Register, Vendor Register, DSAR log
- Executor access — one-tap approve to auto-populate records
- WhatsApp and Slack notifications
- Weekly compliance briefing
- Monthly posture report (PDF export)
- Watcher runs daily

**Business — €149/month**
- Everything in Pro
- Multi-user access — route findings to different team members by role
- Multiple Slack channel routing
- Custom notification preferences per user
- Priority support
- Watcher runs twice daily

**Enterprise — Custom pricing**
- Everything in Business
- Portfolio view — for outsourced DPOs managing multiple client organisations
- API access for integration with existing tools
- Dedicated onboarding
- SLA-backed support

### Conversion Triggers

The product should prompt upgrade at the following moments:
- When a Critical finding is detected and the user is on Free (findings are locked past 3)
- When a DSAR is received and the user needs Executor access to log the response
- When a deadline is approaching and the notification would go to WhatsApp but the user has not connected it
- When a new AI system is detected and the full Analyst output requires Pro to view

---

## 12. MVP Scope

### What Is In MVP

The MVP proves the core loop end-to-end: a client onboards, the agent finds something real, a notification reaches the user outside the app, the user approves an action in one tap, and a compliance record is created.

**In MVP:**
- Conversational onboarding intake
- Initial posture assessment output
- Watcher running on a daily schedule — monitoring regulatory corpus for changes relevant to the client profile
- Analyst producing findings from the regulatory knowledge base
- Agent feed UI with approve / reject / snooze
- Executor creating ROPA entries and DSAR tasks on approval
- Email notifications (WhatsApp Business in Phase 2)
- ROPA, DSAR Log, and AI Systems Register screens
- Compliance Dashboard (posture status, open items, upcoming deadlines)
- Free and Pro tiers
- Single user per account

### What Is Not In MVP

The following are explicitly deferred to Phase 2 or later:

| Feature | Reason for deferral |
|---|---|
| WhatsApp Business notifications | Meta Business API verification takes weeks — start with email |
| Slack integration | Second-priority channel — build after core loop is proven |
| GitHub / Jira webhook monitoring | Too much integration surface for MVP |
| Voice compliance briefings | Requires additional infrastructure and is not core to the agent loop |
| Multi-user / team routing | Single-user first — validate the product before adding organisational complexity |
| Portfolio view for DPOs | Secondary user segment — validate primary user first |
| DPIA full workflow | Complex process — start with DPIA task creation, not the full assessment workflow |
| UK GDPR / CCPA coverage | EU-first; international expansion is Phase 3 |
| Sector-specific guidance | Horizontal product first |

---

## 13. Success Metrics

### Activation
- % of new signups who complete the onboarding intake
- % of new signups who receive at least one finding within their first 7 days
- Time from signup to first finding delivered

### Engagement
- % of findings that receive a response (approve / reject / snooze) within 48 hours
- % of users who action a finding at least once per week
- % of users who receive a weekly briefing and open it

### Conversion
- Free-to-Pro conversion rate
- Primary conversion trigger (which event most commonly precedes upgrade)
- Days from signup to first paid conversion

### Retention
- Monthly retention rate at 30, 60, and 90 days
- % of Pro users still active at 90 days
- Churn reason distribution

### Compliance Outcomes (later stage)
- Average number of open compliance items per account over time
- Average time from finding detected to action approved
- Number of compliance records created per account per month

---

## 14. Open Questions

These are decisions that require either customer discovery or further deliberation before they can be resolved.

1. **What is the right first trigger for the Watcher in MVP?** Regulatory corpus changes require a seeded knowledge base. Should the first MVP version focus instead on a simpler trigger — the onboarding gaps themselves — to prove the notification loop without the full monitoring infrastructure?

2. **How does the onboarding intake handle companies that genuinely don't know what AI they use?** The shadow AI problem is real. Most founders will undercount their AI tools on first pass. What is the right follow-up mechanism — a periodic re-interview, or a tech stack scan?

3. **What is the minimum viable knowledge base for launch?** Full GDPR + EU AI Act text is large. Which specific articles and guidance documents deliver the highest-value findings for SMEs in the first 90 days?

4. **How should the product handle findings the user repeatedly rejects?** If a user rejects the same finding three times, does the agent stop surfacing it? Flag it differently? This has both UX and liability implications.

5. **What is the right escalation behaviour when a user goes dark?** If a user has not responded to findings in 30 days and a Critical item is open — what does the product do? Escalate to a secondary contact? Simply log and wait?

6. **Should the free tier include any Executor access?** The current proposal locks Executor behind Pro. The risk is that free users don't experience the magic of one-tap record creation — which is the primary conversion driver. Consider allowing one Executor action per month on free.

7. **Customer discovery is not done.** Before Phase 2 is scoped, at minimum 10 customer discovery conversations should be completed with EU SME founders to validate: the notification channel preference (WhatsApp vs. Slack vs. email), the willingness to grant tool access for shadow AI detection, and the price sensitivity at €49/month.

---

*End of Document*

**Next steps:**
- Customer discovery interviews (10 minimum) before Phase 2 scoping
- WhatsApp Business API application (submit immediately — approval takes 2–4 weeks)
- Knowledge base curation — identify the 20 highest-value regulatory documents for MVP corpus
- Design review of onboarding intake flow
