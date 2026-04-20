import { NextResponse } from 'next/server'
import { renderToBuffer, DocumentProps } from '@react-pdf/renderer'
import React from 'react'
import { cookies } from 'next/headers'
import { getApiConfig, buildApiUrl, API_ENDPOINTS } from '@/lib/api/config'
import { ComplianceReport } from '@/lib/pdf/compliance-report'
import type { BusinessProfile, Assessment, Finding } from '@/lib/types/database'

export async function GET() {
  try {
    const config = getApiConfig()
    const cookieStore = await cookies()
    const accessToken = cookieStore.get(config.accessTokenCookie)?.value

    if (!accessToken) {
      return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
    }

    // Check user plan for premium access
    const planUrl = buildApiUrl(API_ENDPOINTS.users.plan, config)
    const planResponse = await fetch(planUrl, {
      headers: { 'Authorization': `Bearer ${accessToken}` },
    })

    if (!planResponse.ok) {
      return NextResponse.json({ error: 'Failed to check subscription' }, { status: 500 })
    }

    const plan = await planResponse.json()
    if (plan.plan !== 'premium' && plan.plan !== 'professional' && plan.plan !== 'team') {
      return NextResponse.json(
        { error: 'Premium subscription required' },
        { status: 403 }
      )
    }

    // Fetch latest assessment from Gateway
    const assessmentUrl = buildApiUrl(API_ENDPOINTS.assessments.latest, config)
    const assessmentResponse = await fetch(assessmentUrl, {
      headers: { 'Authorization': `Bearer ${accessToken}` },
    })

    if (!assessmentResponse.ok) {
      return NextResponse.json(
        { error: 'No completed assessment found' },
        { status: 404 }
      )
    }

    const assessment: Assessment = await assessmentResponse.json()

    if (assessment.status !== 'complete' || assessment.type !== 'gdpr') {
      return NextResponse.json(
        { error: 'No completed GDPR assessment found' },
        { status: 404 }
      )
    }

    // Fetch findings from Gateway
    const findingsUrl = buildApiUrl(API_ENDPOINTS.assessments.findings(assessment.id), config)
    const findingsResponse = await fetch(findingsUrl, {
      headers: { 'Authorization': `Bearer ${accessToken}` },
    })

    let findings: Finding[] = []
    if (findingsResponse.ok) {
      const findingsData = await findingsResponse.json()
      findings = findingsData.findings || []
    }

    // Fetch business profile from Gateway
    const profileUrl = buildApiUrl(API_ENDPOINTS.profile, config)
    const profileResponse = await fetch(profileUrl, {
      headers: { 'Authorization': `Bearer ${accessToken}` },
    })

    let profile: BusinessProfile | null = null
    if (profileResponse.ok) {
      profile = await profileResponse.json()
    }

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const reportElement = React.createElement(ComplianceReport, {
      companyName: profile?.company_name || 'Your Company',
      date: new Date().toISOString().split('T')[0],
      overallScore: assessment.overall_score || 0,
      riskLevel: (assessment.risk_level as 'low' | 'medium' | 'high' | 'critical') || 'medium',
      summary:
        (assessment.result as Record<string, unknown>)?.summary as string ||
        'No summary available.',
      findings: findings.map((f) => ({
        id: f.id,
        category: f.category,
        severity: f.severity as 'critical' | 'high' | 'medium' | 'low' | 'pass',
        title: f.title,
        description: f.description,
        recommendation: f.recommendation,
        gdpr_article: f.gdpr_article,
      })),
    })

    const pdfBuffer = await renderToBuffer(
      reportElement as unknown as React.ReactElement<DocumentProps>
    )

    return new Response(new Uint8Array(pdfBuffer), {
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
