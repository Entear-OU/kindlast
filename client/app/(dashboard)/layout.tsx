import { redirect } from 'next/navigation'
import { cookies } from 'next/headers'
import { getApiConfig, buildApiUrl, API_ENDPOINTS } from '@/lib/api/config'
import { SidebarNav } from '@/components/dashboard/sidebar-nav'
import { MobileNav } from '@/components/dashboard/mobile-nav'

async function getAuthUser() {
  const config = getApiConfig()
  const cookieStore = await cookies()
  const accessToken = cookieStore.get(config.accessTokenCookie)?.value

  if (!accessToken) {
    return null
  }

  try {
    const url = buildApiUrl(API_ENDPOINTS.auth.me, config)
    const response = await fetch(url, {
      headers: { 'Authorization': `Bearer ${accessToken}` },
      cache: 'no-store',
    })

    if (!response.ok) {
      return null
    }

    return await response.json()
  } catch {
    return null
  }
}

async function getProfile(accessToken: string) {
  const config = getApiConfig()
  try {
    const url = buildApiUrl(API_ENDPOINTS.profile, config)
    const response = await fetch(url, {
      headers: { 'Authorization': `Bearer ${accessToken}` },
      cache: 'no-store',
    })

    if (!response.ok) {
      return null
    }

    return await response.json()
  } catch {
    return null
  }
}

export default async function DashboardLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const config = getApiConfig()
  const cookieStore = await cookies()
  const accessToken = cookieStore.get(config.accessTokenCookie)?.value

  const user = await getAuthUser()

  if (!user) {
    redirect('/login')
  }

  const profile = accessToken ? await getProfile(accessToken) : null

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
