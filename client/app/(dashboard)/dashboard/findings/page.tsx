import { redirect } from 'next/navigation'
import { cookies } from 'next/headers'
import { getApiConfig, buildApiUrl, API_ENDPOINTS } from '@/lib/api/config'
import { FindingsPageClient } from './findings-page-client'
import { LegalDisclaimer } from '@/components/dashboard/legal-disclaimer'
import type { Finding } from '@/lib/types/database'

interface FindingsResponse {
  findings: Finding[]
  total: number
  page: number
  page_size: number
  total_pages: number
}

export default async function FindingsPage() {
  const config = getApiConfig()
  const cookieStore = await cookies()
  const accessToken = cookieStore.get(config.accessTokenCookie)?.value

  if (!accessToken) {
    redirect('/login')
  }

  let findings: Finding[] = []

  try {
    const url = buildApiUrl(API_ENDPOINTS.findings.list, config)
    const response = await fetch(url, {
      headers: { 'Authorization': `Bearer ${accessToken}` },
      cache: 'no-store',
    })

    if (response.ok) {
      const data: FindingsResponse = await response.json()
      findings = data.findings || []
    }
  } catch {
    // Failed to fetch findings, show empty list
  }

  return (
    <div className="flex flex-col gap-6 p-6">
      <div>
        <h1 className="text-2xl font-bold">Findings</h1>
        <p className="text-sm text-muted-foreground">
          Review and manage your compliance findings.
        </p>
      </div>

      <FindingsPageClient findings={findings} />

      <LegalDisclaimer />
    </div>
  )
}
