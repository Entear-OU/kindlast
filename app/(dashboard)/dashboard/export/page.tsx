'use client'

import { useState } from 'react'
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@/components/ui/card'

export default function ExportPage() {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleDownload() {
    setLoading(true)
    setError(null)

    try {
      const response = await fetch('/api/export')

      if (!response.ok) {
        const data = await response.json()
        throw new Error(data.error || 'Failed to generate report')
      }

      const blob = await response.blob()
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `kindlast-compliance-report-${new Date().toISOString().split('T')[0]}.pdf`
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      URL.revokeObjectURL(url)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Export Compliance Report</h1>
        <p className="text-muted-foreground">
          Generate and download a PDF compliance report for your records
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>GDPR Compliance Report</CardTitle>
          <CardDescription>
            A comprehensive PDF document including your compliance score,
            detailed findings, recommendations, and legal disclaimer. Suitable
            for internal audits and stakeholder reporting.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="text-sm text-muted-foreground">
            <p>The report includes:</p>
            <ul className="mt-2 list-inside list-disc space-y-1">
              <li>Cover page with company details</li>
              <li>Overall compliance score and risk level</li>
              <li>Detailed findings with severity ratings</li>
              <li>Actionable recommendations for each finding</li>
              <li>Legal disclaimer</li>
            </ul>
          </div>

          <button
            onClick={handleDownload}
            disabled={loading}
            className="inline-flex h-9 items-center rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
          >
            {loading ? 'Generating...' : 'Generate & Download PDF'}
          </button>

          {error && (
            <p className="text-sm text-destructive">{error}</p>
          )}
        </CardContent>
      </Card>

      <p className="text-xs text-muted-foreground italic">
        Kindlast provides AI-generated compliance guidance for educational and
        planning purposes. It is not a substitute for professional legal advice.
      </p>
    </div>
  )
}
