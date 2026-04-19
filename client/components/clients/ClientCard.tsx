'use client'

import Link from 'next/link'
import { Building2, MapPin, Users, Calendar, MoreVertical } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import type { Client } from '@/lib/types/database'

interface ClientCardProps {
  client: Client
  artifactCount?: number
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
    month: 'short',
    year: 'numeric',
  })
}

export function ClientCard({ client, artifactCount = 0 }: ClientCardProps) {
  const sectorLabel = client.sector
    ? sectorLabels[client.sector] || client.sector
    : null

  return (
    <Link href={`/dashboard/clients/${client.id}`}>
      <Card className="transition-shadow hover:shadow-md cursor-pointer">
        <CardHeader className="pb-2">
          <div className="flex items-start justify-between">
            <div className="flex items-center gap-2">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10">
                <Building2 className="h-5 w-5 text-primary" />
              </div>
              <div>
                <CardTitle className="text-base">{client.name}</CardTitle>
                {sectorLabel && (
                  <span className="text-xs text-muted-foreground">
                    {sectorLabel}
                  </span>
                )}
              </div>
            </div>
            <Badge
              variant={client.status === 'active' ? 'default' : 'secondary'}
            >
              {client.status}
            </Badge>
          </div>
        </CardHeader>
        <CardContent>
          {client.description && (
            <p className="mb-3 text-sm text-muted-foreground line-clamp-2">
              {client.description}
            </p>
          )}
          <div className="flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
            {client.country && (
              <span className="flex items-center gap-1">
                <MapPin className="h-3 w-3" />
                {client.country}
              </span>
            )}
            {client.employee_count && (
              <span className="flex items-center gap-1">
                <Users className="h-3 w-3" />
                {client.employee_count} employees
              </span>
            )}
            <span className="flex items-center gap-1">
              <Calendar className="h-3 w-3" />
              Added {formatDate(client.created_at)}
            </span>
          </div>
          {client.tech_stack && client.tech_stack.length > 0 && (
            <div className="mt-3 flex flex-wrap gap-1">
              {client.tech_stack.slice(0, 4).map((tool) => (
                <Badge key={tool} variant="outline" className="text-xs">
                  {tool}
                </Badge>
              ))}
              {client.tech_stack.length > 4 && (
                <Badge variant="outline" className="text-xs">
                  +{client.tech_stack.length - 4} more
                </Badge>
              )}
            </div>
          )}
          {artifactCount > 0 && (
            <div className="mt-3 border-t pt-3">
              <span className="text-xs text-muted-foreground">
                {artifactCount} artifact{artifactCount !== 1 ? 's' : ''} generated
              </span>
            </div>
          )}
        </CardContent>
      </Card>
    </Link>
  )
}
