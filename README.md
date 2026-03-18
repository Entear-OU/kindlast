# Kindlast

AI-powered GDPR & EU AI Act compliance platform for EU SMEs.

**Two regulations, one platform, zero guesswork.**

## Features

- **GDPR Compliance Assessment** — AI-powered gap analysis with actionable recommendations
- **Compliance Score Dashboard** — 0-100 score with color-coded risk levels
- **Findings & Recommendations** — Detailed findings with specific GDPR article references
- **EU AI Act Risk Classification** — Classify AI systems by risk tier (premium)
- **PDF Compliance Report** — Audit-ready PDF export (premium)
- **Freemium Model** — Free tier with core features, premium at €49/mo

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Framework | Next.js 15 (App Router) |
| UI | shadcn/ui + Tailwind CSS v4 |
| AI | Vercel AI SDK + Google Gemini 2.5 Flash |
| Database | Supabase (Postgres + Auth + RLS) |
| Payments | Stripe (Checkout + Customer Portal) |
| PDF Export | @react-pdf/renderer |
| Validation | Zod |
| Testing | Vitest + React Testing Library |

## Getting Started

### Prerequisites

- Node.js 18+
- pnpm
- Supabase project
- Google AI API key (Gemini)
- Stripe account (test mode)

### Setup

```bash
# Install dependencies
pnpm install

# Copy environment variables
cp .env.example .env.local
# Fill in your API keys in .env.local

# Run database migration
# Apply supabase/migrations/001_initial_schema.sql to your Supabase project

# Start development server
pnpm dev
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `SUPABASE_URL` | Supabase project URL |
| `SUPABASE_ANON_KEY` | Supabase anonymous key |
| `SUPABASE_SERVICE_ROLE_KEY` | Supabase service role key (server-side only) |
| `GOOGLE_GENERATIVE_AI_API_KEY` | Google AI API key for Gemini |
| `STRIPE_SECRET_KEY` | Stripe secret key |
| `STRIPE_WEBHOOK_SECRET` | Stripe webhook signing secret |
| `NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY` | Stripe publishable key |
| `STRIPE_PRICE_ID_PREMIUM` | Stripe Price ID for premium plan |
| `NEXT_PUBLIC_APP_URL` | App URL (e.g., `http://localhost:3000`) |

## Development

```bash
# Run tests
pnpm test

# Run tests in watch mode
pnpm test:watch

# Run tests with coverage
pnpm test:coverage

# Build for production
pnpm build

# Start production server
pnpm start

# Lint
pnpm lint
```

## Project Structure

```
app/
├── (public)/           # Public pages (landing, login, pricing)
├── (dashboard)/        # Protected dashboard pages
│   └── dashboard/
│       ├── onboarding/ # 4-step onboarding wizard
│       ├── findings/   # Compliance findings list
│       ├── ai-act/     # AI Act classification (premium)
│       ├── export/     # PDF report export (premium)
│       └── settings/   # Profile & billing settings
├── api/
│   ├── assess/         # GDPR assessment endpoint
│   ├── classify/       # AI Act classification endpoint
│   ├── export/         # PDF generation endpoint
│   └── webhooks/stripe # Stripe webhook handler
└── auth/callback/      # OAuth callback

lib/
├── ai/                 # AI assessment & classification
├── auth/               # Auth server actions
├── pdf/                # PDF report template
├── schemas/            # Zod validation schemas
├── stripe/             # Stripe utilities
├── subscription/       # Premium gating
├── supabase/           # Supabase client helpers & queries
└── types/              # TypeScript type definitions

components/
├── ai-act/             # AI Act risk tier components
├── dashboard/          # Dashboard UI components
├── findings/           # Finding cards & filters
├── landing/            # Landing page sections
├── onboarding/         # Onboarding wizard steps
├── premium/            # Upgrade prompt
└── ui/                 # shadcn/ui base components
```

## Testing

The project follows test-driven development (TDD). Tests are written with Vitest and React Testing Library.

```bash
pnpm test           # 38 test suites, 197 tests
```

## Legal Disclaimer

Kindlast provides AI-generated compliance guidance for educational and planning purposes. It is not a substitute for professional legal advice. For binding compliance determinations, consult a qualified data protection attorney or certified DPO.

## License

Private — All rights reserved.
