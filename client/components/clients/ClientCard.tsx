'use client'

import Link from 'next/link'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
      <Card className="border border-[#EAEAEA] rounded-[10px] shadow-none bg-[#FAFAFA] transition-all duration-200 ease-out hover:translate-y-[-1px] hover:opacity-95 cursor-pointer">
        <CardHeader className="pb-3">
          <div className="flex items-start justify-between">
            <div>
              <CardTitle className="text-[15px] font-medium text-[#111111] leading-relaxed tracking-[-0.01em]">
                {client.name}
              </CardTitle>
              {sectorLabel && (
                <span className="text-[11px] text-[#666666] tracking-wide">
                  {sectorLabel}
                </span>
              )}
            </div>
            <span
              className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-[10px] font-medium uppercase tracking-[0.08em] ${
                client.status === 'active'
                  ? 'bg-[#F0F7F4] text-[#2D6A4F] border border-[#D4E9DF]'
                  : 'bg-[#F5F5F5] text-[#666666] border border-[#EAEAEA]'
              }`}
            >
              {client.status}
            </span>
          </div>
        </CardHeader>
        <CardContent>
          {client.description && (
            <p className="mb-4 text-[13px] text-[#444444] leading-[1.6] line-clamp-2">
              {client.description}
            </p>
          )}
          <div className="flex flex-wrap items-center gap-4 text-[11px] text-[#888888]">
            {client.country && (
              <span>{client.country}</span>
            )}
            {client.employee_count && (
              <span>{client.employee_count} employees</span>
            )}
            <span>Added {formatDate(client.created_at)}</span>
          </div>
          {client.tech_stack && client.tech_stack.length > 0 && (
            <div className="mt-4 flex flex-wrap gap-1.5">
              {client.tech_stack.slice(0, 4).map((tool) => (
                <span
                  key={tool}
                  className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium text-[#555555] bg-white border border-[#EAEAEA] tracking-wide"
                >
                  {tool}
                </span>
              ))}
              {client.tech_stack.length > 4 && (
                <span className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium text-[#888888] bg-white border border-[#EAEAEA] tracking-wide">
                  +{client.tech_stack.length - 4}
                </span>
              )}
            </div>
          )}
          {artifactCount > 0 && (
            <div className="mt-4 pt-4 border-t border-[#EAEAEA]">
              <span className="text-[11px] text-[#888888] tracking-wide">
                {artifactCount} artifact{artifactCount !== 1 ? 's' : ''} generated
              </span>
            </div>
          )}
        </CardContent>
      </Card>
    </Link>
  )
}
