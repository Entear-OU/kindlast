import Link from 'next/link'

export function Footer() {
  return (
    <footer className="border-t bg-background">
      <div className="mx-auto max-w-7xl px-6 py-12 lg:px-8">
        <div className="flex flex-col items-center justify-between gap-6 sm:flex-row">
          <div className="flex items-center gap-2">
            <span className="text-lg font-bold text-foreground">Kindlast</span>
          </div>
          <nav className="flex gap-6 text-sm text-muted-foreground">
            <Link href="/pricing" className="hover:text-foreground transition-colors">
              Pricing
            </Link>
            <Link href="/login" className="hover:text-foreground transition-colors">
              Login
            </Link>
          </nav>
        </div>
        <div className="mt-8 border-t pt-8 text-center text-sm text-muted-foreground">
          <p>&copy; {new Date().getFullYear()} Kindlast. All rights reserved.</p>
          <p className="mt-2 text-xs">
            Kindlast provides AI-generated compliance guidance for educational
            and planning purposes. It is not a substitute for professional legal
            advice.
          </p>
        </div>
      </div>
    </footer>
  )
}
