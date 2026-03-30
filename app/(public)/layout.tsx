import Link from 'next/link'

function KindlastIcon({ size = 32 }: { size?: number }) {
  const scale = size / 56
  return (
    <svg width={size} height={size} viewBox="0 0 56 56" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect width="56" height="56" rx="11" fill="#0D1B2A" />
      <rect x="12" y="8" width="9" height="40" rx="2" fill="white" />
      <line x1="21" y1="28" x2="44" y2="9" stroke="white" strokeWidth="9" strokeLinecap="round" />
      <line x1="21" y1="28" x2="44" y2="47" stroke="white" strokeWidth="9" strokeLinecap="round" />
      <circle cx="21" cy="28" r="5.5" fill="#00C9A7" />
    </svg>
  )
}

export default function PublicLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <div className="flex min-h-[100dvh] flex-col bg-[#F5F4F0]">

      <header className="sticky top-0 z-50 border-b border-black/[0.05] bg-[#F5F4F0]/92 backdrop-blur-2xl">
        <div className="mx-auto flex h-[70px] max-w-5xl items-center justify-between px-6 lg:px-8">

          {/* Logo */}
          <Link href="/" className="flex items-center gap-2.5 group">
            <KindlastIcon size={32} />
            <span className="text-[18px] font-extrabold tracking-[-0.03em] text-[#0D1B2A]">
              kindlast
            </span>
          </Link>

          {/* Nav */}
          <nav className="hidden md:flex items-center gap-8">
            <Link
              href="#features"
              className="text-[15px] font-medium tracking-[-0.01em] text-[#0D1B2A]/45 hover:text-[#0D1B2A] transition-colors duration-150"
            >
              Features
            </Link>
            <Link
              href="#how-it-works"
              className="text-[15px] font-medium tracking-[-0.01em] text-[#0D1B2A]/45 hover:text-[#0D1B2A] transition-colors duration-150"
            >
              How it works
            </Link>
          </nav>

          {/* CTA */}
          <Link
            href="#waitlist"
            className="rounded-full bg-[#0D1B2A] px-6 py-2.5 text-[15px] font-semibold tracking-[-0.01em] text-white transition-all duration-150 hover:bg-[#162537] active:scale-[0.97]"
          >
            Join waitlist
          </Link>

        </div>
      </header>

      <main className="flex-1">{children}</main>
    </div>
  )
}
