import Link from 'next/link'

export default function PublicLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <div className="flex min-h-[100dvh] flex-col bg-[#FAFAF8]">

      <header className="sticky top-0 z-50 border-b border-black/[0.05] bg-[#FAFAF8]/92 backdrop-blur-2xl">
        <div className="mx-auto flex h-[66px] max-w-5xl items-center justify-between px-6 lg:px-8">

          {/* Logo */}
          <Link href="/" className="flex items-center gap-2.5 group">
            <span className="flex h-[30px] w-[30px] items-center justify-center rounded-[8px] bg-primary transition-opacity duration-150 group-hover:opacity-85">
              <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
                <path
                  d="M2 11 L7 3 L12 11"
                  stroke="white"
                  strokeWidth="2.2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            </span>
            <span className="text-[16px] font-extrabold tracking-[-0.03em] text-foreground">
              Kindlast
            </span>
          </Link>

          {/* Nav */}
          <nav className="hidden md:flex items-center gap-7">
            <Link
              href="#features"
              className="text-[14px] font-medium tracking-[-0.01em] text-foreground/42 hover:text-foreground transition-colors duration-150"
            >
              Features
            </Link>
            <Link
              href="#how-it-works"
              className="text-[14px] font-medium tracking-[-0.01em] text-foreground/42 hover:text-foreground transition-colors duration-150"
            >
              How it works
            </Link>
          </nav>

          {/* CTA */}
          <Link
            href="#waitlist"
            className="rounded-full bg-primary px-5 py-2 text-[13.5px] font-semibold tracking-[-0.01em] text-white shadow-[0_2px_12px_-3px_rgba(49,181,77,0.5)] transition-all duration-150 hover:bg-primary/90 active:scale-[0.97]"
          >
            Join waitlist
          </Link>

        </div>
      </header>

      <main className="flex-1">{children}</main>
    </div>
  )
}
