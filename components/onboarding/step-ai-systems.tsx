'use client'

import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'

export interface AiSystemDescription {
  name: string
  purpose: string
  dataUsed: string
  isAutomatedDecision: boolean
}

export interface StepAiSystemsData {
  uses_ai_systems: boolean
  ai_system_descriptions: AiSystemDescription[]
}

interface StepAiSystemsProps {
  data: StepAiSystemsData
  onChange: (data: StepAiSystemsData) => void
}

const emptySystem: AiSystemDescription = {
  name: '',
  purpose: '',
  dataUsed: '',
  isAutomatedDecision: false,
}

export function StepAiSystems({ data, onChange }: StepAiSystemsProps) {
  const addSystem = () => {
    onChange({
      ...data,
      ai_system_descriptions: [...data.ai_system_descriptions, { ...emptySystem }],
    })
  }

  const removeSystem = (index: number) => {
    onChange({
      ...data,
      ai_system_descriptions: data.ai_system_descriptions.filter((_, i) => i !== index),
    })
  }

  const updateSystem = (index: number, updates: Partial<AiSystemDescription>) => {
    const systems = [...data.ai_system_descriptions]
    systems[index] = { ...systems[index], ...updates }
    onChange({ ...data, ai_system_descriptions: systems })
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">AI Systems</h2>
        <p className="text-sm text-muted-foreground">
          Tell us about any AI systems your company uses. This is needed for EU AI Act compliance.
        </p>
      </div>

      <div className="space-y-4">
        <div className="flex items-center gap-3">
          <input
            type="checkbox"
            id="uses_ai_systems"
            checked={data.uses_ai_systems}
            onChange={(e) =>
              onChange({
                ...data,
                uses_ai_systems: e.target.checked,
                ai_system_descriptions: e.target.checked
                  ? data.ai_system_descriptions.length > 0
                    ? data.ai_system_descriptions
                    : [{ ...emptySystem }]
                  : [],
              })
            }
            className="h-4 w-4 rounded border-gray-300"
          />
          <Label htmlFor="uses_ai_systems">We use AI systems in our business</Label>
        </div>

        {data.uses_ai_systems && (
          <div className="space-y-6">
            {data.ai_system_descriptions.map((system, index) => (
              <div key={index} className="space-y-3 rounded-md border p-4">
                <div className="flex items-center justify-between">
                  <h3 className="text-sm font-medium">AI System {index + 1}</h3>
                  {data.ai_system_descriptions.length > 1 && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => removeSystem(index)}
                    >
                      Remove
                    </Button>
                  )}
                </div>

                <div className="space-y-2">
                  <Label htmlFor={`ai-name-${index}`}>System Name</Label>
                  <Input
                    id={`ai-name-${index}`}
                    value={system.name}
                    onChange={(e) => updateSystem(index, { name: e.target.value })}
                    placeholder="e.g., Customer chatbot"
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor={`ai-purpose-${index}`}>Purpose</Label>
                  <Textarea
                    id={`ai-purpose-${index}`}
                    value={system.purpose}
                    onChange={(e) => updateSystem(index, { purpose: e.target.value })}
                    placeholder="What does this AI system do?"
                    rows={2}
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor={`ai-data-${index}`}>Data Used</Label>
                  <Input
                    id={`ai-data-${index}`}
                    value={system.dataUsed}
                    onChange={(e) => updateSystem(index, { dataUsed: e.target.value })}
                    placeholder="What data does the system process?"
                  />
                </div>

                <div className="flex items-center gap-3">
                  <input
                    type="checkbox"
                    id={`ai-decision-${index}`}
                    checked={system.isAutomatedDecision}
                    onChange={(e) =>
                      updateSystem(index, { isAutomatedDecision: e.target.checked })
                    }
                    className="h-4 w-4 rounded border-gray-300"
                  />
                  <Label htmlFor={`ai-decision-${index}`}>
                    Makes automated decisions affecting individuals
                  </Label>
                </div>
              </div>
            ))}

            <Button type="button" variant="outline" onClick={addSystem}>
              + Add another AI system
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
