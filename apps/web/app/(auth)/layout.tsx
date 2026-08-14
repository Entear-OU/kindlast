import Link from 'next/link'

/**
 * The auth shell.
 *
 * Its own route group rather than living under `(public)`, because the
 * marketing header does not belong here. A sign-in screen has one job, and
 * offering "How it works", "Features" and "Why" at the moment someone is
 * trying to get into their account is three ways to leave.
 *
 * It also keeps the document valid: `(public)` already provides a `<main>`, so
 * a page rendering its own inside it produced two, which is exactly the kind
 * of thing that is invisible until a screen reader hits it.
 *
 * The wordmark stays as the one way out, because a screen with no exit is its
 * own trap.
 */
export default function AuthLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <div className="flex min-h-[100dvh] flex-col bg-background">
      <header className="px-6 py-6 sm:px-10">
        <Link
          href="/"
          className="rounded-sm font-semibold tracking-tight text-foreground outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          kindlast
        </Link>
      </header>

      <main className="relative flex flex-1 items-center justify-center overflow-hidden px-4 pb-24">
        {children}
      </main>
    </div>
  )
}
