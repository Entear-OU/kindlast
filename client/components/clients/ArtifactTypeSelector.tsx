'use client'

import { Check } from 'lucide-react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type { ArtifactType } from '@/lib/types/database'

interface ArtifactTypeSelectorProps {
  selectedType: ArtifactType | null
  onSelect: (type: ArtifactType) => void
  disabled?: boolean
}

interface ArtifactTypeOption {
  type: ArtifactType
  label: string
  description: string
  details: string[]
  accentColor: string
  gdprArticle?: string
}

const artifactTypes: ArtifactTypeOption[] = [
  {
    type: 'ropa',
    label: 'Record of Processing Activities',
    description: 'Generate a comprehensive RoPA documenting all data processing activities.',
    details: [
      'Processing purposes and lawful basis',
      'Data categories and subjects',
      'Recipients and transfers',
      'Retention periods',
      'Security measures',
    ],
    accentColor: '#4A6FA5',
    gdprArticle: 'Article 30',
  },
  {
    type: 'dpia_screening',
    label: 'DPIA Screening',
    description: 'Pre-assessment to determine if a full DPIA is required.',
    details: [
      'EDPB 9 criteria evaluation',
      'Risk level assessment',
      'Processing activity analysis',
      'Recommendations',
    ],
    accentColor: '#7B6B8D',
    gdprArticle: 'Article 35',
  },
  {
    type: 'dpa_gap',
    label: 'DPA Gap Analysis',
    description: 'Identify missing Data Processing Agreements with vendors.',
    details: [
      'Processor inventory',
      'DPA status tracking',
      'Transfer mechanism analysis',
      'Action items',
    ],
    accentColor: '#B5835A',
    gdprArticle: 'Article 28',
  },
  {
    type: 'lawful_basis',
    label: 'Lawful Basis Assessment',
    description: 'Document the lawful basis for each processing activity.',
    details: [
      'Six lawful bases analysis',
      'Legitimate interest assessment',
      'Consent requirements',
      'Legal citations',
    ],
    accentColor: '#5A8F7B',
    gdprArticle: 'Article 6',
  },
  {
    type: 'ai_act_classification',
    label: 'AI Act Classification',
    description: 'Classify AI systems under the EU AI Act risk framework.',
    details: [
      'Risk category determination',
      'Annex reference mapping',
      'Compliance obligations',
      'Timeline requirements',
    ],
    accentColor: '#8B6B7B',
  },
]

export function ArtifactTypeSelector({
  selectedType,
  onSelect,
  disabled = false,
}: ArtifactTypeSelectorProps) {
  return (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      {artifactTypes.map((option) => {
        const isSelected = selectedType === option.type

        return (
          <Card
            key={option.type}
            className={cn(
              'border border-[#EAEAEA] rounded-[10px] shadow-none bg-[#FAFAFA] transition-all duration-200 ease-out cursor-pointer',
              isSelected && 'border-[#111111] bg-white',
              disabled && 'opacity-50 cursor-not-allowed',
              !disabled && !isSelected && 'hover:translate-y-[-1px] hover:opacity-95'
            )}
            onClick={() => !disabled && onSelect(option.type)}
          >
            <CardHeader className="pb-3">
              <div className="flex items-start justify-between">
                <div
                  className="w-1 h-8 rounded-full"
                  style={{ backgroundColor: option.accentColor }}
                />
                <div className="flex items-center gap-2">
                  {option.gdprArticle && (
                    <span className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium text-[#555555] bg-white border border-[#EAEAEA] tracking-[0.05em] uppercase">
                      {option.gdprArticle}
                    </span>
                  )}
                  {isSelected && (
                    <div className="flex h-5 w-5 items-center justify-center rounded-full bg-[#111111]">
                      <Check className="h-3 w-3 text-white" />
                    </div>
                  )}
                </div>
              </div>
              <CardTitle className="text-[15px] font-medium text-[#111111] leading-relaxed tracking-[-0.01em] mt-3">
                {option.label}
              </CardTitle>
              <CardDescription className="text-[13px] text-[#666666] leading-[1.6]">
                {option.description}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="space-y-2">
                {option.details.map((detail, index) => (
                  <li
                    key={index}
                    className="flex items-center gap-2.5 text-[12px] text-[#888888] leading-relaxed"
                  >
                    <span className="h-1 w-1 rounded-full bg-[#CCCCCC] flex-shrink-0" />
                    {detail}
                  </li>
                ))}
              </ul>
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}
