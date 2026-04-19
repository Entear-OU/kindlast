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

function ArtifactCardSkeleton() {
  return (
    <div className="rounded-xl border border-border/40 bg-card p-6">
      <div className="flex items-start justify-between mb-6">
        <div className="flex items-center gap-4">
          <Skeleton className="h-12 w-12 rounded-lg" />
          <div className="space-y-2">
            <Skeleton className="h-5 w-28" />
            <Skeleton className="h-3 w-40" />
          </div>
        </div>
        <Skeleton className="h-5 w-16 rounded-full" />
      </div>
      <Skeleton className="h-10 w-full mb-4" />
      <div className="flex gap-6">
        <Skeleton className="h-3 w-24" />
        <Skeleton className="h-3 w-20" />
      </div>
    </div>
  )
}

function PageSkeleton() {
  return (
    <div className="min-h-screen">
      <div className="px-6 pt-8 lg:px-12">
        <Skeleton className="h-4 w-32 mb-8" />
      </div>
      <header className="px-6 pb-8 lg:px-12">
        <div className="max-w-7xl">
          <Skeleton className="h-10 w-48 mb-2" />
          <Skeleton className="h-5 w-72" />
        </div>
      </header>
      <div className="px-6 pb-12 lg:px-12">
        <div className="max-w-7xl grid gap-6 sm:grid-cols-2 xl:grid-cols-3">
          {[...Array(6)].map((_, i) => (
            <ArtifactCardSkeleton key={i} />
          ))}
        </div>
      </div>
    </div>
  )
}

function EmptyState({ hasFilters, clientId }: { hasFilters: boolean; clientId: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-24 px-6">
      <div className="relative mb-8">
        <div className="absolute -inset-4 rounded-full bg-muted/50" />
        <div className="absolute -inset-8 rounded-full bg-muted/30" />
        <div className="relative flex h-20 w-20 items-center justify-center rounded-full bg-muted">
          <FileText className="h-10 w-10 text-muted-foreground/60" strokeWidth={1.5} />
        </div>
      </div>
      <h2 className="text-xl font-medium text-[#111111] tracking-tight">
        {hasFilters ? 'No matching artifacts' : 'No artifacts yet'}
      </h2>
      <p className="mt-3 text-muted-foreground max-w-sm text-center leading-relaxed">
        {hasFilters
          ? 'Try adjusting your filters to find what you are looking for.'
          : 'Generate your first compliance artifact for this client.'}
      </p>
      {!hasFilters && (
        <Link href={`/dashboard/clients/${clientId}/artifacts/new`} className="mt-8">
          <Button size="lg" className="gap-2">
            <Plus className="h-4 w-4" />
            Generate Artifact
          </Button>
        </Link>
      )}
    </div>
  )
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

  const hasFilters = typeFilter !== 'all' || statusFilter !== 'all'

  return (
    <div className="min-h-screen">
      {/* Breadcrumb */}
      <div className="px-6 pt-8 lg:px-12">
        <div className="max-w-7xl">
          <Link
            href={`/dashboard/clients/${clientId}`}
            className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-[#111111] transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
            Back to {client.name}
          </Link>
        </div>
      </div>

      {/* Page Header */}
      <header className="px-6 pt-6 pb-8 lg:px-12">
        <div className="max-w-7xl">
          <div className="grid grid-cols-1 lg:grid-cols-[1fr,auto] gap-6 items-end">
            <div>
              <h1 className="text-4xl font-semibold tracking-tighter text-[#111111]">
                Artifacts
              </h1>
              <p className="mt-2 text-muted-foreground text-lg">
                Compliance documents generated for {client.name}.
              </p>
            </div>
            <Link href={`/dashboard/clients/${clientId}/artifacts/new`}>
              <Button size="lg" className="gap-2 whitespace-nowrap">
                <Plus className="h-4 w-4" />
                Generate Artifact
              </Button>
            </Link>
          </div>
        </div>
      </header>

      {/* Filters */}
      <div className="px-6 pb-8 lg:px-12">
        <div className="max-w-7xl">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
            <Select
              value={typeFilter}
              onValueChange={(value: string | null) => value && setTypeFilter(value as ArtifactType | 'all')}
            >
              <SelectTrigger className="w-[200px] h-11 bg-card border-border/40">
                <Filter className="mr-2 h-4 w-4 text-muted-foreground" />
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
              <SelectTrigger className="w-[160px] h-11 bg-card border-border/40">
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
        </div>
      </div>

      {/* Main Content */}
      <div className="px-6 pb-12 lg:px-12">
        <div className="max-w-7xl">
          {/* Loading State */}
          {isLoading && (
            <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-3">
              {[...Array(6)].map((_, i) => (
                <ArtifactCardSkeleton key={i} />
              ))}
            </div>
          )}

          {/* Empty State */}
          {!isLoading && artifacts.length === 0 && (
            <EmptyState hasFilters={hasFilters} clientId={clientId!} />
          )}

          {/* Artifacts Grid */}
          {!isLoading && artifacts.length > 0 && (
            <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-3">
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
      </div>
    </div>
  )
}
