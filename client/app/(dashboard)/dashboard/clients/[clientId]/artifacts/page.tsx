'use client'

import { useState, useEffect } from 'react'
import Link from 'next/link'
import { ArrowLeft, Plus, FileText, Filter } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { ArtifactCard } from '@/components/clients'
import {
  getClient,
  listArtifacts,
  isNotFoundError,
} from '@/lib/api/clients'
import type { Client, Artifact, ArtifactType, ArtifactStatus } from '@/lib/types/database'

interface ClientArtifactsPageProps {
  params: Promise<{ clientId: string }>
}

const artifactTypeLabels: Record<ArtifactType, string> = {
  ropa: 'RoPA',
  dpia_screening: 'DPIA Screening',
  dpa_gap: 'DPA Gap Analysis',
  lawful_basis: 'Lawful Basis',
  ai_act_classification: 'AI Act Classification',
}

const statusLabels: Record<ArtifactStatus, string> = {
  draft: 'Draft',
  reviewed: 'Reviewed',
  approved: 'Approved',
  exported: 'Exported',
}

export default function ClientArtifactsPage({ params }: ClientArtifactsPageProps) {
  const [clientId, setClientId] = useState<string | null>(null)
  const [client, setClient] = useState<Client | null>(null)
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [typeFilter, setTypeFilter] = useState<ArtifactType | 'all'>('all')
  const [statusFilter, setStatusFilter] = useState<ArtifactStatus | 'all'>('all')

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
          listArtifacts(clientId!, {
            type: typeFilter === 'all' ? undefined : typeFilter,
            status: statusFilter === 'all' ? undefined : statusFilter,
          }),
        ])
        setClient(clientData)
        setArtifacts(artifactsData.artifacts)
        setError(null)
      } catch (err) {
        if (isNotFoundError(err)) {
          setError('Client not found.')
        } else {
          setError('Failed to load artifacts.')
        }
        console.error('Error fetching artifacts:', err)
      } finally {
        setIsLoading(false)
      }
    }

    fetchData()
  }, [clientId, typeFilter, statusFilter])

  if (isLoading && !client) {
    return (
      <div className="flex flex-col gap-6 p-6">
        <Skeleton className="h-4 w-32" />
        <Skeleton className="h-8 w-64" />
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[...Array(6)].map((_, i) => (
            <div key={i} className="rounded-xl border bg-card p-4">
              <div className="flex items-center gap-3 mb-4">
                <Skeleton className="h-10 w-10 rounded-lg" />
                <div className="space-y-2">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-3 w-48" />
                </div>
              </div>
              <Skeleton className="h-12 w-full mb-3" />
              <div className="flex gap-4">
                <Skeleton className="h-3 w-16" />
                <Skeleton className="h-3 w-20" />
              </div>
            </div>
          ))}
        </div>
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

  return (
    <div className="flex flex-col gap-6 p-6">
      {/* Breadcrumb */}
      <Link
        href={`/dashboard/clients/${clientId}`}
        className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to {client.name}
      </Link>

      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold">Artifacts</h1>
          <p className="text-sm text-muted-foreground">
            Compliance documents generated for {client.name}.
          </p>
        </div>
        <Link href={`/dashboard/clients/${clientId}/artifacts/new`}>
          <Button>
            <Plus className="mr-2 h-4 w-4" />
            Generate Artifact
          </Button>
        </Link>
      </div>

      {/* Filters */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
        <Select
          value={typeFilter}
          onValueChange={(value: string | null) => value && setTypeFilter(value as ArtifactType | 'all')}
        >
          <SelectTrigger className="w-[200px]">
            <Filter className="mr-2 h-4 w-4" />
            <SelectValue placeholder="Filter by type" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Types</SelectItem>
            {(Object.keys(artifactTypeLabels) as ArtifactType[]).map((type) => (
              <SelectItem key={type} value={type}>
                {artifactTypeLabels[type]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select
          value={statusFilter}
          onValueChange={(value: string | null) => value && setStatusFilter(value as ArtifactStatus | 'all')}
        >
          <SelectTrigger className="w-[150px]">
            <SelectValue placeholder="Filter by status" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Status</SelectItem>
            {(Object.keys(statusLabels) as ArtifactStatus[]).map((status) => (
              <SelectItem key={status} value={status}>
                {statusLabels[status]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Loading State */}
      {isLoading && (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[...Array(6)].map((_, i) => (
            <div key={i} className="rounded-xl border bg-card p-4">
              <div className="flex items-center gap-3 mb-4">
                <Skeleton className="h-10 w-10 rounded-lg" />
                <div className="space-y-2">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-3 w-48" />
                </div>
              </div>
              <Skeleton className="h-12 w-full mb-3" />
              <div className="flex gap-4">
                <Skeleton className="h-3 w-16" />
                <Skeleton className="h-3 w-20" />
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Empty State */}
      {!isLoading && artifacts.length === 0 && (
        <div className="rounded-lg border bg-card p-12 text-center">
          <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-muted">
            <FileText className="h-8 w-8 text-muted-foreground" />
          </div>
          <h2 className="text-lg font-semibold">
            {typeFilter !== 'all' || statusFilter !== 'all'
              ? 'No matching artifacts'
              : 'No artifacts yet'}
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            {typeFilter !== 'all' || statusFilter !== 'all'
              ? 'Try adjusting your filters.'
              : 'Generate your first compliance artifact for this client.'}
          </p>
          {typeFilter === 'all' && statusFilter === 'all' && (
            <Link
              href={`/dashboard/clients/${clientId}/artifacts/new`}
              className="mt-4 inline-block"
            >
              <Button>
                <Plus className="mr-2 h-4 w-4" />
                Generate Artifact
              </Button>
            </Link>
          )}
        </div>
      )}

      {/* Artifacts Grid */}
      {!isLoading && artifacts.length > 0 && (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {artifacts.map((artifact) => (
            <ArtifactCard
              key={artifact.id}
              artifact={artifact}
              clientId={clientId!}
            />
          ))}
        </div>
      )}
    </div>
  )
}
