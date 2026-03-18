import { createClient } from '@/lib/supabase/server'
import { redirect } from 'next/navigation'
import { FindingsPageClient } from './findings-page-client'
import { LegalDisclaimer } from '@/components/dashboard/legal-disclaimer'
import type { Finding } from '@/lib/types/database'

export default async function FindingsPage() {
  const supabase = await createClient()
  const { data: { user } } = await supabase.auth.getUser()

  if (!user) {
    redirect('/login')
  }

  const { data: findings } = await supabase
    .from('findings')
    .select('*')
    .eq('user_id', user.id)
    .order('created_at', { ascending: false })

  return (
    <div className="flex flex-col gap-6 p-6">
      <div>
        <h1 className="text-2xl font-bold">Findings</h1>
        <p className="text-sm text-muted-foreground">
          Review and manage your compliance findings.
        </p>
      </div>

      <FindingsPageClient findings={(findings as Finding[]) || []} />

      <LegalDisclaimer />
    </div>
  )
}
