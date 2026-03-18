import { NextResponse } from 'next/server'
import { renderToBuffer } from '@react-pdf/renderer'
import React from 'react'
import { createClient } from '@/lib/supabase/server'
import { checkPremium } from '@/lib/subscription/gate'
import { ComplianceReport } from '@/lib/pdf/compliance-report'

export async function GET() {
  try {
    const supabase = await createClient()
    const {
      data: { user },
    } = await supabase.auth.getUser()

    if (!user) {
      return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
    }

    const isPremium = await checkPremium(supabase, user.id)
    if (!isPremium) {
      return NextResponse.json(
        { error: 'Premium subscription required' },
        { status: 403 }
      )
    }

    // Fetch latest assessment
    const { data: assessment } = await supabase
      .from('assessments')
      .select('*')
      .eq('user_id', user.id)
      .eq('type', 'gdpr')
      .eq('status', 'complete')
      .order('created_at', { ascending: false })
      .limit(1)
      .single()

    if (!assessment) {
      return NextResponse.json(
        { error: 'No completed assessment found' },
        { status: 404 }
      )
    }

    // Fetch findings
    const { data: findings } = await supabase
      .from('findings')
      .select('*')
      .eq('assessment_id', assessment.id)
      .order('severity', { ascending: true })

    // Fetch business profile
    const { data: profile } = await supabase
      .from('business_profiles')
      .select('company_name')
      .eq('user_id', user.id)
      .single()

    const pdfBuffer = await renderToBuffer(
      React.createElement(ComplianceReport, {
        companyName: profile?.company_name || 'Your Company',
        date: new Date().toISOString().split('T')[0],
        overallScore: assessment.overall_score || 0,
        riskLevel: (assessment.risk_level as 'low' | 'medium' | 'high' | 'critical') || 'medium',
        summary:
          (assessment.result as Record<string, unknown>)?.summary as string ||
          'No summary available.',
        findings: (findings || []).map((f) => ({
          id: f.id,
          category: f.category,
          severity: f.severity as 'critical' | 'high' | 'medium' | 'low' | 'pass',
          title: f.title,
          description: f.description,
          recommendation: f.recommendation,
          gdpr_article: f.gdpr_article,
        })),
      })
    )

    return new Response(pdfBuffer, {
      headers: {
        'Content-Type': 'application/pdf',
        'Content-Disposition': `attachment; filename="kindlast-compliance-report-${new Date().toISOString().split('T')[0]}.pdf"`,
      },
    })
  } catch (error) {
    console.error('PDF export error:', error)
    return NextResponse.json(
      { error: 'Failed to generate report' },
      { status: 500 }
    )
  }
}
