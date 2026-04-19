'use client'

import Link from 'next/link'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
    accentColor: string
  }
> = {
  ropa: {
    label: 'RoPA',
    description: 'Record of Processing Activities',
    accentColor: '#4A6FA5',
  },
  dpia_screening: {
    label: 'DPIA Screening',
    description: 'Data Protection Impact Assessment Pre-check',
    accentColor: '#7B6B8D',
  },
  dpa_gap: {
    label: 'DPA Gap Analysis',
    description: 'Data Processing Agreement Review',
    accentColor: '#B5835A',
  },
  lawful_basis: {
    label: 'Lawful Basis',
    description: 'Lawful Basis Assessment',
    accentColor: '#5A8F7B',
  },
  ai_act_classification: {
    label: 'AI Act Classification',
    description: 'EU AI Act Risk Classification',
    accentColor: '#8B6B7B',
  },
}

const statusConfig: Record<
  ArtifactStatus,
  {
    label: string
    bgColor: string
    textColor: string
    borderColor: string
  }
> = {
  draft: {
    label: 'DRAFT',
    bgColor: '#F5F5F5',
    textColor: '#666666',
    borderColor: '#EAEAEA',
  },
  reviewed: {
    label: 'REVIEWED',
    bgColor: '#F5F5F0',
    textColor: '#7A7A5A',
    borderColor: '#E5E5D8',
  },
  approved: {
    label: 'APPROVED',
    bgColor: '#F0F7F4',
    textColor: '#2D6A4F',
    borderColor: '#D4E9DF',
  },
  exported: {
    label: 'EXPORTED',
    bgColor: '#F0F4F7',
    textColor: '#4A6FA5',
    borderColor: '#D4E1E9',
  },
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

  return (
    <Link href={`/dashboard/clients/${clientId}/artifacts/${artifact.id}`}>
      <Card className="border border-[#EAEAEA] rounded-[10px] shadow-none bg-[#FAFAFA] transition-all duration-200 ease-out hover:translate-y-[-1px] hover:opacity-95 cursor-pointer">
        <CardHeader className="pb-3">
          <div className="flex items-start justify-between">
            <div className="flex items-start gap-3">
              <div
                className="w-1 h-10 rounded-full"
                style={{ backgroundColor: typeConfig.accentColor }}
              />
              <div>
                <CardTitle className="text-[15px] font-medium text-[#111111] leading-relaxed tracking-[-0.01em]">
                  {artifact.title || typeConfig.label}
                </CardTitle>
                <span className="text-[11px] text-[#666666] tracking-wide">
                  {typeConfig.description}
                </span>
              </div>
            </div>
            <span
              className="inline-flex items-center px-2.5 py-0.5 rounded-full text-[10px] font-medium uppercase tracking-[0.08em]"
              style={{
                backgroundColor: status.bgColor,
                color: status.textColor,
                border: `1px solid ${status.borderColor}`,
              }}
            >
              {status.label}
            </span>
          </div>
        </CardHeader>
        <CardContent>
          {artifact.input_context && (
            <p className="mb-4 text-[13px] text-[#444444] leading-[1.6] line-clamp-2">
              {artifact.input_context}
            </p>
          )}

          <div className="flex flex-wrap items-center gap-4 text-[11px] text-[#888888]">
            <span>{formatDate(artifact.created_at)}</span>
            <span>{formatTime(artifact.created_at)}</span>
            {artifact.citations && artifact.citations.length > 0 && (
              <span>
                {artifact.citations.length} citation
                {artifact.citations.length !== 1 ? 's' : ''}
              </span>
            )}
          </div>

          {artifact.generation_meta && (
            <div className="mt-4 pt-4 border-t border-[#EAEAEA] flex items-center gap-3 text-[11px] text-[#888888]">
              <span>{artifact.generation_meta.model}</span>
              <span className="text-[#CCCCCC]">|</span>
              <span>{artifact.generation_meta.latency_ms}ms</span>
            </div>
          )}

          {artifact.version > 1 && (
            <div className="mt-3">
              <span className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium text-[#555555] bg-white border border-[#EAEAEA] tracking-wide uppercase">
                v{artifact.version}
              </span>
            </div>
          )}
        </CardContent>
      </Card>
    </Link>
  )
}
