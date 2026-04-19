'use client'

import Link from 'next/link'
import {
  FileText,
  FileCheck,
  FileWarning,
  FileSearch,
  Scale,
  Brain,
  Calendar,
  Clock,
  Quote,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import type { Artifact, ArtifactType, ArtifactStatus } from '@/lib/types/database'

interface ArtifactCardProps {
  artifact: Artifact
  clientId: string
}

const artifactTypeConfig: Record<
  ArtifactType,
  {
    label: string
    description: string
    icon: React.ComponentType<{ className?: string }>
    color: string
  }
> = {
  ropa: {
    label: 'RoPA',
    description: 'Record of Processing Activities',
    icon: FileText,
    color: 'text-blue-600 bg-blue-100',
  },
  dpia_screening: {
    label: 'DPIA Screening',
    description: 'Data Protection Impact Assessment Pre-check',
    icon: FileSearch,
    color: 'text-purple-600 bg-purple-100',
  },
  dpa_gap: {
    label: 'DPA Gap Analysis',
    description: 'Data Processing Agreement Review',
    icon: FileWarning,
    color: 'text-orange-600 bg-orange-100',
  },
  lawful_basis: {
    label: 'Lawful Basis',
    description: 'Lawful Basis Assessment',
    icon: Scale,
    color: 'text-green-600 bg-green-100',
  },
  ai_act_classification: {
    label: 'AI Act Classification',
    description: 'EU AI Act Risk Classification',
    icon: Brain,
    color: 'text-pink-600 bg-pink-100',
  },
}

const statusConfig: Record<
  ArtifactStatus,
  {
    label: string
    variant: 'default' | 'secondary' | 'outline' | 'destructive'
  }
> = {
  draft: { label: 'Draft', variant: 'secondary' },
  reviewed: { label: 'Reviewed', variant: 'outline' },
  approved: { label: 'Approved', variant: 'default' },
  exported: { label: 'Exported', variant: 'default' },
}

function formatDate(dateString: string): string {
  const date = new Date(dateString)
  return date.toLocaleDateString('en-GB', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  })
}

function formatTime(dateString: string): string {
  const date = new Date(dateString)
  return date.toLocaleTimeString('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function ArtifactCard({ artifact, clientId }: ArtifactCardProps) {
  const typeConfig = artifactTypeConfig[artifact.type]
  const status = statusConfig[artifact.status]
  const Icon = typeConfig.icon

  return (
    <Link href={`/dashboard/clients/${clientId}/artifacts/${artifact.id}`}>
      <Card className="transition-shadow hover:shadow-md cursor-pointer">
        <CardHeader className="pb-2">
          <div className="flex items-start justify-between">
            <div className="flex items-center gap-3">
              <div
                className={`flex h-10 w-10 items-center justify-center rounded-lg ${typeConfig.color}`}
              >
                <Icon className="h-5 w-5" />
              </div>
              <div>
                <CardTitle className="text-base">
                  {artifact.title || typeConfig.label}
                </CardTitle>
                <span className="text-xs text-muted-foreground">
                  {typeConfig.description}
                </span>
              </div>
            </div>
            <Badge variant={status.variant}>{status.label}</Badge>
          </div>
        </CardHeader>
        <CardContent>
          {artifact.input_context && (
            <p className="mb-3 text-sm text-muted-foreground line-clamp-2">
              {artifact.input_context}
            </p>
          )}

          <div className="flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
            <span className="flex items-center gap-1">
              <Calendar className="h-3 w-3" />
              {formatDate(artifact.created_at)}
            </span>
            <span className="flex items-center gap-1">
              <Clock className="h-3 w-3" />
              {formatTime(artifact.created_at)}
            </span>
            {artifact.citations && artifact.citations.length > 0 && (
              <span className="flex items-center gap-1">
                <Quote className="h-3 w-3" />
                {artifact.citations.length} citation
                {artifact.citations.length !== 1 ? 's' : ''}
              </span>
            )}
          </div>

          {artifact.generation_meta && (
            <div className="mt-3 border-t pt-3 flex items-center gap-2 text-xs text-muted-foreground">
              <span>Generated with {artifact.generation_meta.model}</span>
              <span>|</span>
              <span>{artifact.generation_meta.latency_ms}ms</span>
            </div>
          )}

          {artifact.version > 1 && (
            <div className="mt-2">
              <Badge variant="outline" className="text-xs">
                v{artifact.version}
              </Badge>
            </div>
          )}
        </CardContent>
      </Card>
    </Link>
  )
}
