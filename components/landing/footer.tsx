import Link from 'next/link'

export function Footer() {
  return (
    <footer className="bg-foreground">
      <div className="mx-auto max-w-6xl px-6 py-16 lg:px-8">

        {/* Top */}
        <div className="flex flex-col gap-10 sm:flex-row sm:justify-between">
          {/* Brand */}
          <div className="max-w-[260px]">
            <div className="flex items-center gap-2.5 mb-4">
              <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary">
                <svg width="15" height="15" viewBox="0 0 14 14" fill="none">
                  <path d="M2 11 L7 3 L12 11" stroke="white" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"/>
                </svg>
              </span>
              <span className="text-[17px] font-extrabold tracking-[-0.03em] text-white">
                Kindlast
              </span>
            </div>
            <p className="text-[1rem] font-medium leading-[1.65] tracking-[-0.005em] text-white/40">
              AI-powered GDPR &amp; EU AI Act compliance for European SMEs.
            </p>
          </div>

          {/* Links */}
          <div className="flex gap-16">
            <div>
              <p className="mb-4 text-[12px] font-bold uppercase tracking-[0.16em] text-white/25">
                Product
              </p>
              <nav className="flex flex-col gap-3.5">
                <Link href="#features" className="text-[15px] font-medium tracking-[-0.01em] text-white/45 hover:text-white transition-colors duration-150">
                  Features
                </Link>
                <Link href="/pricing" className="text-[15px] font-medium tracking-[-0.01em] text-white/45 hover:text-white transition-colors duration-150">
                  Pricing
                </Link>
              </nav>
            </div>

            <div>
              <p className="mb-4 text-[12px] font-bold uppercase tracking-[0.16em] text-white/25">
                Account
              </p>
              <nav className="flex flex-col gap-3.5">
                <Link href="/login" className="text-[15px] font-medium tracking-[-0.01em] text-white/45 hover:text-white transition-colors duration-150">
                  Sign in
                </Link>
                <Link href="/login" className="text-[15px] font-medium tracking-[-0.01em] text-white/45 hover:text-white transition-colors duration-150">
                  Get started free
                </Link>
              </nav>
            </div>
          </div>
        </div>

        {/* Bottom */}
        <div className="mt-14 flex flex-col gap-3 border-t border-white/[0.07] pt-8 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-[14px] font-medium tracking-[-0.005em] text-white/25">
            &copy; {new Date().getFullYear()} Kindlast. All rights reserved.
          </p>
          <p className="text-[13px] font-medium text-white/20 max-w-sm leading-[1.6]">
            AI-generated compliance guidance for planning purposes only. Not legal advice.
          </p>
        </div>
      </div>
    </footer>
  )
}
