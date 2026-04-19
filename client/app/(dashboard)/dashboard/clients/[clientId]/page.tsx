'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import {
  ArrowLeft,
  Building2,
  MapPin,
  Users,
  Calendar,
  FileText,
  Edit,
  Archive,
  Plus,
  Loader2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { ArtifactCard } from '@/components/clients'
import {
  getClient,
  listArtifacts,
  archiveClient,
  isNotFoundError,
} from '@/lib/api/clients'
import type { Client, Artifact } from '@/lib/types/database'

interface ClientDetailPageProps {
  params: Promise<{ clientId: string }>
}

const sectorLabels: Record<string, string> = {
  fintech: 'Fintech',
  healthtech: 'Healthtech',
  saas: 'SaaS',
  ecommerce: 'E-commerce',
  edtech: 'EdTech',
  legaltech: 'LegalTech',
  proptech: 'PropTech',
  insurtech: 'InsurTech',
  manufacturing: 'Manufacturing',
  retail: 'Retail',
  professional_services: 'Professional Services',
  non_profit: 'Non-profit',
  government: 'Government',
  other: 'Other',
}

function formatDate(dateString: string): string {
  const date = new Date(dateString)
  return date.toLocaleDateString('en-GB', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  })
}

function PageSkeleton() {
  return (
    <div className="min-h-screen">
      <div className="px-6 pt-8 lg:px-12">
        <Skeleton className="h-4 w-32 mb-8" />
      </div>
      <header className="px-6 pb-8 lg:px-12">
        <div className="max-w-6xl">
          <div className="flex items-start gap-6">
            <Skeleton className="h-20 w-20 rounded-xl" />
            <div className="space-y-3 flex-1">
              <Skeleton className="h-10 w-64" />
              <Skeleton className="h-5 w-32" />
            </div>
          </div>
        </div>
      </header>
      <div className="px-6 pb-12 lg:px-12">
        <div className="max-w-6xl grid gap-8 lg:grid-cols-2">
          <Skeleton className="h-48 rounded-xl" />
          <Skeleton className="h-48 rounded-xl" />
        </div>
      </div>
    </div>
  )
}

function EmptyArtifactsState({ clientId }: { clientId: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 px-6 rounded-xl border border-border/40 bg-card">
      <div className="relative mb-6">
        <div className="absolute -inset-3 rounded-full bg-muted/50" />
        <div className="absolute -inset-6 rounded-full bg-muted/30" />
        <div className="relative flex h-16 w-16 items-center justify-center rounded-full bg-muted">
          <FileText className="h-8 w-8 text-muted-foreground/60" strokeWidth={1.5} />
        </div>
      </div>
      <h3 className="text-lg font-medium text-[#111111] tracking-tight">
        No artifacts yet
      </h3>
      <p className="mt-2 text-muted-foreground max-w-xs text-center leading-relaxed">
        Generate your first compliance artifact for this client.
      </p>
      <Link href={`/dashboard/clients/${clientId}/artifacts/new`} className="mt-6">
        <Button className="gap-2">
          <Plus className="h-4 w-4" />
          Generate Artifact
        </Button>
      </Link>
    </div>
  )
}

export default function ClientDetailPage({ params }: ClientDetailPageProps) {
  const router = useRouter()
  const [clientId, setClientId] = useState<string | null>(null)
  const [client, setClient] = useState<Client | null>(null)
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [isArchiving, setIsArchiving] = useState(false)

  // Unwrap params Promise
  useEffect(() => {
    params.then((resolved) => setClientId(resolved.clientId))
  }, [params])

  useEffect(() => {
    if (!clientId) return

    async function fetchData() {
      try {
        setIsLoading(true)
        const [clientData, artifactsData] = await Promise.all([
          getClient(clientId!),
          listArtifacts(clientId!),
        ])
        setClient(clientData)
        setArtifacts(artifactsData.artifacts)
        setError(null)
      } catch (err) {
        if (isNotFoundError(err)) {
          setError('Client not found.')
        } else {
          setError('Failed to load client details.')
        }
        console.error('Error fetching client:', err)
      } finally {
        setIsLoading(false)
      }
    }

    fetchData()
  }, [clientId])

  const handleArchive = async () => {
    if (!clientId || !client) return

    const confirmed = window.confirm(
      `Are you sure you want to archive "${client.name}"? This will hide them from the active clients list.`
    )

    if (!confirmed) return

    try {
      setIsArchiving(true)
      await archiveClient(clientId)
      router.push('/dashboard/clients')
    } catch (err) {
      setError('Failed to archive client.')
      console.error('Error archiving client:', err)
    } finally {
      setIsArchiving(false)
    }
  }

  if (isLoading) {
    return <PageSkeleton />
  }

  if (error || !client) {
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
            <p className="text-destructive mb-4">{error || 'Client not found.'}</p>
            <Link href="/dashboard/clients">
              <Button variant="outline">Return to Clients</Button>
            </Link>
          </div>
        </div>
      </div>
    )
  }

  const sectorLabel = client.sector ? sectorLabels[client.sector] || client.sector : null

  return (
    <div className="min-h-screen">
      {/* Breadcrumb */}
      <div className="px-6 pt-8 lg:px-12">
        <div className="max-w-6xl">
          <Link
            href="/dashboard/clients"
            className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-[#111111] transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
            Back to Clients
          </Link>
        </div>
      </div>

      {/* Page Header */}
      <header className="px-6 pt-6 pb-10 lg:px-12">
        <div className="max-w-6xl">
          <div className="grid grid-cols-1 lg:grid-cols-[1fr,auto] gap-8 items-start">
            <div className="flex items-start gap-6">
              <div className="flex h-20 w-20 items-center justify-center rounded-xl bg-primary/10 shrink-0">
                <Building2 className="h-10 w-10 text-primary" strokeWidth={1.5} />
              </div>
              <div className="space-y-2">
                <div className="flex items-center gap-3 flex-wrap">
                  <h1 className="text-4xl font-semibold tracking-tighter text-[#111111]">
                    {client.name}
                  </h1>
                  <Badge
                    variant={client.status === 'active' ? 'default' : 'secondary'}
                    className="text-xs"
                  >
                    {client.status}
                  </Badge>
                </div>
                {sectorLabel && (
                  <p className="text-lg text-muted-foreground">{sectorLabel}</p>
                )}
              </div>
            </div>
            <div className="flex items-center gap-3">
              <Link href={`/dashboard/clients/${clientId}/artifacts/new`}>
                <Button size="lg" className="gap-2">
                  <Plus className="h-4 w-4" />
                  Generate Artifact
                </Button>
              </Link>
              <Link href={`/dashboard/clients/${clientId}/edit`}>
                <Button variant="outline" size="icon" className="h-11 w-11">
                  <Edit className="h-4 w-4" />
                </Button>
              </Link>
              <Button
                variant="outline"
                size="icon"
                className="h-11 w-11"
                onClick={handleArchive}
                disabled={isArchiving}
              >
                {isArchiving ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Archive className="h-4 w-4" />
                )}
              </Button>
            </div>
          </div>
        </div>
      </header>

      {/* Client Details */}
      <div className="px-6 pb-12 lg:px-12">
        <div className="max-w-6xl">
          <div className="grid gap-8 lg:grid-cols-2">
            <Card className="border-border/40">
              <CardHeader className="pb-4">
                <CardTitle className="text-base font-medium text-[#111111]">
                  Organization Details
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-6">
                {client.description && (
                  <div>
                    <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">
                      Description
                    </p>
                    <p className="text-sm leading-relaxed">{client.description}</p>
                  </div>
                )}

                <div className="grid grid-cols-2 sm:grid-cols-3 gap-6">
                  {client.country && (
                    <div>
                      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">
                        Country
                      </p>
                      <p className="text-sm flex items-center gap-2">
                        <MapPin className="h-4 w-4 text-muted-foreground" />
                        {client.country}
                      </p>
                    </div>
                  )}
                  {client.employee_count && (
                    <div>
                      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">
                        Employees
                      </p>
                      <p className="text-sm flex items-center gap-2">
                        <Users className="h-4 w-4 text-muted-foreground" />
                        {client.employee_count}
                      </p>
                    </div>
                  )}
                  <div>
                    <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">
                      Added
                    </p>
                    <p className="text-sm flex items-center gap-2">
                      <Calendar className="h-4 w-4 text-muted-foreground" />
                      {formatDate(client.created_at)}
                    </p>
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card className="border-border/40">
              <CardHeader className="pb-4">
                <CardTitle className="text-base font-medium text-[#111111]">
                  Processing Context
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-6">
                {client.tech_stack && client.tech_stack.length > 0 && (
                  <div>
                    <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-3">
                      Tech Stack
                    </p>
                    <div className="flex flex-wrap gap-2">
                      {client.tech_stack.map((tool) => (
                        <Badge key={tool} variant="outline" className="text-xs font-normal">
                          {tool}
                        </Badge>
                      ))}
                    </div>
                  </div>
                )}

                {client.data_subjects && client.data_subjects.length > 0 && (
                  <div>
                    <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-3">
                      Data Subjects
                    </p>
                    <div className="flex flex-wrap gap-2">
                      {client.data_subjects.map((subject) => (
                        <Badge key={subject} variant="secondary" className="text-xs font-normal">
                          {subject}
                        </Badge>
                      ))}
                    </div>
                  </div>
                )}

                {client.processing_purposes && client.processing_purposes.length > 0 && (
                  <div>
                    <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-3">
                      Processing Purposes
                    </p>
                    <div className="flex flex-wrap gap-2">
                      {client.processing_purposes.map((purpose) => (
                        <Badge key={purpose} variant="secondary" className="text-xs font-normal">
                          {purpose}
                        </Badge>
                      ))}
                    </div>
                  </div>
                )}

                {!client.tech_stack?.length && !client.data_subjects?.length && !client.processing_purposes?.length && (
                  <p className="text-sm text-muted-foreground">
                    No processing context defined yet.
                  </p>
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      </div>

      {/* Artifacts Section */}
      <div className="px-6 pb-16 lg:px-12">
        <div className="max-w-6xl">
          <div className="flex items-end justify-between mb-8">
            <div>
              <h2 className="text-2xl font-semibold tracking-tight text-[#111111]">
                Artifacts
              </h2>
              <p className="mt-1 text-muted-foreground">
                Generated compliance documents for this client.
              </p>
            </div>
            {artifacts.length > 0 && (
              <Link href={`/dashboard/clients/${clientId}/artifacts`}>
                <Button variant="outline">View All</Button>
              </Link>
            )}
          </div>

          {artifacts.length === 0 ? (
            <EmptyArtifactsState clientId={clientId!} />
          ) : (
            <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-3">
              {artifacts.slice(0, 6).map((artifact) => (
                <ArtifactCard
                  key={artifact.id}
                  artifact={artifact}
                  clientId={clientId!}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
