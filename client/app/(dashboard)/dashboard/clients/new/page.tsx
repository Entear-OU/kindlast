'use client'

import { useState } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { ArrowLeft } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ClientForm } from '@/components/clients'
import { createClient, isPlanLimitError } from '@/lib/api/clients'
import type { CreateClientRequest, UpdateClientRequest } from '@/lib/api/clients'

export default function NewClientPage() {
  const router = useRouter()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (data: CreateClientRequest | UpdateClientRequest) => {
    try {
      setIsSubmitting(true)
      setError(null)
      const client = await createClient(data as CreateClientRequest)
      router.push(`/dashboard/clients/${client.id}`)
    } catch (err) {
      if (isPlanLimitError(err)) {
        setError(
          'You have reached your client limit. Please upgrade your plan to add more clients.'
        )
      } else {
        setError('Failed to create client. Please try again.')
      }
      console.error('Error creating client:', err)
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="flex flex-col gap-6 p-6">
      <div>
        <Link
          href="/dashboard/clients"
          className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Clients
        </Link>
      </div>

      <div>
        <h1 className="text-2xl font-bold">Add New Client</h1>
        <p className="text-sm text-muted-foreground">
          Create a client profile to start generating compliance artifacts.
        </p>
      </div>

      {error && (
        <div className="rounded-lg border border-destructive/50 bg-destructive/10 p-4 text-sm text-destructive">
          {error}
        </div>
      )}

      <div className="rounded-lg border bg-card p-6">
        <ClientForm onSubmit={handleSubmit} isSubmitting={isSubmitting} />
      </div>
    </div>
  )
}
