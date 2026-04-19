'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { ArrowLeft, Loader2, Sparkles } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { ArtifactTypeSelector } from '@/components/clients'
import {
  getClient,
  generateArtifact,
  isPlanLimitError,
  isNotFoundError,
} from '@/lib/api/clients'
import type { Client, ArtifactType } from '@/lib/types/database'

interface GenerateArtifactPageProps {
  params: Promise<{ clientId: string }>
}

export default function GenerateArtifactPage({ params }: GenerateArtifactPageProps) {
  const router = useRouter()
  const [clientId, setClientId] = useState<string | null>(null)
  const [client, setClient] = useState<Client | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [selectedType, setSelectedType] = useState<ArtifactType | null>(null)
  const [inputContext, setInputContext] = useState('')
  const [isGenerating, setIsGenerating] = useState(false)

  // Unwrap params Promise
  useEffect(() => {
    params.then((resolved) => setClientId(resolved.clientId))
  }, [params])

  useEffect(() => {
    if (!clientId) return

    async function fetchClient() {
      try {
        setIsLoading(true)
        const clientData = await getClient(clientId!)
        setClient(clientData)

        // Pre-populate input context with client description
        if (clientData.description) {
          setInputContext(clientData.description)
        }

        setError(null)
      } catch (err) {
        if (isNotFoundError(err)) {
          setError('Client not found.')
        } else {
          setError('Failed to load client.')
        }
        console.error('Error fetching client:', err)
      } finally {
        setIsLoading(false)
      }
    }

    fetchClient()
  }, [clientId])

  const handleGenerate = async () => {
    if (!clientId || !selectedType || !inputContext.trim()) return

    try {
      setIsGenerating(true)
      setError(null)
      const artifact = await generateArtifact(clientId, {
        type: selectedType,
        input_context: inputContext.trim(),
      })
      router.push(`/dashboard/clients/${clientId}/artifacts/${artifact.id}`)
    } catch (err) {
      if (isPlanLimitError(err)) {
        setError(
          'You have reached your artifact generation limit for this month. Please upgrade your plan.'
        )
      } else {
        setError('Failed to generate artifact. Please try again.')
      }
      console.error('Error generating artifact:', err)
    } finally {
      setIsGenerating(false)
    }
  }

  if (isLoading) {
    return (
      <div className="flex flex-col gap-6 p-6">
        <Skeleton className="h-4 w-32" />
        <Skeleton className="h-8 w-64" />
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[...Array(5)].map((_, i) => (
            <Skeleton key={i} className="h-48" />
          ))}
        </div>
      </div>
    )
  }

  if (error && !client) {
    return (
      <div className="flex flex-col gap-6 p-6">
        <Link
          href="/dashboard/clients"
          className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Clients
        </Link>
        <div className="rounded-lg border border-destructive/50 bg-destructive/10 p-8 text-center">
          <p className="text-destructive">{error}</p>
          <Link href="/dashboard/clients" className="mt-4 inline-block">
            <Button variant="outline">Return to Clients</Button>
          </Link>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6 p-6">
      {/* Breadcrumb */}
      <Link
        href={`/dashboard/clients/${clientId}`}
        className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to {client?.name}
      </Link>

      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold">Generate Artifact</h1>
        <p className="text-sm text-muted-foreground">
          Select the type of compliance document to generate for {client?.name}.
        </p>
      </div>

      {error && (
        <div className="rounded-lg border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive">
          {error}
        </div>
      )}

      {/* Step 1: Select Artifact Type */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">1. Select Artifact Type</CardTitle>
          <CardDescription>
            Choose the type of compliance document you want to generate.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ArtifactTypeSelector
            selectedType={selectedType}
            onSelect={setSelectedType}
            disabled={isGenerating}
          />
        </CardContent>
      </Card>

      {/* Step 2: Provide Context */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">2. Provide Context</CardTitle>
          <CardDescription>
            Describe the business activities and data processing to analyze. The more detail you
            provide, the better the generated artifact will be.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="input_context">Business Description</Label>
            <Textarea
              id="input_context"
              value={inputContext}
              onChange={(e) => setInputContext(e.target.value)}
              placeholder="Describe the client's business activities, the data they process, third-party tools they use, and any specific compliance concerns..."
              rows={6}
              disabled={isGenerating}
            />
            <p className="text-xs text-muted-foreground">
              Include details about: data subjects, processing purposes, third-party processors,
              data transfers, and any special categories of data.
            </p>
          </div>

          {/* Pre-populated context info */}
          {client && (
            <div className="rounded-lg bg-muted/50 p-4 space-y-2">
              <p className="text-xs font-medium text-muted-foreground uppercase">
                Client Context (Pre-populated)
              </p>
              <div className="flex flex-wrap gap-4 text-xs">
                {client.sector && (
                  <span>
                    <span className="text-muted-foreground">Sector:</span> {client.sector}
                  </span>
                )}
                {client.country && (
                  <span>
                    <span className="text-muted-foreground">Country:</span> {client.country}
                  </span>
                )}
                {client.employee_count && (
                  <span>
                    <span className="text-muted-foreground">Employees:</span> {client.employee_count}
                  </span>
                )}
              </div>
              {client.tech_stack && client.tech_stack.length > 0 && (
                <p className="text-xs">
                  <span className="text-muted-foreground">Tech Stack:</span>{' '}
                  {client.tech_stack.join(', ')}
                </p>
              )}
              {client.data_subjects && client.data_subjects.length > 0 && (
                <p className="text-xs">
                  <span className="text-muted-foreground">Data Subjects:</span>{' '}
                  {client.data_subjects.join(', ')}
                </p>
              )}
              {client.processing_purposes && client.processing_purposes.length > 0 && (
                <p className="text-xs">
                  <span className="text-muted-foreground">Processing Purposes:</span>{' '}
                  {client.processing_purposes.join(', ')}
                </p>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Generate Button */}
      <div className="flex items-center gap-4">
        <Button
          onClick={handleGenerate}
          disabled={!selectedType || !inputContext.trim() || isGenerating}
          size="lg"
        >
          {isGenerating ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              Generating...
            </>
          ) : (
            <>
              <Sparkles className="mr-2 h-4 w-4" />
              Generate Artifact
            </>
          )}
        </Button>
        <Button
          variant="outline"
          onClick={() => router.back()}
          disabled={isGenerating}
        >
          Cancel
        </Button>
      </div>

      {/* Generation Info */}
      {isGenerating && (
        <Card className="border-primary/50 bg-primary/5">
          <CardContent className="pt-6">
            <div className="flex items-center gap-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-full bg-primary/10">
                <Loader2 className="h-5 w-5 animate-spin text-primary" />
              </div>
              <div>
                <p className="font-medium">Generating your artifact...</p>
                <p className="text-sm text-muted-foreground">
                  This typically takes 10-20 seconds. We are analyzing your business context against
                  our regulatory corpus and generating a cited compliance document.
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
