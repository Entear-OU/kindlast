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
  MoreVertical,
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
    return (
      <div className="flex flex-col gap-6 p-6">
        <Skeleton className="h-4 w-32" />
        <div className="flex items-center gap-4">
          <Skeleton className="h-16 w-16 rounded-lg" />
          <div className="space-y-2">
            <Skeleton className="h-6 w-48" />
            <Skeleton className="h-4 w-32" />
          </div>
        </div>
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }

  if (error || !client) {
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
          <p className="text-destructive">{error || 'Client not found.'}</p>
          <Link href="/dashboard/clients" className="mt-4 inline-block">
            <Button variant="outline">Return to Clients</Button>
          </Link>
        </div>
      </div>
    )
  }

  const sectorLabel = client.sector ? sectorLabels[client.sector] || client.sector : null

  return (
    <div className="flex flex-col gap-6 p-6">
      {/* Breadcrumb */}
      <Link
        href="/dashboard/clients"
        className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to Clients
      </Link>

      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-center gap-4">
          <div className="flex h-16 w-16 items-center justify-center rounded-lg bg-primary/10">
            <Building2 className="h-8 w-8 text-primary" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-2xl font-bold">{client.name}</h1>
              <Badge variant={client.status === 'active' ? 'default' : 'secondary'}>
                {client.status}
              </Badge>
            </div>
            {sectorLabel && (
              <p className="text-sm text-muted-foreground">{sectorLabel}</p>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Link href={`/dashboard/clients/${clientId}/artifacts/new`}>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Generate Artifact
            </Button>
          </Link>
          <Link href={`/dashboard/clients/${clientId}/edit`}>
            <Button variant="outline" size="icon">
              <Edit className="h-4 w-4" />
            </Button>
          </Link>
          <Button
            variant="outline"
            size="icon"
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

      {/* Client Details */}
      <div className="grid gap-6 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Organization Details</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {client.description && (
              <div>
                <p className="text-xs font-medium text-muted-foreground uppercase mb-1">
                  Description
                </p>
                <p className="text-sm">{client.description}</p>
              </div>
            )}

            <div className="flex flex-wrap gap-4">
              {client.country && (
                <div>
                  <p className="text-xs font-medium text-muted-foreground uppercase mb-1">
                    Country
                  </p>
                  <p className="text-sm flex items-center gap-1">
                    <MapPin className="h-3 w-3" />
                    {client.country}
                  </p>
                </div>
              )}
              {client.employee_count && (
                <div>
                  <p className="text-xs font-medium text-muted-foreground uppercase mb-1">
                    Employees
                  </p>
                  <p className="text-sm flex items-center gap-1">
                    <Users className="h-3 w-3" />
                    {client.employee_count}
                  </p>
                </div>
              )}
              <div>
                <p className="text-xs font-medium text-muted-foreground uppercase mb-1">
                  Added
                </p>
                <p className="text-sm flex items-center gap-1">
                  <Calendar className="h-3 w-3" />
                  {formatDate(client.created_at)}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Processing Context</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {client.tech_stack && client.tech_stack.length > 0 && (
              <div>
                <p className="text-xs font-medium text-muted-foreground uppercase mb-2">
                  Tech Stack
                </p>
                <div className="flex flex-wrap gap-1">
                  {client.tech_stack.map((tool) => (
                    <Badge key={tool} variant="outline" className="text-xs">
                      {tool}
                    </Badge>
                  ))}
                </div>
              </div>
            )}

            {client.data_subjects && client.data_subjects.length > 0 && (
              <div>
                <p className="text-xs font-medium text-muted-foreground uppercase mb-2">
                  Data Subjects
                </p>
                <div className="flex flex-wrap gap-1">
                  {client.data_subjects.map((subject) => (
                    <Badge key={subject} variant="secondary" className="text-xs">
                      {subject}
                    </Badge>
                  ))}
                </div>
              </div>
            )}

            {client.processing_purposes && client.processing_purposes.length > 0 && (
              <div>
                <p className="text-xs font-medium text-muted-foreground uppercase mb-2">
                  Processing Purposes
                </p>
                <div className="flex flex-wrap gap-1">
                  {client.processing_purposes.map((purpose) => (
                    <Badge key={purpose} variant="secondary" className="text-xs">
                      {purpose}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Artifacts Section */}
      <div>
        <div className="flex items-center justify-between mb-4">
          <div>
            <h2 className="text-lg font-semibold">Artifacts</h2>
            <p className="text-sm text-muted-foreground">
              Generated compliance documents for this client.
            </p>
          </div>
          <Link href={`/dashboard/clients/${clientId}/artifacts`}>
            <Button variant="outline" size="sm">
              View All
            </Button>
          </Link>
        </div>

        {artifacts.length === 0 ? (
          <Card className="p-8 text-center">
            <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-muted">
              <FileText className="h-6 w-6 text-muted-foreground" />
            </div>
            <h3 className="font-semibold">No artifacts yet</h3>
            <p className="mt-1 text-sm text-muted-foreground">
              Generate your first compliance artifact for this client.
            </p>
            <Link
              href={`/dashboard/clients/${clientId}/artifacts/new`}
              className="mt-4 inline-block"
            >
              <Button>
                <Plus className="mr-2 h-4 w-4" />
                Generate Artifact
              </Button>
            </Link>
          </Card>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
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
  )
}
