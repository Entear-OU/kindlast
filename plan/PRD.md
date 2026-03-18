# Kindlast MVP — Product Requirements Document

## 1. Product Overview

**Kindlast** is an AI-native GDPR and EU AI Act compliance platform for EU SMEs. The MVP validates one core hypothesis: *SMEs will pay for an AI-powered tool that tells them exactly where they're non-compliant and what to fix.*

**One-liner:** Two regulations, one platform, zero guesswork.

---

## 2. MVP Scope — What We're Building (and What We're Not)

### In Scope (MVP)
- GDPR compliance assessment via guided onboarding wizard
- AI-powered gap analysis with actionable fix-it recommendations
- Compliance score dashboard
- EU AI Act risk classification (lightweight — classify AI systems by risk tier)
- PDF export of compliance report (audit-ready artifact)
- Auth + freemium gating

### Out of Scope (Post-MVP)
- Multi-user / team accounts
- Estonian tax / governance compliance
- Full AI Act conformity assessment workflows
- Integrations (Slack, email alerts, calendar reminders)
- Multilingual support (English-only for MVP)
- Custom branding / white-label
- SOC 2 / ISO 27001 modules

---

## 3. Target User

**Primary:** EU-based SME founder or operations lead (1–50 employees) who knows they need to comply with GDPR but hasn't started, can't afford a consultant, and doesn't have in-house legal.

**Secondary:** e-Residents operating Estonian companies who need EU regulatory guidance.

**User Persona — "Marta":**
- Runs a 12-person SaaS company in Tallinn
- Collects customer data, uses analytics, runs email campaigns
- Knows GDPR exists, vaguely aware of the AI Act
- Has no DPO, no legal team, no compliance budget beyond €100/month
- Wants to know: "Am I compliant? If not, what do I fix first?"

---

## 4. Tech Stack

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| **Framework** | Next.js 15 (App Router) | Server components, server actions, RSC streaming |
| **UI** | shadcn/ui + Tailwind CSS v4 | `dashboard-01` block for layout, `sidebar-01` for nav. Copy-paste, full control |
| **AI** | Vercel AI SDK (`ai` + `@ai-sdk/google`) | `generateObject` for structured compliance analysis, `streamText` for chat-style recommendations |
| **Model** | Google Gemini (`gemini-2.5-flash`) | Fast, cost-effective, good at structured output. Swap to `gemini-2.5-pro` for complex analysis if needed |
| **Database** | Supabase (Postgres + Auth + Row Level Security) | Auth out of the box, RLS for multi-tenant data isolation, real-time subscriptions |
| **Hosting** | Vercel | Zero-config Next.js deployment, edge functions, analytics |
| **Payments** | Stripe (Checkout + Customer Portal) | Freemium gating, subscription management |
| **PDF Export** | `@react-pdf/renderer` | Server-side compliance report generation |
| **Validation** | Zod | Schema validation for forms, AI structured output, API contracts |

---

## 5. Architecture

```
┌─────────────────────────────────────────────────┐
│                   NEXT.JS APP                    │
│                                                  │
│  ┌──────────┐  ┌──────────────┐  ┌───────────┐ │
│  │  Public   │  │  Dashboard   │  │   API      │ │
│  │  Landing  │  │  (Protected) │  │  Routes    │ │
│  │  /login   │  │  /dashboard  │  │  /api/...  │ │
│  └──────────┘  └──────────────┘  └───────────┘ │
│                       │                │         │
│              Server Components    Server Actions │
│              + Server Actions     + Route Handlers│
│                       │                │         │
│                       ▼                ▼         │
│              ┌─────────────────────────┐         │
│              │    AI LAYER             │         │
│              │    Vercel AI SDK        │         │
│              │    @ai-sdk/google       │         │
│              │    (Gemini 2.5 Flash)   │         │
│              └─────────────────────────┘         │
│                       │                          │
│                       ▼                          │
│              ┌─────────────────────────┐         │
│              │    SUPABASE             │         │
│              │    Auth + Postgres + RLS│         │
│              └─────────────────────────┘         │
└─────────────────────────────────────────────────┘
```

### Key Architecture Decisions

**Server Components First:** All data fetching and AI calls happen in server components or server actions. Client components are only for interactive UI (forms, toggles, chat input). This keeps the AI API key server-side and reduces client JS.

**Single AI Agent (MVP):** One Gemini call per assessment using `generateObject` with a Zod schema. No multi-agent orchestration for MVP — it adds complexity without validated user value. The AI receives the user's business profile + assessment answers and returns a structured compliance result.

**Supabase RLS:** Every table has a `user_id` column. RLS policies ensure users can only read/write their own data. No API-level auth checks needed beyond Supabase session.

---

## 6. Data Model (Supabase / Postgres)

```sql
-- User's business profile (collected during onboarding)
CREATE TABLE business_profiles (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
  company_name TEXT NOT NULL,
  country TEXT NOT NULL DEFAULT 'Estonia',
  industry TEXT,
  employee_count INTEGER,
  processes_personal_data BOOLEAN DEFAULT true,
  data_types TEXT[],              -- e.g., {'email', 'payment', 'health', 'biometric'}
  uses_ai_systems BOOLEAN DEFAULT false,
  ai_system_descriptions JSONB,  -- [{name, purpose, dataUsed, isAutomatedDecision}]
  third_party_processors TEXT[],  -- e.g., {'Stripe', 'Google Analytics', 'Mailchimp'}
  transfers_data_outside_eu BOOLEAN DEFAULT false,
  has_dpo BOOLEAN DEFAULT false,
  has_privacy_policy BOOLEAN DEFAULT false,
  has_cookie_consent BOOLEAN DEFAULT false,
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id)
);

-- Individual compliance assessment run
CREATE TABLE assessments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
  profile_id UUID REFERENCES business_profiles(id),
  type TEXT NOT NULL CHECK (type IN ('gdpr', 'ai_act')),
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'complete', 'error')),
  overall_score INTEGER,          -- 0-100
  risk_level TEXT,                -- 'low', 'medium', 'high', 'critical'
  result JSONB,                   -- Full structured AI response
  created_at TIMESTAMPTZ DEFAULT now()
);

-- Individual findings from an assessment
CREATE TABLE findings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  assessment_id UUID REFERENCES assessments(id) ON DELETE CASCADE,
  user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
  category TEXT NOT NULL,          -- e.g., 'lawful_basis', 'data_subject_rights', 'security'
  severity TEXT NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low', 'pass')),
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  recommendation TEXT NOT NULL,
  gdpr_article TEXT,               -- e.g., 'Art. 6', 'Art. 13'
  ai_act_article TEXT,             -- e.g., 'Art. 6 (Prohibited)', 'Art. 9 (High-Risk)'
  is_resolved BOOLEAN DEFAULT false,
  resolved_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT now()
);

-- Subscription / feature gating
CREATE TABLE subscriptions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
  stripe_customer_id TEXT,
  stripe_subscription_id TEXT,
  plan TEXT NOT NULL DEFAULT 'free' CHECK (plan IN ('free', 'premium')),
  status TEXT NOT NULL DEFAULT 'active',
  current_period_end TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE(user_id)
);

-- RLS Policies (applied to all tables above)
-- ALTER TABLE business_profiles ENABLE ROW LEVEL SECURITY;
-- CREATE POLICY "Users can CRUD own data" ON business_profiles
--   USING (auth.uid() = user_id)
--   WITH CHECK (auth.uid() = user_id);
```

---

## 7. Core User Flows

### Flow 1: Onboarding Wizard → First Assessment (Critical Path)

This is the MVP's make-or-break flow. If a user doesn't complete onboarding and see a compliance score within 10 minutes, we've lost them.

```
Sign Up (Supabase Auth — email/password or Google OAuth)
  │
  ▼
Onboarding Wizard (4 steps, server actions to save progress)
  │
  ├─ Step 1: Company Basics
  │   - Company name, country, industry, employee count
  │
  ├─ Step 2: Data Processing
  │   - Do you collect personal data? What types?
  │   - Third-party tools/processors used
  │   - Transfer data outside EU?
  │
  ├─ Step 3: Current Compliance State
  │   - Have a privacy policy? Cookie consent? DPO?
  │   - Breach notification process?
  │   - Data subject request handling?
  │
  ├─ Step 4: AI Systems (conditional — only if uses_ai_systems = true)
  │   - Describe each AI system: name, purpose, data used
  │   - Automated decision-making?
  │
  ▼
Assessment Processing (server action)
  │
  ├─ Build prompt from business_profile data
  ├─ Call Gemini via AI SDK generateObject()
  ├─ Parse structured response into findings
  ├─ Calculate overall_score
  ├─ Save assessment + findings to DB
  │
  ▼
Dashboard — Compliance Score + Findings List
```

### Flow 2: Dashboard (Post-Assessment)

```
/dashboard
  │
  ├─ Compliance Score Card (0-100, color-coded)
  ├─ Risk Level Badge (Low / Medium / High / Critical)
  ├─ Findings Summary (X critical, Y high, Z medium)
  │
  ├─ /dashboard/findings
  │   ├─ Filterable list of all findings
  │   ├─ Each finding: severity, title, description, recommendation, GDPR article
  │   ├─ "Mark as Resolved" toggle
  │   └─ Re-run assessment button
  │
  ├─ /dashboard/ai-act (premium only)
  │   ├─ AI system risk classification results
  │   ├─ Risk tier: Unacceptable / High / Limited / Minimal
  │   └─ Obligations summary per system
  │
  └─ /dashboard/export (premium only)
      └─ Generate & download PDF compliance report
```

### Flow 3: Premium Upgrade

```
User hits gated feature (AI Act module, PDF export, re-assessment)
  │
  ▼
Upgrade prompt → Stripe Checkout (€49/month)
  │
  ▼
Webhook → Update subscriptions table → Unlock features
```

---

## 8. AI Layer — Prompt Architecture

### GDPR Assessment Prompt

The core AI call uses `generateObject` from the AI SDK with a Zod schema to guarantee structured output.

```typescript
// lib/ai/assess-gdpr.ts
import { generateObject } from 'ai';
import { google } from '@ai-sdk/google';
import { z } from 'zod';

const FindingSchema = z.object({
  category: z.enum([
    'lawful_basis',
    'consent',
    'data_subject_rights',
    'privacy_policy',
    'data_security',
    'breach_notification',
    'data_processing_records',
    'dpo_requirement',
    'cross_border_transfers',
    'cookie_compliance',
    'children_data',
    'data_minimization',
  ]),
  severity: z.enum(['critical', 'high', 'medium', 'low', 'pass']),
  title: z.string(),
  description: z.string(),
  recommendation: z.string(),
  gdpr_article: z.string(),
});

const AssessmentResultSchema = z.object({
  overall_score: z.number().min(0).max(100),
  risk_level: z.enum(['low', 'medium', 'high', 'critical']),
  summary: z.string(),
  findings: z.array(FindingSchema),
});

export async function assessGDPRCompliance(profile: BusinessProfile) {
  const { object } = await generateObject({
    model: google('gemini-2.5-flash'),
    schema: AssessmentResultSchema,
    system: `You are an expert EU data protection consultant specializing in GDPR compliance 
for small and medium-sized enterprises. You assess businesses against the full scope of the 
General Data Protection Regulation (EU) 2016/679.

Your assessment must be:
- Specific to the business context provided (industry, size, data types, tools)
- Actionable — every finding must include a concrete next step the business can take
- Accurate — cite the specific GDPR article for each finding
- Proportionate — consider the business size when assessing risk severity

Score rubric:
- 90-100: Largely compliant, minor improvements needed
- 70-89: Mostly compliant, some significant gaps
- 50-69: Partially compliant, multiple areas need attention
- 30-49: Significant non-compliance risks
- 0-29: Critical non-compliance, immediate action required`,

    prompt: `Assess the GDPR compliance of the following business:

Company: ${profile.company_name}
Country: ${profile.country}
Industry: ${profile.industry}
Employees: ${profile.employee_count}

Data Processing:
- Collects personal data: ${profile.processes_personal_data}
- Data types collected: ${profile.data_types?.join(', ')}
- Third-party processors: ${profile.third_party_processors?.join(', ')}
- Transfers data outside EU: ${profile.transfers_data_outside_eu}

Current Compliance Measures:
- Has privacy policy: ${profile.has_privacy_policy}
- Has cookie consent: ${profile.has_cookie_consent}
- Has DPO: ${profile.has_dpo}

AI Systems: ${profile.uses_ai_systems ? JSON.stringify(profile.ai_system_descriptions) : 'None'}

Provide a comprehensive GDPR compliance assessment with specific findings and actionable recommendations.`,
  });

  return object;
}
```

### AI Act Risk Classification Prompt

```typescript
// lib/ai/classify-ai-risk.ts
const AIActClassificationSchema = z.object({
  systems: z.array(z.object({
    name: z.string(),
    risk_tier: z.enum(['unacceptable', 'high', 'limited', 'minimal']),
    reasoning: z.string(),
    obligations: z.array(z.string()),
    ai_act_articles: z.array(z.string()),
    deadline: z.string(),
  })),
  overall_summary: z.string(),
});
```

---

## 9. Page Structure & Routing

```
app/
├── (public)/
│   ├── page.tsx                    # Landing page
│   ├── login/page.tsx              # Auth (Supabase login/signup)
│   └── pricing/page.tsx            # Pricing page
│
├── (dashboard)/
│   ├── layout.tsx                  # Sidebar layout (shadcn sidebar-01)
│   ├── dashboard/
│   │   ├── page.tsx                # Overview — score + summary cards
│   │   ├── onboarding/
│   │   │   └── page.tsx            # Onboarding wizard (multi-step form)
│   │   ├── findings/
│   │   │   └── page.tsx            # Findings list with filters
│   │   ├── ai-act/
│   │   │   └── page.tsx            # AI Act risk classification (premium)
│   │   ├── export/
│   │   │   └── page.tsx            # PDF report export (premium)
│   │   └── settings/
│   │       └── page.tsx            # Business profile edit + billing
│
├── api/
│   ├── assess/route.ts             # POST — trigger GDPR assessment
│   ├── classify/route.ts           # POST — AI Act classification (premium)
│   ├── export/route.ts             # GET — generate PDF report (premium)
│   └── webhooks/
│       └── stripe/route.ts         # Stripe webhook handler
│
├── layout.tsx                       # Root layout
└── middleware.ts                    # Auth redirect (Supabase middleware)
```

---

## 10. UI Components (shadcn/ui)

### Install Commands

```bash
# Base setup
npx shadcn@latest init

# Dashboard layout
npx shadcn add dashboard-01        # Full dashboard with sidebar + charts
npx shadcn add sidebar-01          # Simple sidebar nav

# Core components needed
npx shadcn add card badge button input label select textarea
npx shadcn add progress tabs table dialog sheet separator
npx shadcn add form                 # React Hook Form + Zod integration
npx shadcn add chart                # Recharts wrapper for score visualization
npx shadcn add sonner               # Toast notifications
```

### Key UI Patterns

**Compliance Score Card:**
```
┌─────────────────────────────────┐
│  Your GDPR Compliance Score     │
│                                 │
│         ┌──────┐               │
│         │  67  │  MEDIUM RISK  │
│         └──────┘               │
│  ████████████░░░░░  67/100     │
│                                 │
│  3 Critical  ·  5 High  ·  12 Medium
└─────────────────────────────────┘
```

**Findings Card:**
```
┌─────────────────────────────────────┐
│ 🔴 CRITICAL  ·  Art. 6 GDPR        │
│ No lawful basis documented          │
│                                     │
│ You collect email addresses via     │
│ signup forms but haven't documented │
│ which lawful basis applies...       │
│                                     │
│ ✅ Recommendation:                  │
│ Document "consent" as your lawful   │
│ basis for marketing emails and...   │
│                                     │
│ [ Mark Resolved ]                   │
└─────────────────────────────────────┘
```

---

## 11. Feature Gating (Free vs Premium)

| Feature | Free | Premium (€49/mo) |
|---------|------|-------------------|
| Onboarding wizard | ✅ | ✅ |
| GDPR compliance score | ✅ | ✅ |
| Top 3 critical findings | ✅ | ✅ |
| Full findings list | ❌ (blurred) | ✅ |
| AI-powered recommendations | ❌ | ✅ |
| AI Act risk classification | ❌ | ✅ |
| PDF compliance report export | ❌ | ✅ |
| Re-run assessment | 1x / month | Unlimited |
| Regulatory update alerts | ❌ | ✅ |

**Gating UX:** Free users see all findings titles but descriptions/recommendations are blurred with a "Upgrade to see full analysis" overlay. This creates a clear value preview.

---

## 12. Server Actions (Key Implementation Patterns)

```typescript
// app/(dashboard)/dashboard/onboarding/actions.ts
'use server'

import { createClient } from '@/lib/supabase/server'
import { assessGDPRCompliance } from '@/lib/ai/assess-gdpr'
import { revalidatePath } from 'next/cache'

export async function saveBusinessProfile(formData: BusinessProfileInput) {
  const supabase = await createClient()
  const { data: { user } } = await supabase.auth.getUser()
  if (!user) throw new Error('Unauthorized')

  const { data: profile } = await supabase
    .from('business_profiles')
    .upsert({ ...formData, user_id: user.id })
    .select()
    .single()

  return profile
}

export async function runAssessment(profileId: string) {
  const supabase = await createClient()
  const { data: { user } } = await supabase.auth.getUser()
  if (!user) throw new Error('Unauthorized')

  // Get profile
  const { data: profile } = await supabase
    .from('business_profiles')
    .select()
    .eq('id', profileId)
    .single()

  // Create pending assessment
  const { data: assessment } = await supabase
    .from('assessments')
    .insert({
      user_id: user.id,
      profile_id: profileId,
      type: 'gdpr',
      status: 'processing',
    })
    .select()
    .single()

  // Run AI assessment
  const result = await assessGDPRCompliance(profile)

  // Save results
  await supabase
    .from('assessments')
    .update({
      status: 'complete',
      overall_score: result.overall_score,
      risk_level: result.risk_level,
      result: result,
    })
    .eq('id', assessment.id)

  // Save individual findings
  const findings = result.findings.map(f => ({
    assessment_id: assessment.id,
    user_id: user.id,
    ...f,
  }))

  await supabase.from('findings').insert(findings)

  revalidatePath('/dashboard')
  return assessment.id
}
```

---

## 13. Environment Variables

```env
# Supabase
NEXT_PUBLIC_SUPABASE_URL=
NEXT_PUBLIC_SUPABASE_ANON_KEY=
SUPABASE_SERVICE_ROLE_KEY=

# Google AI (Gemini)
GOOGLE_GENERATIVE_AI_API_KEY=

# Stripe
STRIPE_SECRET_KEY=
STRIPE_WEBHOOK_SECRET=
NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY=
STRIPE_PRICE_ID_PREMIUM=

# App
NEXT_PUBLIC_APP_URL=https://kindlast.com
```

---

## 14. Build Plan (6-Week MVP Sprint)

### Week 1-2: Foundation
- [ ] Next.js 15 project setup with App Router
- [ ] shadcn/ui init + dashboard-01 block + sidebar
- [ ] Supabase project: auth, database schema, RLS policies
- [ ] Auth flow: signup, login, logout, middleware redirect
- [ ] Landing page (public)

### Week 3-4: Core Intelligence
- [ ] Onboarding wizard (4-step multi-step form with Zod validation)
- [ ] GDPR assessment AI prompt + `generateObject` integration
- [ ] Assessment processing server action
- [ ] Dashboard: compliance score card + findings list
- [ ] Finding detail view with recommendations
- [ ] "Mark as resolved" toggle

### Week 5: Premium & Monetization
- [ ] Stripe integration: checkout, webhook, customer portal
- [ ] Feature gating logic (free vs premium)
- [ ] AI Act risk classification module (premium)
- [ ] PDF compliance report export (premium)

### Week 6: Polish & Launch Prep
- [ ] Loading states, error handling, edge cases
- [ ] Mobile responsiveness
- [ ] SEO meta tags + Open Graph
- [ ] Deploy to Vercel (production)
- [ ] Seed 10 beta testers for feedback

---

## 15. Success Metrics (First 90 Days Post-Launch)

| Metric | Target | Why It Matters |
|--------|--------|----------------|
| Signups | 200 | Market interest signal |
| Onboarding completion rate | >60% | Wizard is usable and not too long |
| Time to first score | <10 min | Core value delivered fast |
| Free → Premium conversion | >3% | Willingness to pay validated |
| Monthly active users | 50+ | Retention signal |
| NPS from beta users | >40 | Product-market fit signal |

---

## 16. Risk Register

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| AI hallucinations in compliance advice | Medium | High | Zod schema constrains output structure; system prompt anchors to specific GDPR articles; disclaimer that tool is guidance, not legal advice |
| Gemini output quality insufficient | Medium | High | Prompt engineering iteration; fallback to `gemini-2.5-pro` for complex cases; human review of first 50 assessments |
| Low onboarding completion | Medium | High | Keep wizard to 4 steps max; save progress server-side; show progress bar |
| Stripe integration delays | Low | Medium | Use Stripe Checkout (hosted) — minimal custom code |
| Supabase RLS misconfiguration | Low | Critical | Test RLS policies with multiple test accounts; automated test suite |
| GDPR applies to us too | Certain | Medium | Privacy policy, cookie consent, DPA with Supabase, data minimization in our own DB |

---

## 17. Legal Disclaimer

Every assessment result page must display:

> *Kindlast provides AI-generated compliance guidance for educational and planning purposes. It is not a substitute for professional legal advice. For binding compliance determinations, consult a qualified data protection attorney or certified DPO.*

This is non-negotiable. It appears on the dashboard, every finding card, and every exported PDF.
