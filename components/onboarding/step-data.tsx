'use client'

import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'

export interface StepDataData {
  processes_personal_data: boolean
  data_types: string[]
  third_party_processors: string[]
  transfers_data_outside_eu: boolean
}

const commonDataTypes = [
  'Email addresses',
  'Payment information',
  'Health data',
  'Biometric data',
  'Location data',
  'IP addresses',
  'Names and contact info',
  'Behavioral/analytics data',
]

interface StepDataProps {
  data: StepDataData
  onChange: (data: StepDataData) => void
}

export function StepData({ data, onChange }: StepDataProps) {
  const toggleDataType = (type: string) => {
    const types = data.data_types.includes(type)
      ? data.data_types.filter((t) => t !== type)
      : [...data.data_types, type]
    onChange({ ...data, data_types: types })
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Data Processing</h2>
        <p className="text-sm text-muted-foreground">
          Help us understand what personal data your company processes.
        </p>
      </div>

      <div className="space-y-4">
        <div className="flex items-center gap-3">
          <input
            type="checkbox"
            id="processes_personal_data"
            checked={data.processes_personal_data}
            onChange={(e) =>
              onChange({ ...data, processes_personal_data: e.target.checked })
            }
            className="h-4 w-4 rounded border-gray-300"
          />
          <Label htmlFor="processes_personal_data">
            We collect and process personal data
          </Label>
        </div>

        {data.processes_personal_data && (
          <div className="space-y-2">
            <Label>What types of personal data do you collect?</Label>
            <div className="grid grid-cols-2 gap-2">
              {commonDataTypes.map((type) => (
                <label key={type} className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={data.data_types.includes(type)}
                    onChange={() => toggleDataType(type)}
                    className="h-4 w-4 rounded border-gray-300"
                  />
                  {type}
                </label>
              ))}
            </div>
          </div>
        )}

        <div className="space-y-2">
          <Label htmlFor="third_party_processors">
            Third-party processors (comma-separated)
          </Label>
          <Input
            id="third_party_processors"
            value={data.third_party_processors.join(', ')}
            onChange={(e) =>
              onChange({
                ...data,
                third_party_processors: e.target.value
                  .split(',')
                  .map((s) => s.trim())
                  .filter(Boolean),
              })
            }
            placeholder="Stripe, Google Analytics, Mailchimp"
          />
        </div>

        <div className="flex items-center gap-3">
          <input
            type="checkbox"
            id="transfers_data_outside_eu"
            checked={data.transfers_data_outside_eu}
            onChange={(e) =>
              onChange({ ...data, transfers_data_outside_eu: e.target.checked })
            }
            className="h-4 w-4 rounded border-gray-300"
          />
          <Label htmlFor="transfers_data_outside_eu">
            We transfer data outside the EU
          </Label>
        </div>
      </div>
    </div>
  )
}
