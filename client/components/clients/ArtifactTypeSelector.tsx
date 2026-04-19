'use client'

import {
  FileText,
  FileSearch,
  FileWarning,
  Scale,
  Brain,
  Check,
} from 'lucide-react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
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
  icon: React.ComponentType<{ className?: string }>
  color: string
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
    icon: FileText,
    color: 'border-blue-500 bg-blue-50 text-blue-700',
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
    icon: FileSearch,
    color: 'border-purple-500 bg-purple-50 text-purple-700',
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
    icon: FileWarning,
    color: 'border-orange-500 bg-orange-50 text-orange-700',
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
    icon: Scale,
    color: 'border-green-500 bg-green-50 text-green-700',
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
    icon: Brain,
    color: 'border-pink-500 bg-pink-50 text-pink-700',
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
        const Icon = option.icon
        const isSelected = selectedType === option.type

        return (
          <Card
            key={option.type}
            className={cn(
              'cursor-pointer transition-all',
              isSelected && `ring-2 ring-offset-2 ${option.color.split(' ')[0].replace('border', 'ring')}`,
              disabled && 'opacity-50 cursor-not-allowed',
              !disabled && 'hover:shadow-md'
            )}
            onClick={() => !disabled && onSelect(option.type)}
          >
            <CardHeader className="pb-2">
              <div className="flex items-start justify-between">
                <div
                  className={cn(
                    'flex h-10 w-10 items-center justify-center rounded-lg',
                    option.color.split(' ').slice(1).join(' ')
                  )}
                >
                  <Icon className="h-5 w-5" />
                </div>
                <div className="flex items-center gap-2">
                  {option.gdprArticle && (
                    <Badge variant="outline" className="text-xs">
                      {option.gdprArticle}
                    </Badge>
                  )}
                  {isSelected && (
                    <div className="flex h-6 w-6 items-center justify-center rounded-full bg-primary text-primary-foreground">
                      <Check className="h-4 w-4" />
                    </div>
                  )}
                </div>
              </div>
              <CardTitle className="text-base mt-2">{option.label}</CardTitle>
              <CardDescription className="text-sm">
                {option.description}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="space-y-1">
                {option.details.map((detail, index) => (
                  <li
                    key={index}
                    className="flex items-center gap-2 text-xs text-muted-foreground"
                  >
                    <span className="h-1 w-1 rounded-full bg-muted-foreground" />
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
