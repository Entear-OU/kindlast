'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { ArrowLeft, Loader2, Sparkles, FileText } from 'lucide-react'
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

function PageSkeleton() {
  return (
    <div className="min-h-screen">
      <div className="px-6 pt-8 lg:px-12">
        <Skeleton className="h-4 w-32 mb-8" />
      </div>
      <header className="px-6 pb-8 lg:px-12">
        <div className="max-w-3xl">
          <Skeleton className="h-10 w-56 mb-2" />
          <Skeleton className="h-5 w-80" />
        </div>
      </header>
      <div className="px-6 pb-12 lg:px-12">
        <div className="max-w-3xl space-y-8">
          <div className="rounded-xl border border-border/40 bg-card p-6">
            <Skeleton className="h-6 w-40 mb-2" />
            <Skeleton className="h-4 w-72 mb-6" />
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {[...Array(5)].map((_, i) => (
                <Skeleton key={i} className="h-32 rounded-lg" />
              ))}
            </div>
          </div>
          <div className="rounded-xl border border-border/40 bg-card p-6">
            <Skeleton className="h-6 w-36 mb-2" />
            <Skeleton className="h-4 w-96 mb-6" />
            <Skeleton className="h-40 w-full" />
          </div>
        </div>
      </div>
    </div>
  )
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
    return <PageSkeleton />
  }

  if (error && !client) {
    return (
      <div className="min-h-screen">
        <div className="px-6 pt-8 lg:px-12">
          <Link
            href="/dashboard/clients"
            className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-[#111111] transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
            Back to Clients
          </Link>
        </div>
        <div className="px-6 py-12 lg:px-12">
          <div className="max-w-md mx-auto rounded-xl border border-destructive/30 bg-destructive/5 p-8 text-center">
            <p className="text-destructive mb-4">{error}</p>
            <Link href="/dashboard/clients">
              <Button variant="outline">Return to Clients</Button>
            </Link>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen">
      {/* Breadcrumb */}
      <div className="px-6 pt-8 lg:px-12">
        <div className="max-w-3xl">
          <Link
            href={`/dashboard/clients/${clientId}`}
            className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-[#111111] transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
            Back to {client?.name}
          </Link>
        </div>
      </div>

      {/* Page Header */}
      <header className="px-6 pt-6 pb-10 lg:px-12">
        <div className="max-w-3xl">
          <div className="flex items-start gap-6">
            <div className="flex h-16 w-16 items-center justify-center rounded-xl bg-primary/10 shrink-0">
              <Sparkles className="h-8 w-8 text-primary" strokeWidth={1.5} />
            </div>
            <div>
              <h1 className="text-4xl font-semibold tracking-tighter text-[#111111]">
                Generate Artifact
              </h1>
              <p className="mt-2 text-muted-foreground text-lg">
                Select the type of compliance document to generate for {client?.name}.
              </p>
            </div>
          </div>
        </div>
      </header>

      {/* Error Alert */}
      {error && (
        <div className="px-6 pb-6 lg:px-12">
          <div className="max-w-3xl">
            <div className="rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-destructive">
              {error}
            </div>
          </div>
        </div>
      )}

      {/* Main Content */}
      <div className="px-6 pb-12 lg:px-12">
        <div className="max-w-3xl space-y-8">
          {/* Step 1: Select Artifact Type */}
          <Card className="border-border/40">
            <CardHeader className="pb-4">
              <div className="flex items-center gap-4">
                <span className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10 text-sm font-semibold text-primary">
                  1
                </span>
                <div>
                  <CardTitle className="text-lg font-medium text-[#111111]">
                    Select Artifact Type
                  </CardTitle>
                  <CardDescription className="mt-1">
                    Choose the type of compliance document you want to generate.
                  </CardDescription>
                </div>
              </div>
            </CardHeader>
            <CardContent className="pt-2">
              <ArtifactTypeSelector
                selectedType={selectedType}
                onSelect={setSelectedType}
                disabled={isGenerating}
              />
            </CardContent>
          </Card>

          {/* Step 2: Provide Context */}
          <Card className="border-border/40">
            <CardHeader className="pb-4">
              <div className="flex items-center gap-4">
                <span className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10 text-sm font-semibold text-primary">
                  2
                </span>
                <div>
                  <CardTitle className="text-lg font-medium text-[#111111]">
                    Provide Context
                  </CardTitle>
                  <CardDescription className="mt-1">
                    Describe the business activities and data processing to analyze.
                  </CardDescription>
                </div>
              </div>
            </CardHeader>
            <CardContent className="pt-2 space-y-6">
              <div className="space-y-3">
                <Label htmlFor="input_context" className="text-sm font-medium text-[#111111]">
                  Business Description
                </Label>
                <Textarea
                  id="input_context"
                  value={inputContext}
                  onChange={(e) => setInputContext(e.target.value)}
                  placeholder="Describe the client's business activities, the data they process, third-party tools they use, and any specific compliance concerns..."
                  rows={6}
                  disabled={isGenerating}
                  className="resize-none bg-background border-border/40"
                />
                <p className="text-sm text-muted-foreground leading-relaxed">
                  Include details about: data subjects, processing purposes, third-party processors,
                  data transfers, and any special categories of data. The more detail you provide,
                  the better the generated artifact will be.
                </p>
              </div>

              {/* Pre-populated context info */}
              {client && (
                <div className="rounded-xl bg-muted/30 p-5 space-y-4">
                  <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Client Context (Pre-populated)
                  </p>
                  <div className="grid grid-cols-2 sm:grid-cols-3 gap-4">
                    {client.sector && (
                      <div>
                        <p className="text-xs text-muted-foreground mb-1">Sector</p>
                        <p className="text-sm font-medium">{client.sector}</p>
                      </div>
                    )}
                    {client.country && (
                      <div>
                        <p className="text-xs text-muted-foreground mb-1">Country</p>
                        <p className="text-sm font-medium">{client.country}</p>
                      </div>
                    )}
                    {client.employee_count && (
                      <div>
                        <p className="text-xs text-muted-foreground mb-1">Employees</p>
                        <p className="text-sm font-medium">{client.employee_count}</p>
                      </div>
                    )}
                  </div>
                  {client.tech_stack && client.tech_stack.length > 0 && (
                    <div>
                      <p className="text-xs text-muted-foreground mb-2">Tech Stack</p>
                      <p className="text-sm">{client.tech_stack.join(', ')}</p>
                    </div>
                  )}
                  {client.data_subjects && client.data_subjects.length > 0 && (
                    <div>
                      <p className="text-xs text-muted-foreground mb-2">Data Subjects</p>
                      <p className="text-sm">{client.data_subjects.join(', ')}</p>
                    </div>
                  )}
                  {client.processing_purposes && client.processing_purposes.length > 0 && (
                    <div>
                      <p className="text-xs text-muted-foreground mb-2">Processing Purposes</p>
                      <p className="text-sm">{client.processing_purposes.join(', ')}</p>
                    </div>
                  )}
                </div>
              )}
            </CardContent>
          </Card>

          {/* Generation Info */}
          {isGenerating && (
            <Card className="border-primary/20 bg-primary/5">
              <CardContent className="py-6">
                <div className="flex items-start gap-5">
                  <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-primary/10 shrink-0">
                    <Loader2 className="h-6 w-6 animate-spin text-primary" />
                  </div>
                  <div>
                    <p className="font-medium text-[#111111]">Generating your artifact...</p>
                    <p className="mt-1 text-sm text-muted-foreground leading-relaxed">
                      This typically takes 10-20 seconds. We are analyzing your business context against
                      our regulatory corpus and generating a cited compliance document.
                    </p>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          {/* Action Buttons */}
          <div className="flex items-center gap-4 pt-4">
            <Button
              onClick={handleGenerate}
              disabled={!selectedType || !inputContext.trim() || isGenerating}
              size="lg"
              className="gap-2"
            >
              {isGenerating ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Generating...
                </>
              ) : (
                <>
                  <Sparkles className="h-4 w-4" />
                  Generate Artifact
                </>
              )}
            </Button>
            <Button
              variant="outline"
              size="lg"
              onClick={() => router.back()}
              disabled={isGenerating}
            >
              Cancel
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
