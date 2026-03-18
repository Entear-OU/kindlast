import { NextResponse } from 'next/server'
import { createClient } from '@/lib/supabase/server'
import { checkPremium } from '@/lib/subscription/gate'
import { classifyAIRisk } from '@/lib/ai/classify-ai-risk'

export async function POST(request: Request) {
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

    const { data: profile } = await supabase
      .from('business_profiles')
      .select('ai_system_descriptions')
      .eq('user_id', user.id)
      .single()

    if (!profile?.ai_system_descriptions) {
      return NextResponse.json(
        { error: 'No AI systems found in your profile' },
        { status: 400 }
      )
    }

    const aiSystems = profile.ai_system_descriptions as Array<{
      name: string
      purpose: string
      dataUsed: string
      isAutomatedDecision: boolean
    }>

    const result = await classifyAIRisk(aiSystems)

    // Save as ai_act assessment
    await supabase.from('assessments').insert({
      user_id: user.id,
      type: 'ai_act',
      status: 'complete',
      result,
    })

    return NextResponse.json(result)
  } catch (error) {
    console.error('AI Act classification error:', error)
    return NextResponse.json(
      { error: 'Classification failed' },
      { status: 500 }
    )
  }
}
