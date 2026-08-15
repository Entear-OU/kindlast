'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'

import { cn } from '@/lib/utils'

/**
 * One navigation link, marked when it is the surface you are on.
 *
 * The only client component in the console shell, and deliberately the
 * smallest one that can be: it exists because marking the active surface needs
 * `usePathname`, and nothing else in the sidebar needs the client at all.
 *
 * The first version made the whole sidebar a client component, which pulled
 * SignOutForm in with it. That form is an async server component reading a
 * cookie through `next/headers`, so the build failed with "You're importing a
 * component that needs next/headers". Worth recording rather than quietly
 * fixing: a 'use client' directive is not local to the file it appears in, it
 * is a boundary that everything imported below it has to live on the far side
 * of.
 */
export function NavLink({
  href,
  label,
  exact = false,
  children,
}: {
  href: string
  label: string
  /** Match the path exactly, for a link that is the parent of the others. */
  exact?: boolean
  children: React.ReactNode
}) {
  const pathname = usePathname()
  // Prefix matching everywhere except the organisation home, which would
  // otherwise light up on every page beneath it.
  const active = exact ? pathname === href : pathname.startsWith(href)

  return (
    <Link
      href={href}
      aria-current={active ? 'page' : undefined}
      className={cn(
        'flex items-center gap-2.5 rounded-lg px-2 py-2 text-sm transition-colors',
        active
          ? 'bg-muted font-medium text-foreground'
          : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground',
      )}
    >
      {children}
      {label}
    </Link>
  )
}
