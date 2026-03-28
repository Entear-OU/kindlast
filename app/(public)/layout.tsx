import Link from 'next/link'

export default function PublicLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <div className="flex min-h-[100dvh] flex-col bg-[#FAFAF8]">
      <header className="sticky top-0 z-50 border-b border-black/[0.06] bg-[#FAFAF8]/90 backdrop-blur-2xl">
        <div className="mx-auto flex h-[68px] max-w-6xl items-center justify-between px-6 lg:px-8">
          {/* Logo */}
          <Link href="/" className="flex items-center gap-2.5">
            <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary">
              <svg width="15" height="15" viewBox="0 0 14 14" fill="none">
                <path d="M2 11 L7 3 L12 11" stroke="white" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"/>
              </svg>
            </span>
            <span className="text-[17px] font-extrabold tracking-[-0.03em] text-foreground">
              Kindlast
            </span>
          </Link>

          {/* Nav */}
          <nav className="hidden md:flex items-center gap-8">
            <Link
              href="#features"
              className="text-[15px] font-medium tracking-[-0.01em] text-foreground/50 hover:text-foreground transition-colors duration-150"
            >
              Features
            </Link>
            <Link
              href="/pricing"
              className="text-[15px] font-medium tracking-[-0.01em] text-foreground/50 hover:text-foreground transition-colors duration-150"
            >
              Pricing
            </Link>
          </nav>

          {/* CTA group */}
          <div className="flex items-center gap-4">
            <Link
              href="/login"
              className="text-[15px] font-semibold tracking-[-0.01em] text-foreground/50 hover:text-foreground transition-colors duration-150"
            >
              Sign in
            </Link>
            <Link
              href="/login"
              className="rounded-full bg-primary px-5 py-2.5 text-[15px] font-semibold tracking-[-0.01em] text-white transition-all duration-150 hover:bg-primary/90 active:scale-[0.97]"
            >
              Get started
            </Link>
          </div>
        </div>
      </header>

      <main className="flex-1">{children}</main>
    </div>
  )
}
