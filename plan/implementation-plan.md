# Kindlast MVP — Implementation Plan

## Context

Kindlast is an AI-native GDPR and EU AI Act compliance platform for EU SMEs. This plan builds the MVP from an empty repo to a deployable product across 5 sequential phases. Each phase produces a working increment.

**Core hypothesis:** SMEs will pay for an AI-powered tool that tells them exactly where they're non-compliant and what to fix.

---

## Phase 0: Project Bootstrap

**Goal:** Running Next.js 15 app with shadcn/ui, Supabase client, and all tooling configured.

### Steps

1. Initialize Next.js 15 with App Router, TypeScript, Tailwind CSS
2. Initialize shadcn/ui (New York style, CSS variables, Tailwind v4)
3. Install base shadcn components:
   ```
   npx shadcn add card badge button input label select textarea separator sonner
   ```
4. Install core dependencies:
   ```
   npm install @supabase/supabase-js @supabase/ssr ai @ai-sdk/google zod stripe @react-pdf/renderer
   ```
5. Create `.env.local` and `.env.example` with all keys from PRD section 13
6. Set up Supabase client helpers:
   - `lib/supabase/client.ts` — browser client (`createBrowserClient`)
   - `lib/supabase/server.ts` — server client (`createServerClient` with cookies)
   - `lib/supabase/middleware.ts` — middleware helper for session refresh

### Files
```
package.json, next.config.ts, tailwind.config.ts, tsconfig.json, components.json
app/layout.tsx, app/page.tsx, app/globals.css
lib/utils.ts, lib/supabase/client.ts, lib/supabase/server.ts, lib/supabase/middleware.ts
.env.local, .env.example, .gitignore
components/ui/* (shadcn base components)
```

### Verification
- `npm run dev` starts without errors
- Visit `http://localhost:3000` — see default page with Tailwind styling
- No TypeScript errors

---

## Phase 1: Database Schema + Auth

**Goal:** Working auth (email/password + Google OAuth), database tables with RLS, auth middleware.

### Steps

1. Create Supabase project, enable Email/Password and Google OAuth providers
2. Create migration `supabase/migrations/001_initial_schema.sql`:

   ```sql
   -- business_profiles
   CREATE TABLE business_profiles (
     id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
     user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE NOT NULL,
     company_name TEXT NOT NULL,
     country TEXT NOT NULL DEFAULT 'Estonia',
     industry TEXT,
     employee_count INTEGER,
     processes_personal_data BOOLEAN DEFAULT true,
     data_types TEXT[],
     uses_ai_systems BOOLEAN DEFAULT false,
     ai_system_descriptions JSONB,
     third_party_processors TEXT[],
     transfers_data_outside_eu BOOLEAN DEFAULT false,
     has_dpo BOOLEAN DEFAULT false,
     has_privacy_policy BOOLEAN DEFAULT false,
     has_cookie_consent BOOLEAN DEFAULT false,
     has_breach_notification BOOLEAN DEFAULT false,
     has_dsr_process BOOLEAN DEFAULT false,
     created_at TIMESTAMPTZ DEFAULT now(),
     updated_at TIMESTAMPTZ DEFAULT now(),
     UNIQUE(user_id)
   );
   ALTER TABLE business_profiles ENABLE ROW LEVEL SECURITY;
   CREATE POLICY "Users can CRUD own profiles" ON business_profiles
     USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

   -- assessments
   CREATE TABLE assessments (
     id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
     user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE NOT NULL,
     profile_id UUID REFERENCES business_profiles(id),
     type TEXT NOT NULL CHECK (type IN ('gdpr', 'ai_act')),
     status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'complete', 'error')),
     overall_score INTEGER,
     risk_level TEXT,
     result JSONB,
     created_at TIMESTAMPTZ DEFAULT now()
   );
   ALTER TABLE assessments ENABLE ROW LEVEL SECURITY;
   CREATE POLICY "Users can CRUD own assessments" ON assessments
     USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

   -- findings
   CREATE TABLE findings (
     id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
     assessment_id UUID REFERENCES assessments(id) ON DELETE CASCADE NOT NULL,
     user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE NOT NULL,
     category TEXT NOT NULL,
     severity TEXT NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low', 'pass')),
     title TEXT NOT NULL,
     description TEXT NOT NULL,
     recommendation TEXT NOT NULL,
     gdpr_article TEXT,
     ai_act_article TEXT,
     is_resolved BOOLEAN DEFAULT false,
     resolved_at TIMESTAMPTZ,
     created_at TIMESTAMPTZ DEFAULT now()
   );
   ALTER TABLE findings ENABLE ROW LEVEL SECURITY;
   CREATE POLICY "Users can CRUD own findings" ON findings
     USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

   -- subscriptions (users can only read; service role manages via webhook)
   CREATE TABLE subscriptions (
     id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
     user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE NOT NULL,
     stripe_customer_id TEXT,
     stripe_subscription_id TEXT,
     plan TEXT NOT NULL DEFAULT 'free' CHECK (plan IN ('free', 'premium')),
     status TEXT NOT NULL DEFAULT 'active',
     current_period_end TIMESTAMPTZ,
     created_at TIMESTAMPTZ DEFAULT now(),
     UNIQUE(user_id)
   );
   ALTER TABLE subscriptions ENABLE ROW LEVEL SECURITY;
   CREATE POLICY "Users can read own subscription" ON subscriptions
     FOR SELECT USING (auth.uid() = user_id);

   -- Auto-create free subscription on signup
   CREATE OR REPLACE FUNCTION handle_new_user()
   RETURNS TRIGGER AS $$
   BEGIN
     INSERT INTO subscriptions (user_id, plan, status)
     VALUES (NEW.id, 'free', 'active');
     RETURN NEW;
   END;
   $$ LANGUAGE plpgsql SECURITY DEFINER;

   CREATE TRIGGER on_auth_user_created
     AFTER INSERT ON auth.users
     FOR EACH ROW EXECUTE FUNCTION handle_new_user();

   -- Auto-update updated_at
   CREATE OR REPLACE FUNCTION update_updated_at()
   RETURNS TRIGGER AS $$
   BEGIN NEW.updated_at = now(); RETURN NEW; END;
   $$ LANGUAGE plpgsql;

   CREATE TRIGGER set_updated_at
     BEFORE UPDATE ON business_profiles
     FOR EACH ROW EXECUTE FUNCTION update_updated_at();
   ```

3. Build login/signup page at `app/(public)/login/page.tsx` — two tabs (Login/Sign Up) + Google OAuth button
4. Create auth server actions at `lib/auth/actions.ts` — signUp, signIn, signInWithGoogle, signOut
5. Create OAuth callback route at `app/auth/callback/route.ts`
6. Create `middleware.ts` — session refresh, protect `/dashboard/*`, redirect authenticated users away from `/login`
7. Create TypeScript types at `lib/types/database.ts`

### Files
```
supabase/migrations/001_initial_schema.sql
app/(public)/login/page.tsx
lib/auth/actions.ts
app/auth/callback/route.ts
middleware.ts
lib/types/database.ts
```

### Verification
- Sign up with email/password — user appears in Supabase Auth
- Sign in redirects to `/dashboard`
- Unauthenticated `/dashboard` visit redirects to `/login`
- `subscriptions` table auto-creates `free` row for new users

---

## Phase 2: Dashboard Layout + Onboarding Wizard

**Goal:** Sidebar dashboard layout. New users routed to 4-step onboarding wizard that saves business profile.

### Steps

1. Install shadcn components:
   ```
   npx shadcn add sidebar sheet tooltip avatar dropdown-menu tabs form progress checkbox radio-group switch
   ```
2. Create dashboard layout `app/(dashboard)/layout.tsx` — auth check, fetch profile/subscription, redirect to onboarding if no profile
3. Create sidebar nav `components/dashboard/sidebar-nav.tsx` — Dashboard, Findings, AI Act (premium badge), Export (premium badge), Settings
4. Create onboarding wizard `app/(dashboard)/dashboard/onboarding/page.tsx` — client component, 4-step form with progress indicator
5. Define Zod schemas `lib/schemas/onboarding.ts`:
   - Step 1: company_name, country, industry, employee_count
   - Step 2: processes_personal_data, data_types, third_party_processors, transfers_data_outside_eu
   - Step 3: has_privacy_policy, has_cookie_consent, has_dpo, has_breach_notification, has_dsr_process
   - Step 4: uses_ai_systems, ai_system_descriptions (conditional)
6. Create step components: `components/onboarding/step-company.tsx`, `step-data.tsx`, `step-compliance.tsx`, `step-ai-systems.tsx`, `wizard-progress.tsx`
7. Create server actions `app/(dashboard)/dashboard/onboarding/actions.ts` — saveBusinessProfile, completeOnboarding
8. Create reusable DB queries `lib/supabase/queries.ts` — getBusinessProfile, getLatestAssessment, getFindings, getSubscription

### Files
```
app/(dashboard)/layout.tsx
app/(dashboard)/dashboard/page.tsx (placeholder)
app/(dashboard)/dashboard/onboarding/page.tsx
app/(dashboard)/dashboard/onboarding/actions.ts
components/dashboard/sidebar-nav.tsx
components/onboarding/step-company.tsx
components/onboarding/step-data.tsx
components/onboarding/step-compliance.tsx
components/onboarding/step-ai-systems.tsx
components/onboarding/wizard-progress.tsx
lib/schemas/onboarding.ts
lib/supabase/queries.ts
```

### Verification
- New user redirected to `/dashboard/onboarding`
- Complete 4 steps — data saves to `business_profiles`
- Step 4 conditionally shows AI system fields
- Returning user goes directly to `/dashboard`
- Validation errors display for missing required fields

---

## Phase 3: AI Assessment + Dashboard UI

**Goal:** AI runs GDPR assessment after onboarding. Dashboard shows compliance score, risk level, and findings.

### Steps

1. Create AI assessment module `lib/ai/assess-gdpr.ts` — `assessGDPRCompliance(profile)` using `generateObject` with Gemini 2.5 Flash and Zod schemas from PRD
2. Create AI output schemas `lib/ai/schemas.ts` — FindingSchema, AssessmentResultSchema
3. Create assessment API route `app/api/assess/route.ts` — POST, authenticates, creates pending assessment, runs AI, saves results + findings
4. Wire onboarding completion to trigger first assessment with loading state ("Analyzing your compliance posture...")
5. Install: `npx shadcn add chart table dialog`
6. Build dashboard page `app/(dashboard)/dashboard/page.tsx` — server component fetching latest assessment
7. Build dashboard components:
   - `components/dashboard/score-card.tsx` — 0-100 score, color-coded, risk badge
   - `components/dashboard/findings-summary.tsx` — counts by severity
   - `components/dashboard/recent-findings.tsx` — top 3-5 finding cards
   - `components/dashboard/assessment-status.tsx` — processing indicator
   - `components/dashboard/legal-disclaimer.tsx` — mandatory disclaimer from PRD section 17
8. Build findings page `app/(dashboard)/dashboard/findings/page.tsx` — filterable list
9. Build finding components:
   - `components/findings/finding-card.tsx` — severity, title, description, recommendation, GDPR article
   - `components/findings/finding-filters.tsx` — severity/category dropdowns
   - `components/findings/resolve-button.tsx` — toggles is_resolved
10. Create findings server actions `app/(dashboard)/dashboard/findings/actions.ts` — toggleFindingResolved, rerunAssessment

### Files
```
lib/ai/assess-gdpr.ts
lib/ai/schemas.ts
app/api/assess/route.ts
app/(dashboard)/dashboard/page.tsx
app/(dashboard)/dashboard/findings/page.tsx
app/(dashboard)/dashboard/findings/actions.ts
components/dashboard/score-card.tsx
components/dashboard/findings-summary.tsx
components/dashboard/recent-findings.tsx
components/dashboard/assessment-status.tsx
components/dashboard/legal-disclaimer.tsx
components/findings/finding-card.tsx
components/findings/finding-filters.tsx
components/findings/resolve-button.tsx
```

### Verification
- Complete onboarding → see "Analyzing..." loading state
- Dashboard shows compliance score (0-100), color-coded
- Findings page lists all findings with correct severity badges
- "Mark as Resolved" updates finding in DB and UI
- Legal disclaimer visible on dashboard and findings pages
- Different business profiles produce varied AI results

---

## Phase 4: Premium Features — Stripe, AI Act, PDF Export

**Goal:** Stripe payments, freemium gating, AI Act classification, PDF export.

### Steps

1. Create Stripe utilities `lib/stripe/index.ts` — client init, createCheckoutSession, createCustomerPortalSession
2. Create Stripe webhook `app/api/webhooks/stripe/route.ts` — verify signature, handle checkout.session.completed, subscription.updated, subscription.deleted
3. Create gating utility `lib/subscription/gate.ts` — checkPremium, PremiumGate wrapper component
4. Create upgrade prompt `components/premium/upgrade-prompt.tsx`
5. Create Stripe server actions `lib/stripe/actions.ts` — createCheckout, createPortalSession
6. Apply gating to findings page — free users see top 3 fully, rest blurred with upgrade overlay
7. Build AI Act module:
   - `lib/ai/classify-ai-risk.ts` — classifyAIRisk using generateObject with AIActClassificationSchema
   - `app/api/classify/route.ts` — POST, premium-gated
   - `app/(dashboard)/dashboard/ai-act/page.tsx` — risk classifications per AI system
   - `components/ai-act/risk-tier-card.tsx`, `risk-tier-badge.tsx`
8. Build PDF export:
   - `lib/pdf/compliance-report.tsx` — React PDF document (cover, summary, findings, disclaimer)
   - `app/api/export/route.ts` — GET, premium-gated, renders PDF stream
   - `app/(dashboard)/dashboard/export/page.tsx` — generate + download button
9. Build settings page `app/(dashboard)/dashboard/settings/page.tsx` — edit profile, subscription status, manage billing
10. Build pricing page `app/(public)/pricing/page.tsx` — feature comparison table, CTAs

### Files
```
lib/stripe/index.ts
lib/stripe/actions.ts
app/api/webhooks/stripe/route.ts
lib/subscription/gate.ts
components/premium/upgrade-prompt.tsx
components/findings/blurred-finding.tsx
lib/ai/classify-ai-risk.ts
app/api/classify/route.ts
app/(dashboard)/dashboard/ai-act/page.tsx
components/ai-act/risk-tier-card.tsx
components/ai-act/risk-tier-badge.tsx
lib/pdf/compliance-report.tsx
app/api/export/route.ts
app/(dashboard)/dashboard/export/page.tsx
app/(dashboard)/dashboard/settings/page.tsx
app/(public)/pricing/page.tsx
```

### Verification
- Free user sees top 3 findings; rest blurred with upgrade prompt
- Stripe Checkout flow works in test mode
- Webhook updates subscription to premium
- Premium user sees all findings, AI Act page, Export page
- PDF downloads with correct formatting
- Settings shows subscription status + manage billing link

---

## Phase 5: Landing Page, Polish, Launch

**Goal:** Public landing page, error handling, loading states, mobile responsiveness, SEO, deploy.

### Steps

1. Build landing page `app/(public)/page.tsx` — hero, problem statement, how-it-works, features grid, pricing preview, footer
2. Create public layout `app/(public)/layout.tsx` — header with nav, footer
3. Add loading skeletons: `npx shadcn add skeleton`, create `loading.tsx` files for dashboard, findings, onboarding
4. Add error boundaries: `error.tsx` for dashboard + findings, global `app/error.tsx`, `app/not-found.tsx`
5. Mobile responsiveness pass — collapsible sidebar, responsive cards, stacked layouts
6. SEO metadata — title, description, Open Graph in layout and page metadata exports
7. Add `app/robots.ts` and `app/sitemap.ts`
8. Add Sonner `<Toaster />` to root layout, toast notifications for key actions
9. Re-assessment flow — "Re-run Assessment" button with free tier limit (1/month)
10. Deploy to Vercel — env vars, Stripe webhook URL, Google OAuth redirect URIs, Supabase auth URLs

### Files
```
app/(public)/page.tsx
app/(public)/layout.tsx
components/landing/hero.tsx
components/landing/features.tsx
components/landing/how-it-works.tsx
components/landing/footer.tsx
app/(dashboard)/dashboard/loading.tsx
app/(dashboard)/dashboard/findings/loading.tsx
app/(dashboard)/dashboard/onboarding/loading.tsx
app/(dashboard)/dashboard/error.tsx
app/error.tsx
app/not-found.tsx
app/robots.ts
app/sitemap.ts
```

### Verification
- Landing page is professional and mobile-responsive
- Loading skeletons appear during data fetching
- Error boundaries catch and display errors gracefully
- Full end-to-end flow works: landing → signup → onboarding → assessment → dashboard → findings → upgrade → premium features → PDF export
- Deploy to Vercel staging succeeds

---

## Phase Dependency Chain

```
Phase 0 (Bootstrap) → Phase 1 (Auth + DB) → Phase 2 (Layout + Onboarding) → Phase 3 (AI + Dashboard) → Phase 4 (Premium) → Phase 5 (Polish + Launch)
```

Each phase must complete before the next begins.

## Key Design Decisions

1. **Server actions over API routes** where possible — API routes only for long-running AI calls and webhooks
2. **Subscription check at layout level** — fetched once in `(dashboard)/layout.tsx`, passed via context
3. **PDF rendered server-side only** — `/api/export` uses `renderToStream`, keeps library out of client bundle
4. **Onboarding redirect via data check** — layout checks for `business_profiles` row, no separate flag needed
5. **Free tier blur is UX, not security** — data is fetched server-side (user owns it), blur is a conversion nudge
