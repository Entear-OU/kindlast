import Link from 'next/link'
import { GitHubMark } from '@/components/icons/github-mark'
import { GITHUB_LICENSE_URL, GITHUB_REPO_URL, LICENSE_SPDX } from '@/lib/links'

function KindlastIcon({ size = 32 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 56 56" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect width="56" height="56" rx="11" fill="#162537" />
      <rect x="12" y="8" width="9" height="40" rx="2" fill="white" />
      <line x1="21" y1="28" x2="44" y2="9" stroke="white" strokeWidth="9" strokeLinecap="round" />
      <line x1="21" y1="28" x2="44" y2="47" stroke="white" strokeWidth="9" strokeLinecap="round" />
      <circle cx="21" cy="28" r="5.5" fill="#00C9A7" />
    </svg>
  )
}

export function Footer() {
  return (
    <footer style={{ backgroundColor: '#0D1B2A' }}>
      <div className="mx-auto max-w-5xl px-6 py-16 lg:px-8">

        {/* Top */}
        <div className="flex flex-col gap-10 sm:flex-row sm:justify-between">

          {/* Brand */}
          <div className="max-w-[260px]">
            <div className="flex items-center gap-2.5 mb-4">
              <KindlastIcon size={32} />
              <span className="text-[18px] font-extrabold tracking-[-0.03em] text-white">
                kindlast
              </span>
            </div>
            <p className="text-[1rem] font-medium leading-[1.65] tracking-[-0.005em] text-white/38">
              AI-powered GDPR &amp; EU AI Act compliance for European SMEs.
            </p>
          </div>

          {/* Links */}
          <div className="flex flex-wrap gap-x-16 gap-y-10">
            <div>
              <p className="mb-4 text-[12px] font-bold uppercase tracking-[0.18em] text-white/22">
                Product
              </p>
              <nav className="flex flex-col gap-3.5">
                <Link href="#features" className="text-[15px] font-medium tracking-[-0.01em] text-white/42 hover:text-white transition-colors duration-150">
                  Features
                </Link>
                <Link href="#how-it-works" className="text-[15px] font-medium tracking-[-0.01em] text-white/42 hover:text-white transition-colors duration-150">
                  How it works
                </Link>
              </nav>
            </div>

            <div>
              <p className="mb-4 text-[12px] font-bold uppercase tracking-[0.18em] text-white/22">
                Early access
              </p>
              <nav className="flex flex-col gap-3.5">
                <Link href="#waitlist" className="text-[15px] font-medium tracking-[-0.01em] text-white/42 hover:text-white transition-colors duration-150">
                  Join waitlist
                </Link>
                <Link href="/login" className="text-[15px] font-medium tracking-[-0.01em] text-white/42 hover:text-white transition-colors duration-150">
                  Sign in
                </Link>
              </nav>
            </div>

            <div>
              <p className="mb-4 text-[12px] font-bold uppercase tracking-[0.18em] text-white/22">
                Open source
              </p>
              <nav className="flex flex-col gap-3.5">
                <a
                  href={GITHUB_REPO_URL}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-2 text-[15px] font-medium tracking-[-0.01em] text-white/42 hover:text-white transition-colors duration-150"
                >
                  <GitHubMark size={15} />
                  GitHub
                </a>
                <a
                  href={GITHUB_LICENSE_URL}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-[15px] font-medium tracking-[-0.01em] text-white/42 hover:text-white transition-colors duration-150"
                >
                  Licence
                </a>
              </nav>
            </div>
          </div>

        </div>

        {/* Bottom */}
        <div className="mt-14 flex flex-col gap-3 pt-8 sm:flex-row sm:items-center sm:justify-between" style={{ borderTop: '1px solid rgba(255,255,255,0.07)' }}>
          <p className="text-[14px] font-medium tracking-[-0.005em] text-white/22">
            &copy; {new Date().getFullYear()} Entear O&Uuml;. Free software under{' '}
            <a
              href={GITHUB_LICENSE_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="text-white/38 underline underline-offset-2 hover:text-white transition-colors duration-150"
            >
              {LICENSE_SPDX}
            </a>
            .
          </p>
          <p className="text-[13px] font-medium text-white/18 max-w-xs leading-[1.6]">
            AI-generated compliance guidance for planning purposes only. Not legal advice.
          </p>
        </div>

      </div>
    </footer>
  )
}
