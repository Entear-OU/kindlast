'use client'

import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'

export interface StepCompanyData {
  company_name: string
  country: string
  industry: string
  employee_count: number | undefined
}

interface StepCompanyProps {
  data: StepCompanyData
  onChange: (data: StepCompanyData) => void
}

export function StepCompany({ data, onChange }: StepCompanyProps) {
  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Company Basics</h2>
        <p className="text-sm text-muted-foreground">
          Tell us about your company so we can tailor the assessment.
        </p>
      </div>

      <div className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="company_name">Company Name</Label>
          <Input
            id="company_name"
            value={data.company_name}
            onChange={(e) => onChange({ ...data, company_name: e.target.value })}
            placeholder="Acme Corp"
            required
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="country">Country</Label>
          <Input
            id="country"
            value={data.country}
            onChange={(e) => onChange({ ...data, country: e.target.value })}
            placeholder="Estonia"
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="industry">Industry</Label>
          <Input
            id="industry"
            value={data.industry}
            onChange={(e) => onChange({ ...data, industry: e.target.value })}
            placeholder="Technology, Healthcare, Finance..."
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="employee_count">Employee Count</Label>
          <Input
            id="employee_count"
            type="number"
            value={data.employee_count ?? ''}
            onChange={(e) =>
              onChange({
                ...data,
                employee_count: e.target.value ? parseInt(e.target.value, 10) : undefined,
              })
            }
            placeholder="10"
            min={1}
          />
        </div>
      </div>
    </div>
  )
}
