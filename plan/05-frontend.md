# PRD 05 — Frontend

**Agent**: Frontend agent  
**DEPENDS ON**: `04-api-gateway.md` (API contract finalised)  
**Produces**: Next.js 15 frontend with auth, streaming Q&A, citation rendering, upgrade flow  

---

## Overview

The Kindlast frontend is a Next.js 15 App Router application. Key UX principles: answers feel instant (streaming), citations feel trustworthy (inline superscripts linking to sources), and the upgrade prompt feels natural (surfaced after the 3rd citation, not as a blocker).

---

## Project structure

```
frontend/
├── app/
│   ├── layout.tsx              # root layout, fonts, providers
│   ├── page.tsx                # landing / marketing page
│   ├── (auth)/
│   │   ├── login/page.tsx
│   │   └── register/page.tsx
│   ├── (app)/
│   │   ├── layout.tsx          # app shell with sidebar
│   │   ├── query/page.tsx      # main Q&A interface
│   │   ├── history/page.tsx    # past queries
│   │   └── settings/page.tsx   # account, plan, API key
│   └── api/
│       ├── auth/[...route]/route.ts   # auth proxy
│       └── query/route.ts             # query proxy (SSE stream)
├── components/
│   ├── ui/                     # shadcn/ui base components
│   ├── query/
│   │   ├── QueryInput.tsx      # search input + topic filter
│   │   ├── AnswerStream.tsx    # streaming answer with inline citations
│   │   ├── CitationList.tsx    # numbered citation cards below answer
│   │   ├── FreemiumGate.tsx    # upgrade prompt after 3 free citations
│   │   └── FeedbackButtons.tsx # thumbs up/down
│   ├── auth/
│   │   ├── LoginForm.tsx
│   │   └── RegisterForm.tsx
│   └── layout/
│       ├── Sidebar.tsx
│       └── TopBar.tsx
├── lib/
│   ├── api.ts                  # typed API client
│   ├── auth.ts                 # JWT storage + refresh
│   ├── stream.ts               # SSE parsing utility
│   └── types.ts                # shared TypeScript types
├── hooks/
│   ├── useQuery.ts             # main query hook with streaming
│   ├── useAuth.ts              # auth state
│   └── usePlan.ts              # current plan + limits
├── next.config.js              # output: standalone
└── tailwind.config.js
```

---

## Task 1 — Shared types

Create `frontend/lib/types.ts`:

```typescript
export type Plan = 'free' | 'premium' | 'api'

export interface User {
  id: string
  email: string
  plan: Plan
}

export interface Citation {
  index: number
  sourceUrl: string
  title: string
  section: string
  chunkText: string
}

export interface QueryResponse {
  answer: string
  citations: Citation[]
  cacheHit: boolean
  provider: string
  warning?: string
}

export interface StreamEvent {
  type: 'chunk' | 'done' | 'error'
  text?: string           // present when type === 'chunk'
  citations?: Citation[]  // present when type === 'done'
  provider?: string
  cacheHit?: boolean
  warning?: string
  message?: string        // present when type === 'error'
}

export type TopicFilter = 'gdpr' | 'ai_act' | 'both'

export interface QueryState {
  status: 'idle' | 'loading' | 'streaming' | 'done' | 'error'
  answer: string
  citations: Citation[]
  warning?: string
  provider?: string
  cacheHit?: boolean
  error?: string
}
```

---

## Task 2 — API client

Create `frontend/lib/api.ts`:

```typescript
const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? 'https://api.kindlast.com'

function getToken(): string | null {
  if (typeof window === 'undefined') return null
  return localStorage.getItem('kindlast_token')
}

export function setToken(token: string) {
  localStorage.setItem('kindlast_token', token)
}

export function clearToken() {
  localStorage.removeItem('kindlast_token')
}

async function request<T>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const token = getToken()
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...options.headers,
  }
  const res = await fetch(`${API_BASE}${path}`, { ...options, headers })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `HTTP ${res.status}`)
  }
  return res.json()
}

export const api = {
  auth: {
    register: (email: string, password: string) =>
      request<{ token: string; plan: Plan }>('/auth/register', {
        method: 'POST',
        body: JSON.stringify({ email, password }),
      }),
    login: (email: string, password: string) =>
      request<{ token: string; plan: Plan }>('/auth/login', {
        method: 'POST',
        body: JSON.stringify({ email, password }),
      }),
    me: () => request<User>('/v1/user'),
  },
  billing: {
    checkout: () =>
      request<{ url: string }>('/billing/checkout', { method: 'POST' }),
  },
  feedback: {
    submit: (queryHash: string, rating: 1 | -1, comment?: string) =>
      request('/v1/feedback', {
        method: 'POST',
        body: JSON.stringify({ query_hash: queryHash, rating, comment }),
      }),
  },
}
```

---

## Task 3 — SSE stream parser

Create `frontend/lib/stream.ts`:

```typescript
import type { StreamEvent } from './types'

export async function* parseSSEStream(
  response: Response
): AsyncGenerator<StreamEvent> {
  const reader = response.body!.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() ?? ''  // keep incomplete line in buffer

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          const jsonStr = line.slice(6).trim()
          if (!jsonStr) continue
          try {
            const event = JSON.parse(jsonStr) as StreamEvent
            yield event
          } catch {
            // malformed event — skip
          }
        }
      }
    }
  } finally {
    reader.cancel()
  }
}
```

---

## Task 4 — Query hook

Create `frontend/hooks/useQuery.ts`:

```typescript
'use client'
import { useState, useCallback, useRef } from 'react'
import { parseSSEStream } from '@/lib/stream'
import type { QueryState, TopicFilter } from '@/lib/types'

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? 'https://api.kindlast.com'

export function useQuery() {
  const [state, setState] = useState<QueryState>({
    status: 'idle',
    answer: '',
    citations: [],
  })
  const abortRef = useRef<AbortController | null>(null)

  const submit = useCallback(async (query: string, topic: TopicFilter) => {
    // cancel any in-flight request
    abortRef.current?.abort()
    abortRef.current = new AbortController()

    setState({ status: 'loading', answer: '', citations: [] })

    const token = localStorage.getItem('kindlast_token')
    
    try {
      const response = await fetch(`${API_BASE}/v1/query`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({
          query,
          topic_filter: topic === 'both' ? ['gdpr', 'ai_act'] : [topic],
          max_citations: 10,
        }),
        signal: abortRef.current.signal,
      })

      if (!response.ok) {
        const err = await response.json().catch(() => ({}))
        setState(s => ({ ...s, status: 'error', error: err.error ?? 'Request failed' }))
        return
      }

      setState(s => ({ ...s, status: 'streaming' }))

      for await (const event of parseSSEStream(response)) {
        if (event.type === 'chunk' && event.text) {
          setState(s => ({ ...s, answer: s.answer + event.text }))
        } else if (event.type === 'done') {
          setState(s => ({
            ...s,
            status: 'done',
            citations: event.citations ?? [],
            provider: event.provider,
            cacheHit: event.cacheHit,
            warning: event.warning,
          }))
        } else if (event.type === 'error') {
          setState(s => ({
            ...s,
            status: 'error',
            error: event.message ?? 'Generation error',
          }))
        }
      }
    } catch (err: any) {
      if (err.name === 'AbortError') return
      setState(s => ({ ...s, status: 'error', error: err.message }))
    }
  }, [])

  const reset = useCallback(() => {
    abortRef.current?.abort()
    setState({ status: 'idle', answer: '', citations: [] })
  }, [])

  return { state, submit, reset }
}
```

---

## Task 5 — Answer stream component

Create `frontend/components/query/AnswerStream.tsx`:

```tsx
'use client'
import { useMemo } from 'react'
import type { Citation } from '@/lib/types'

interface Props {
  answer: string
  citations: Citation[]
  streaming: boolean
}

export function AnswerStream({ answer, citations, streaming }: Props) {
  // replace [1], [2] markers with clickable superscript links
  const rendered = useMemo(() => {
    return answer.replace(/\[(\d+)\]/g, (match, num) => {
      const idx = parseInt(num) - 1
      const citation = citations[idx]
      if (!citation) return match
      return `<sup><a href="#citation-${num}" class="citation-ref" title="${citation.title}">[${num}]</a></sup>`
    })
  }, [answer, citations])

  return (
    <div className="answer-container">
      <div
        className="prose prose-sm max-w-none"
        dangerouslySetInnerHTML={{ __html: rendered }}
      />
      {streaming && (
        <span className="inline-block w-0.5 h-4 bg-current animate-pulse ml-0.5" />
      )}
    </div>
  )
}
```

Create `frontend/components/query/CitationList.tsx`:

```tsx
'use client'
import type { Citation } from '@/lib/types'

interface Props {
  citations: Citation[]
  planLimit: number   // 3 for free, 10 for premium
  onUpgrade: () => void
}

export function CitationList({ citations, planLimit, onUpgrade }: Props) {
  const visible = citations.slice(0, planLimit)
  const hidden = citations.length - visible.length

  return (
    <div className="citations-container mt-6 space-y-3">
      <h3 className="text-sm font-medium text-muted-foreground">
        Sources ({citations.length})
      </h3>

      {visible.map((c) => (
        <div
          key={c.index}
          id={`citation-${c.index}`}
          className="citation-card rounded-lg border p-3 text-sm"
        >
          <div className="flex items-start gap-2">
            <span className="citation-number flex-shrink-0 text-xs font-medium
                             rounded-full bg-primary/10 text-primary w-5 h-5
                             flex items-center justify-center">
              {c.index}
            </span>
            <div className="flex-1 min-w-0">
              <div className="font-medium text-sm truncate">
                {c.title || new URL(c.sourceUrl).hostname}
              </div>
              {c.section && (
                <div className="text-xs text-muted-foreground mt-0.5">
                  {c.section}
                </div>
              )}
              <a
                href={c.sourceUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs text-blue-600 hover:underline mt-1 inline-block truncate max-w-full"
              >
                {c.sourceUrl}
              </a>
              <p className="text-xs text-muted-foreground mt-1 line-clamp-2">
                {c.chunkText}
              </p>
            </div>
          </div>
        </div>
      ))}

      {hidden > 0 && (
        <FreemiumGate hiddenCount={hidden} onUpgrade={onUpgrade} />
      )}
    </div>
  )
}
```

Create `frontend/components/query/FreemiumGate.tsx`:

```tsx
'use client'

interface Props {
  hiddenCount: number
  onUpgrade: () => void
}

export function FreemiumGate({ hiddenCount, onUpgrade }: Props) {
  return (
    <div className="freemium-gate rounded-lg border border-dashed p-4 text-center">
      <p className="text-sm text-muted-foreground">
        {hiddenCount} more source{hiddenCount > 1 ? 's' : ''} available on Premium
      </p>
      <p className="text-xs text-muted-foreground mt-1">
        Full citations, EU AI Act coverage, and document generation — €49/month
      </p>
      <button
        onClick={onUpgrade}
        className="mt-3 px-4 py-1.5 text-sm font-medium rounded-md
                   bg-primary text-primary-foreground hover:bg-primary/90"
      >
        Upgrade to Premium
      </button>
    </div>
  )
}
```

---

## Task 6 — Main query page

Create `frontend/app/(app)/query/page.tsx`:

```tsx
'use client'
import { useState } from 'react'
import { useQuery } from '@/hooks/useQuery'
import { usePlan } from '@/hooks/usePlan'
import { QueryInput } from '@/components/query/QueryInput'
import { AnswerStream } from '@/components/query/AnswerStream'
import { CitationList } from '@/components/query/CitationList'
import { FeedbackButtons } from '@/components/query/FeedbackButtons'
import { api } from '@/lib/api'
import type { TopicFilter } from '@/lib/types'

export default function QueryPage() {
  const { state, submit, reset } = useQuery()
  const { plan, citationLimit } = usePlan()
  const [topic, setTopic] = useState<TopicFilter>('gdpr')

  const handleUpgrade = async () => {
    const { url } = await api.billing.checkout()
    window.location.href = url
  }

  const isStreaming = state.status === 'streaming'
  const isDone = state.status === 'done'

  return (
    <div className="max-w-3xl mx-auto px-4 py-8">
      <QueryInput
        onSubmit={(q) => submit(q, topic)}
        onReset={reset}
        topic={topic}
        onTopicChange={setTopic}
        loading={state.status === 'loading' || isStreaming}
      />

      {state.status === 'error' && (
        <div className="mt-4 rounded-lg bg-destructive/10 border border-destructive/20 p-3 text-sm text-destructive">
          {state.error}
        </div>
      )}

      {state.warning === 'low_confidence' && (
        <div className="mt-4 rounded-lg bg-amber-50 border border-amber-200 p-3 text-sm text-amber-800">
          Limited source material found. Consider consulting a qualified DPO for your specific situation.
        </div>
      )}

      {(isStreaming || isDone) && state.answer && (
        <div className="mt-6">
          <AnswerStream
            answer={state.answer}
            citations={state.citations}
            streaming={isStreaming}
          />

          {state.cacheHit && (
            <p className="text-xs text-muted-foreground mt-2">
              Cached result · {state.provider}
            </p>
          )}

          {isDone && state.citations.length > 0 && (
            <>
              <CitationList
                citations={state.citations}
                planLimit={citationLimit}
                onUpgrade={handleUpgrade}
              />
              <FeedbackButtons
                queryHash={btoa(state.answer.slice(0, 50))}
              />
            </>
          )}
        </div>
      )}
    </div>
  )
}
```

---

## Task 7 — Auth pages

Create `frontend/app/(auth)/login/page.tsx` and `register/page.tsx`:

```tsx
// login/page.tsx
'use client'
import { useState } from 'react'
import { useRouter } from 'next/navigation'
import { api, setToken } from '@/lib/api'

export default function LoginPage() {
  const router = useRouter()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      const { token } = await api.auth.login(email, password)
      setToken(token)
      router.push('/query')
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="w-full max-w-sm space-y-6">
        <div className="text-center">
          <h1 className="text-2xl font-semibold">Sign in to Kindlast</h1>
          <p className="text-sm text-muted-foreground mt-1">
            GDPR and EU AI Act compliance intelligence
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1">Email</label>
            <input
              type="email"
              value={email}
              onChange={e => setEmail(e.target.value)}
              className="w-full"
              required
              autoComplete="email"
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">Password</label>
            <input
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              className="w-full"
              required
              autoComplete="current-password"
            />
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <button
            type="submit"
            disabled={loading}
            className="w-full py-2 px-4 rounded-md bg-primary text-primary-foreground
                       font-medium disabled:opacity-50"
          >
            {loading ? 'Signing in...' : 'Sign in'}
          </button>
        </form>

        <p className="text-center text-sm text-muted-foreground">
          No account?{' '}
          <a href="/register" className="text-primary hover:underline">
            Create one free
          </a>
        </p>
      </div>
    </div>
  )
}
```

Create identical structure for `register/page.tsx` calling `api.auth.register`.

---

## Task 8 — QueryInput component

Create `frontend/components/query/QueryInput.tsx`:

```tsx
'use client'
import { useState, useRef, useEffect } from 'react'
import type { TopicFilter } from '@/lib/types'

const TOPIC_LABELS: Record<TopicFilter, string> = {
  gdpr: 'GDPR',
  ai_act: 'EU AI Act',
  both: 'Both',
}

interface Props {
  onSubmit: (query: string) => void
  onReset: () => void
  topic: TopicFilter
  onTopicChange: (t: TopicFilter) => void
  loading: boolean
}

// Example questions for empty state
const EXAMPLES = [
  'What are the lawful bases for processing personal data under GDPR Article 6?',
  'Do I need a Data Protection Officer for my 30-person SaaS company?',
  'What does the EU AI Act require for high-risk AI systems?',
  'What is the right to erasure and when does it apply?',
]

export function QueryInput({ onSubmit, onReset, topic, onTopicChange, loading }: Props) {
  const [query, setQuery] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // auto-resize textarea
  useEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }, [query])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!query.trim() || loading) return
    onSubmit(query.trim())
  }

  const handleExample = (example: string) => {
    setQuery(example)
    onSubmit(example)
  }

  return (
    <div className="space-y-3">
      <form onSubmit={handleSubmit}>
        <div className="relative rounded-xl border bg-background focus-within:ring-1 focus-within:ring-primary">
          <textarea
            ref={textareaRef}
            value={query}
            onChange={e => setQuery(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                handleSubmit(e)
              }
            }}
            placeholder="Ask a GDPR or EU AI Act compliance question..."
            className="w-full resize-none rounded-xl bg-transparent px-4 pt-4 pb-12
                       text-sm focus:outline-none min-h-[56px] max-h-[200px]"
            rows={1}
          />
          <div className="absolute bottom-2 left-3 right-3 flex items-center justify-between">
            <div className="flex gap-1">
              {(Object.keys(TOPIC_LABELS) as TopicFilter[]).map(t => (
                <button
                  key={t}
                  type="button"
                  onClick={() => onTopicChange(t)}
                  className={`text-xs px-2 py-0.5 rounded-full border transition-colors
                    ${topic === t
                      ? 'bg-primary text-primary-foreground border-primary'
                      : 'border-input text-muted-foreground hover:border-primary'
                    }`}
                >
                  {TOPIC_LABELS[t]}
                </button>
              ))}
            </div>
            <button
              type="submit"
              disabled={!query.trim() || loading}
              className="text-xs px-3 py-1.5 rounded-lg bg-primary text-primary-foreground
                         font-medium disabled:opacity-40"
            >
              {loading ? 'Asking...' : 'Ask →'}
            </button>
          </div>
        </div>
      </form>

      {!query && !loading && (
        <div className="grid grid-cols-2 gap-2">
          {EXAMPLES.map((ex) => (
            <button
              key={ex}
              onClick={() => handleExample(ex)}
              className="text-left text-xs p-3 rounded-lg border
                         text-muted-foreground hover:border-primary
                         hover:text-foreground transition-colors line-clamp-2"
            >
              {ex}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
```

---

## Task 9 — next.config.js and environment

Create `frontend/next.config.js`:

```js
/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',    // required for Docker slim builds
  experimental: {
    serverComponentsExternalPackages: [],
  },
}
module.exports = nextConfig
```

Create `frontend/.env.example`:
```
NEXT_PUBLIC_API_URL=http://localhost:8080
```

---

## Global acceptance criteria

- [ ] `npm run build` in `frontend/` completes without TypeScript errors
- [ ] Login → query → answer streams in chunks (not all at once)
- [ ] Citation `[1]` in answer text is a clickable superscript linking to `#citation-1`
- [ ] Free user sees exactly 3 citation cards + FreemiumGate for hidden ones
- [ ] Premium user sees all citation cards, no FreemiumGate
- [ ] Clicking upgrade redirects to Stripe Checkout
- [ ] Successful Stripe return shows updated plan in UI
- [ ] Low confidence warning shown when API returns `warning: "low_confidence"`
- [ ] Error state shown when API returns non-200 or SSE `error` event
- [ ] Mobile responsive: query input and citations render correctly at 375px width
- [ ] `docker build -f infrastructure/docker/frontend.Dockerfile .` succeeds
