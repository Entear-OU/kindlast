import { SiteHeader } from '@/components/landing/site-header'

/**
 * The public shell.
 *
 * The header moved into its own client component in ENT-190: two routes now
 * open on a full-bleed dark plate, and the bar has to be transparent over
 * those and solid everywhere else, which needs scroll and route state.
 */
export default function PublicLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <div className="flex min-h-[100dvh] flex-col bg-[#F5F4F0]">
      <SiteHeader />
      <main className="flex-1">{children}</main>
    </div>
  )
}
