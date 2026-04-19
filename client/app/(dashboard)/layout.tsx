import { redirect } from 'next/navigation'
import { createClient } from '@/lib/supabase/server'
import { getBusinessProfile, getSubscription } from '@/lib/supabase/queries'
import { SidebarNav } from '@/components/dashboard/sidebar-nav'
import { MobileNav } from '@/components/dashboard/mobile-nav'

export default async function DashboardLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()

  if (!user) {
    redirect('/login')
  }

  const [{ data: profile }] = await Promise.all([
    getBusinessProfile(supabase, user.id),
    getSubscription(supabase, user.id),
  ])

  // If no profile, render children without sidebar (for onboarding)
  if (!profile) {
    return (
      <div className="min-h-screen">
        <main className="mx-auto max-w-3xl py-8 px-4">
          {children}
        </main>
      </div>
    )
  }

  return (
    <div className="flex min-h-screen flex-col md:flex-row">
      {/* Mobile header with hamburger menu */}
      <header className="flex items-center gap-4 border-b bg-card px-4 py-3 md:hidden">
        <MobileNav />
        <h1 className="text-lg font-semibold">Kindlast</h1>
      </header>
      {/* Desktop sidebar */}
      <aside className="hidden w-64 shrink-0 border-r bg-card md:block">
        <SidebarNav />
      </aside>
      <main className="flex-1 overflow-y-auto">
        {children}
      </main>
    </div>
  )
}
