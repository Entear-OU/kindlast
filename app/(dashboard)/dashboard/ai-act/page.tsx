'use client'

import { useState } from 'react'
import { RiskTierCard } from '@/components/ai-act/risk-tier-card'
import type { AIActClassification } from '@/lib/ai/classify-ai-risk'

export default function AIActPage() {
  const [classification, setClassification] =
    useState<AIActClassification | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function runClassification() {
    setLoading(true)
    setError(null)

    try {
      const response = await fetch('/api/classify', {
        method: 'POST',
      })

      if (!response.ok) {
        const data = await response.json()
        throw new Error(data.error || 'Classification failed')
      }

      const result = await response.json()
      setClassification(result)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">EU AI Act Classification</h1>
        <p className="text-muted-foreground">
          Classify your AI systems by risk tier under the EU AI Act
        </p>
      </div>

      {!classification && (
        <div className="flex flex-col items-center gap-4 rounded-lg border border-dashed p-8">
          <p className="text-center text-muted-foreground">
            Analyze your AI systems to determine their risk classification under
            the EU AI Act (Regulation (EU) 2024/1689).
          </p>
          <button
            onClick={runClassification}
            disabled={loading}
            className="inline-flex h-9 items-center rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
          >
            {loading ? 'Classifying...' : 'Classify AI Systems'}
          </button>
          {error && (
            <p className="text-sm text-destructive">{error}</p>
          )}
        </div>
      )}

      {classification && (
        <>
          <div className="rounded-lg border bg-muted/50 p-4">
            <h3 className="mb-1 font-medium">Summary</h3>
            <p className="text-sm text-muted-foreground">
              {classification.overall_summary}
            </p>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            {classification.systems.map((system, i) => (
              <RiskTierCard
                key={i}
                name={system.name}
                riskTier={system.risk_tier}
                reasoning={system.reasoning}
                obligations={system.obligations}
                aiActArticles={system.ai_act_articles}
                deadline={system.deadline}
              />
            ))}
          </div>

          <button
            onClick={runClassification}
            disabled={loading}
            className="inline-flex h-9 items-center rounded-lg border px-4 text-sm font-medium transition-colors hover:bg-muted disabled:opacity-50"
          >
            {loading ? 'Re-classifying...' : 'Re-classify'}
          </button>
        </>
      )}

      <p className="text-xs text-muted-foreground italic">
        Kindlast provides AI-generated compliance guidance for educational and
        planning purposes. It is not a substitute for professional legal advice.
        For binding compliance determinations, consult a qualified data
        protection attorney or certified DPO.
      </p>
    </div>
  )
}
