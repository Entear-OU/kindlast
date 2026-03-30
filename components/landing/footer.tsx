import Link from 'next/link'

export function Footer() {
  return (
    <footer className="bg-foreground">
      <div className="mx-auto max-w-5xl px-6 py-16 lg:px-8">

        {/* Top */}
        <div className="flex flex-col gap-10 sm:flex-row sm:justify-between">

          {/* Brand */}
          <div className="max-w-[240px]">
            <div className="flex items-center gap-2.5 mb-4">
              <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary">
                <svg width="15" height="15" viewBox="0 0 14 14" fill="none">
                  <path
                    d="M2 11 L7 3 L12 11"
                    stroke="white"
                    strokeWidth="2.2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
              </span>
              <span className="text-[17px] font-extrabold tracking-[-0.03em] text-white">
                Kindlast
              </span>
            </div>
            <p className="text-[0.9375rem] font-medium leading-[1.65] tracking-[-0.005em] text-white/38">
              AI-powered GDPR &amp; EU AI Act compliance for European SMEs.
            </p>
          </div>

          {/* Links */}
          <div className="flex gap-16">
            <div>
              <p className="mb-4 text-[11px] font-bold uppercase tracking-[0.18em] text-white/22">
                Product
              </p>
              <nav className="flex flex-col gap-3.5">
                <Link
                  href="#features"
                  className="text-[14px] font-medium tracking-[-0.01em] text-white/42 hover:text-white transition-colors duration-150"
                >
                  Features
                </Link>
                <Link
                  href="#how-it-works"
                  className="text-[14px] font-medium tracking-[-0.01em] text-white/42 hover:text-white transition-colors duration-150"
                >
                  How it works
                </Link>
              </nav>
            </div>

            <div>
              <p className="mb-4 text-[11px] font-bold uppercase tracking-[0.18em] text-white/22">
                Early access
              </p>
              <nav className="flex flex-col gap-3.5">
                <Link
                  href="#waitlist"
                  className="text-[14px] font-medium tracking-[-0.01em] text-white/42 hover:text-white transition-colors duration-150"
                >
                  Join waitlist
                </Link>
                <Link
                  href="/login"
                  className="text-[14px] font-medium tracking-[-0.01em] text-white/42 hover:text-white transition-colors duration-150"
                >
                  Sign in
                </Link>
              </nav>
            </div>
          </div>

        </div>

        {/* Bottom */}
        <div className="mt-14 flex flex-col gap-3 border-t border-white/[0.07] pt-8 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-[13.5px] font-medium tracking-[-0.005em] text-white/22">
            &copy; {new Date().getFullYear()} Kindlast. All rights reserved.
          </p>
          <p className="text-[12.5px] font-medium text-white/18 max-w-xs leading-[1.6]">
            AI-generated compliance guidance for planning purposes only. Not legal advice.
          </p>
        </div>

      </div>
    </footer>
  )
}
