import { NextRequest, NextResponse } from 'next/server'
import { createClient } from '@/lib/supabase/server'
import { assessGDPRCompliance } from '@/lib/ai/assess-gdpr'

export async function POST(request: NextRequest) {
  try {
    const supabase = await createClient()
    const { data: { user } } = await supabase.auth.getUser()

    if (!user) {
      return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
    }

    const body = await request.json()
    const { profileId } = body

    if (!profileId) {
      return NextResponse.json({ error: 'profileId is required' }, { status: 400 })
    }

    // Fetch business profile
    const { data: profile, error: profileError } = await supabase
      .from('business_profiles')
      .select()
      .eq('id', profileId)
      .single()

    if (profileError || !profile) {
      return NextResponse.json({ error: 'Profile not found' }, { status: 404 })
    }

    // Create pending assessment
    const { data: assessment, error: assessmentError } = await supabase
      .from('assessments')
      .insert({
        user_id: user.id,
        profile_id: profileId,
        type: 'gdpr',
        status: 'processing',
      })
      .select()
      .single()

    if (assessmentError || !assessment) {
      return NextResponse.json({ error: 'Failed to create assessment' }, { status: 500 })
    }

    // Run AI assessment
    const result = await assessGDPRCompliance(profile)

    // Update assessment with results
    await supabase
      .from('assessments')
      .update({
        status: 'complete',
        overall_score: result.overall_score,
        risk_level: result.risk_level,
        result: result as unknown as Record<string, unknown>,
      })
      .eq('id', assessment.id)

    // Save individual findings
    const findings = result.findings.map((f) => ({
      assessment_id: assessment.id,
      user_id: user.id,
      ...f,
    }))

    if (findings.length > 0) {
      await supabase.from('findings').insert(findings)
    }

    return NextResponse.json({ assessmentId: assessment.id })
  } catch (error) {
    console.error('Assessment error:', error)
    return NextResponse.json({ error: 'Internal server error' }, { status: 500 })
  }
}
