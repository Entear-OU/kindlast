import Link from 'next/link'

export function Hero() {
  return (
    <section className="relative overflow-hidden bg-background py-24 sm:py-32">
      <div className="mx-auto max-w-4xl px-6 text-center lg:px-8">
        <h1 className="text-4xl font-bold tracking-tight text-foreground sm:text-6xl">
          Two regulations, one platform, zero guesswork
        </h1>
        <p className="mx-auto mt-6 max-w-2xl text-lg leading-8 text-muted-foreground">
          AI-powered GDPR &amp; AI Act compliance for EU SMEs. Answer a few
          questions, get an instant gap analysis, and know exactly what to fix.
        </p>
        <div className="mt-10 flex items-center justify-center gap-x-6">
          <Link
            href="/login"
            className="rounded-md bg-primary px-6 py-3 text-sm font-semibold text-primary-foreground shadow-sm hover:bg-primary/90 transition-colors"
          >
            Get Started Free
          </Link>
          <Link
            href="#how-it-works"
            className="text-sm font-semibold leading-6 text-foreground hover:text-muted-foreground transition-colors"
          >
            Learn more <span aria-hidden="true">&rarr;</span>
          </Link>
        </div>
      </div>
    </section>
  )
}
