'use client'

import { useState, useEffect } from 'react'
import Link from 'next/link'
import { Plus, Building2, Search, Filter } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ClientCard } from '@/components/clients'
import { Skeleton } from '@/components/ui/skeleton'
import { listClients } from '@/lib/api/clients'
import type { Client } from '@/lib/types/database'

function ClientCardSkeleton() {
  return (
    <div className="rounded-xl border border-border/40 bg-card p-6">
      <div className="flex items-start justify-between mb-6">
        <div className="flex items-center gap-4">
          <Skeleton className="h-12 w-12 rounded-lg" />
          <div className="space-y-2">
            <Skeleton className="h-5 w-36" />
            <Skeleton className="h-3 w-24" />
          </div>
        </div>
        <Skeleton className="h-5 w-16 rounded-full" />
      </div>
      <Skeleton className="h-10 w-full mb-4" />
      <div className="flex gap-6">
        <Skeleton className="h-3 w-20" />
        <Skeleton className="h-3 w-24" />
        <Skeleton className="h-3 w-28" />
      </div>
    </div>
  )
}

function EmptyState({ hasFilters }: { hasFilters: boolean }) {
  return (
    <div className="flex flex-col items-center justify-center py-24 px-6">
      <div className="relative mb-8">
        {/* Decorative background circles */}
        <div className="absolute -inset-4 rounded-full bg-muted/50" />
        <div className="absolute -inset-8 rounded-full bg-muted/30" />
        <div className="relative flex h-20 w-20 items-center justify-center rounded-full bg-muted">
          <Building2 className="h-10 w-10 text-muted-foreground/60" strokeWidth={1.5} />
        </div>
      </div>
      <h2 className="text-xl font-medium text-[#111111] tracking-tight">
        {hasFilters ? 'No clients found' : 'No clients yet'}
      </h2>
      <p className="mt-3 text-muted-foreground max-w-sm text-center leading-relaxed">
        {hasFilters
          ? 'Try adjusting your search or filters to find what you are looking for.'
          : 'Get started by adding your first client. You will be able to generate compliance artifacts for them.'}
      </p>
      {!hasFilters && (
        <Link href="/dashboard/clients/new" className="mt-8">
          <Button size="lg" className="gap-2">
            <Plus className="h-4 w-4" />
            Add Your First Client
          </Button>
        </Link>
      )}
    </div>
  )
}

export default function ClientsPage() {
  const [clients, setClients] = useState<Client[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState<'all' | 'active' | 'archived'>('active')

  useEffect(() => {
    async function fetchClients() {
      try {
        setIsLoading(true)
        const response = await listClients({
          status: statusFilter === 'all' ? undefined : statusFilter,
        })
        setClients(response.clients)
        setError(null)
      } catch (err) {
        setError('Failed to load clients. Please try again.')
        console.error('Error fetching clients:', err)
      } finally {
        setIsLoading(false)
      }
    }

    fetchClients()
  }, [statusFilter])

  const filteredClients = clients.filter((client) =>
    client.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
    client.description?.toLowerCase().includes(searchQuery.toLowerCase()) ||
    client.sector?.toLowerCase().includes(searchQuery.toLowerCase())
  )

  return (
    <div className="min-h-screen">
      {/* Page Header */}
      <header className="px-6 pt-12 pb-8 lg:px-12 lg:pt-16">
        <div className="max-w-7xl mx-auto">
          <div className="grid grid-cols-1 lg:grid-cols-[1fr,auto] gap-6 items-end">
            <div>
              <h1 className="text-4xl font-semibold tracking-tighter text-[#111111]">
                Clients
              </h1>
              <p className="mt-2 text-muted-foreground text-lg">
                Manage your client organizations and their compliance artifacts.
              </p>
            </div>
            <Link href="/dashboard/clients/new">
              <Button size="lg" className="gap-2 whitespace-nowrap">
                <Plus className="h-4 w-4" />
                Add Client
              </Button>
            </Link>
          </div>
        </div>
      </header>

      {/* Filters */}
      <div className="px-6 pb-8 lg:px-12">
        <div className="max-w-7xl mx-auto">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
            <div className="relative flex-1 max-w-md">
              <Search className="absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                placeholder="Search clients..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-11 h-11 bg-card border-border/40"
              />
            </div>
            <Select
              value={statusFilter}
              onValueChange={(value: string | null) => value && setStatusFilter(value as 'all' | 'active' | 'archived')}
            >
              <SelectTrigger className="w-[160px] h-11 bg-card border-border/40">
                <Filter className="mr-2 h-4 w-4 text-muted-foreground" />
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Status</SelectItem>
                <SelectItem value="active">Active</SelectItem>
                <SelectItem value="archived">Archived</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </div>

      {/* Main Content */}
      <div className="px-6 pb-12 lg:px-12">
        <div className="max-w-7xl mx-auto">
          {/* Error State */}
          {error && (
            <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-6 text-center">
              <p className="text-destructive">{error}</p>
            </div>
          )}

          {/* Loading State */}
          {isLoading && (
            <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-3">
              {[...Array(6)].map((_, i) => (
                <ClientCardSkeleton key={i} />
              ))}
            </div>
          )}

          {/* Empty State */}
          {!isLoading && !error && filteredClients.length === 0 && (
            <EmptyState hasFilters={!!searchQuery || statusFilter !== 'active'} />
          )}

          {/* Client Grid */}
          {!isLoading && !error && filteredClients.length > 0 && (
            <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-3">
              {filteredClients.map((client) => (
                <ClientCard key={client.id} client={client} />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
