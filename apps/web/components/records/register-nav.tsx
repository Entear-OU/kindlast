import Link from 'next/link'

import { orgPath } from '@/lib/auth/org'

/**
 * The three registers, as links rather than tabs held in state.
 *
 * Each register is its own URL so it can be shared, bookmarked and reached with
 * the back button, which is the same reasoning the feed's status filter follows.
 * A client-side tab would put the current register in memory, where a reload
 * loses it and a link to "the AI systems register" cannot exist.
 */
export const REGISTERS = [
  { label: 'Processing activities', href: '', key: 'ropa' },
  { label: 'AI systems', href: '/ai-systems', key: 'ai-systems' },
  { label: 'Data-subject requests', href: '/dsars', key: 'dsars' },
] as const

export type RegisterKey = (typeof REGISTERS)[number]['key']

export function RegisterNav({
  slug,
  active,
}: {
  slug: string
  active: RegisterKey
}) {
  return (
    <nav aria-label="Record types" className="mt-8 flex flex-wrap gap-2">
      {REGISTERS.map(({ label, href, key }) => {
        const current = key === active

        return (
          <Link
            key={key}
            href={orgPath(slug, `/records${href}`)}
            aria-current={current ? 'page' : undefined}
            className={`rounded-full border px-3 py-1 text-xs transition-colors ${
              current
                ? 'border-primary/40 bg-primary/10 text-primary'
                : 'border-border/60 text-muted-foreground hover:border-border hover:text-foreground'
            }`}
          >
            {label}
          </Link>
        )
      })}
    </nav>
  )
}
