'use client'

import { Label } from '@/components/ui/label'

export interface StepComplianceData {
  has_privacy_policy: boolean
  has_cookie_consent: boolean
  has_dpo: boolean
  has_breach_notification: boolean
  has_dsr_process: boolean
}

interface StepComplianceProps {
  data: StepComplianceData
  onChange: (data: StepComplianceData) => void
}

const complianceItems: { key: keyof StepComplianceData; label: string; description: string }[] = [
  {
    key: 'has_privacy_policy',
    label: 'Privacy Policy',
    description: 'A published privacy policy on your website or app',
  },
  {
    key: 'has_cookie_consent',
    label: 'Cookie Consent',
    description: 'A cookie consent banner or mechanism for website visitors',
  },
  {
    key: 'has_dpo',
    label: 'Data Protection Officer',
    description: 'A designated Data Protection Officer (DPO)',
  },
  {
    key: 'has_breach_notification',
    label: 'Breach Notification Process',
    description: 'A process for notifying authorities and users of data breaches',
  },
  {
    key: 'has_dsr_process',
    label: 'Data Subject Request Process',
    description: 'A process for handling data access, deletion, and portability requests',
  },
]

export function StepCompliance({ data, onChange }: StepComplianceProps) {
  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Current Compliance Measures</h2>
        <p className="text-sm text-muted-foreground">
          Let us know which compliance measures you already have in place.
        </p>
      </div>

      <div className="space-y-4">
        {complianceItems.map((item) => (
          <div key={item.key} className="flex items-start gap-3">
            <input
              type="checkbox"
              id={item.key}
              checked={data[item.key]}
              onChange={(e) => onChange({ ...data, [item.key]: e.target.checked })}
              className="mt-1 h-4 w-4 rounded border-gray-300"
            />
            <div>
              <Label htmlFor={item.key}>{item.label}</Label>
              <p className="text-xs text-muted-foreground">{item.description}</p>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
