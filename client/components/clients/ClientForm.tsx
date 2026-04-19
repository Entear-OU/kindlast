'use client'

import { useState } from 'react'
import { useRouter } from 'next/navigation'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { X, Plus, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Client } from '@/lib/types/database'
import type { CreateClientRequest, UpdateClientRequest } from '@/lib/api/clients'

interface ClientFormProps {
  client?: Client
  onSubmit: (data: CreateClientRequest | UpdateClientRequest) => Promise<void>
  isSubmitting?: boolean
}

// Section header component with uppercase styling
function SectionHeader({ title, description }: { title: string; description: string }) {
  return (
    <div className="space-y-1">
      <h3 className="text-xs font-medium uppercase tracking-[0.1em] text-foreground/80">
        {title}
      </h3>
      <p className="text-sm text-muted-foreground">
        {description}
      </p>
    </div>
  )
}

// Pill chip component for tech stack and selectable items
function Chip({
  children,
  selected = false,
  removable = false,
  onClick,
  onRemove,
  className,
}: {
  children: React.ReactNode
  selected?: boolean
  removable?: boolean
  onClick?: () => void
  onRemove?: () => void
  className?: string
}) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-sm font-medium transition-all duration-200 ease-out',
        'border border-[#EAEAEA] dark:border-white/10',
        selected
          ? 'bg-[#111111] text-white border-[#111111] dark:bg-white dark:text-[#111111] dark:border-white'
          : 'bg-transparent text-foreground/80 hover:bg-muted/50 hover:border-[#DADADA]',
        onClick && 'cursor-pointer',
        className
      )}
      onClick={onClick}
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
      onKeyDown={onClick ? (e) => e.key === 'Enter' && onClick() : undefined}
    >
      {children}
      {removable && onRemove && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onRemove()
          }}
          className="ml-0.5 rounded-full p-0.5 transition-colors duration-150 hover:bg-white/20 dark:hover:bg-black/20"
          aria-label="Remove"
        >
          <X className="h-3 w-3" />
        </button>
      )}
    </span>
  )
}

// Form field wrapper for consistent styling
function FormField({
  children,
  label,
  htmlFor,
  required,
  hint,
  error,
}: {
  children: React.ReactNode
  label: string
  htmlFor: string
  required?: boolean
  hint?: string
  error?: string
}) {
  return (
    <div className="space-y-2">
      <Label
        htmlFor={htmlFor}
        className="text-sm font-medium text-foreground/90"
      >
        {label}
        {required && <span className="ml-0.5 text-muted-foreground">*</span>}
      </Label>
      {children}
      {hint && !error && (
        <p className="text-xs text-muted-foreground">{hint}</p>
      )}
      {error && (
        <p className="text-xs text-destructive animate-in fade-in slide-in-from-top-1 duration-200">
          {error}
        </p>
      )}
    </div>
  )
}

const sectors = [
  { value: 'fintech', label: 'Fintech' },
  { value: 'healthtech', label: 'Healthtech' },
  { value: 'saas', label: 'SaaS' },
  { value: 'ecommerce', label: 'E-commerce' },
  { value: 'edtech', label: 'EdTech' },
  { value: 'legaltech', label: 'LegalTech' },
  { value: 'proptech', label: 'PropTech' },
  { value: 'insurtech', label: 'InsurTech' },
  { value: 'manufacturing', label: 'Manufacturing' },
  { value: 'retail', label: 'Retail' },
  { value: 'professional_services', label: 'Professional Services' },
  { value: 'non_profit', label: 'Non-profit' },
  { value: 'government', label: 'Government' },
  { value: 'other', label: 'Other' },
]

const countries = [
  { value: 'AT', label: 'Austria' },
  { value: 'BE', label: 'Belgium' },
  { value: 'BG', label: 'Bulgaria' },
  { value: 'HR', label: 'Croatia' },
  { value: 'CY', label: 'Cyprus' },
  { value: 'CZ', label: 'Czech Republic' },
  { value: 'DK', label: 'Denmark' },
  { value: 'EE', label: 'Estonia' },
  { value: 'FI', label: 'Finland' },
  { value: 'FR', label: 'France' },
  { value: 'DE', label: 'Germany' },
  { value: 'GR', label: 'Greece' },
  { value: 'HU', label: 'Hungary' },
  { value: 'IE', label: 'Ireland' },
  { value: 'IT', label: 'Italy' },
  { value: 'LV', label: 'Latvia' },
  { value: 'LT', label: 'Lithuania' },
  { value: 'LU', label: 'Luxembourg' },
  { value: 'MT', label: 'Malta' },
  { value: 'NL', label: 'Netherlands' },
  { value: 'PL', label: 'Poland' },
  { value: 'PT', label: 'Portugal' },
  { value: 'RO', label: 'Romania' },
  { value: 'SK', label: 'Slovakia' },
  { value: 'SI', label: 'Slovenia' },
  { value: 'ES', label: 'Spain' },
  { value: 'SE', label: 'Sweden' },
  { value: 'GB', label: 'United Kingdom' },
  { value: 'NO', label: 'Norway' },
  { value: 'CH', label: 'Switzerland' },
  { value: 'IS', label: 'Iceland' },
  { value: 'LI', label: 'Liechtenstein' },
]

const commonDataSubjects = [
  'Customers',
  'Employees',
  'Website Visitors',
  'Contractors',
  'Job Applicants',
  'Business Contacts',
  'Newsletter Subscribers',
  'App Users',
]

const commonPurposes = [
  'Customer Relationship Management',
  'Email Marketing',
  'Payment Processing',
  'HR Management',
  'Analytics',
  'Customer Support',
  'Fraud Prevention',
  'Legal Compliance',
]

export function ClientForm({ client, onSubmit, isSubmitting = false }: ClientFormProps) {
  const router = useRouter()
  const [formData, setFormData] = useState({
    name: client?.name ?? '',
    description: client?.description ?? '',
    sector: client?.sector ?? '',
    country: client?.country ?? '',
    employee_count: client?.employee_count ?? undefined,
    tech_stack: client?.tech_stack ?? [],
    data_subjects: client?.data_subjects ?? [],
    processing_purposes: client?.processing_purposes ?? [],
  })

  const [newTechItem, setNewTechItem] = useState('')
  const [newDataSubject, setNewDataSubject] = useState('')
  const [newPurpose, setNewPurpose] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    const data: CreateClientRequest | UpdateClientRequest = {
      name: formData.name,
      description: formData.description || undefined,
      sector: formData.sector || undefined,
      country: formData.country || undefined,
      employee_count: formData.employee_count,
      tech_stack: formData.tech_stack.length > 0 ? formData.tech_stack : undefined,
      data_subjects: formData.data_subjects.length > 0 ? formData.data_subjects : undefined,
      processing_purposes: formData.processing_purposes.length > 0 ? formData.processing_purposes : undefined,
    }

    await onSubmit(data)
  }

  const addTechItem = () => {
    if (newTechItem.trim() && !formData.tech_stack.includes(newTechItem.trim())) {
      setFormData({
        ...formData,
        tech_stack: [...formData.tech_stack, newTechItem.trim()],
      })
      setNewTechItem('')
    }
  }

  const removeTechItem = (item: string) => {
    setFormData({
      ...formData,
      tech_stack: formData.tech_stack.filter((t) => t !== item),
    })
  }

  const toggleDataSubject = (subject: string) => {
    if (formData.data_subjects.includes(subject)) {
      setFormData({
        ...formData,
        data_subjects: formData.data_subjects.filter((s) => s !== subject),
      })
    } else {
      setFormData({
        ...formData,
        data_subjects: [...formData.data_subjects, subject],
      })
    }
  }

  const addCustomDataSubject = () => {
    if (newDataSubject.trim() && !formData.data_subjects.includes(newDataSubject.trim())) {
      setFormData({
        ...formData,
        data_subjects: [...formData.data_subjects, newDataSubject.trim()],
      })
      setNewDataSubject('')
    }
  }

  const togglePurpose = (purpose: string) => {
    if (formData.processing_purposes.includes(purpose)) {
      setFormData({
        ...formData,
        processing_purposes: formData.processing_purposes.filter((p) => p !== purpose),
      })
    } else {
      setFormData({
        ...formData,
        processing_purposes: [...formData.processing_purposes, purpose],
      })
    }
  }

  const addCustomPurpose = () => {
    if (newPurpose.trim() && !formData.processing_purposes.includes(newPurpose.trim())) {
      setFormData({
        ...formData,
        processing_purposes: [...formData.processing_purposes, newPurpose.trim()],
      })
      setNewPurpose('')
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-10">
      {/* Basic Information */}
      <section className="space-y-6">
        <SectionHeader
          title="Basic Information"
          description="Core details about your client organization."
        />

        <div className="grid gap-6 md:grid-cols-2">
          <FormField label="Organization Name" htmlFor="name" required>
            <Input
              id="name"
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              placeholder="Acme Corp"
              required
              className="border-[#EAEAEA] focus-visible:border-[#111111] focus-visible:ring-[#111111]/10 dark:border-white/10 dark:focus-visible:border-white/30"
            />
          </FormField>

          <FormField label="Sector" htmlFor="sector">
            <Select
              value={formData.sector}
              onValueChange={(value: string | null) => value && setFormData({ ...formData, sector: value })}
            >
              <SelectTrigger
                id="sector"
                className="border-[#EAEAEA] focus-visible:border-[#111111] focus-visible:ring-[#111111]/10 dark:border-white/10 dark:focus-visible:border-white/30"
              >
                <SelectValue placeholder="Select sector" />
              </SelectTrigger>
              <SelectContent>
                {sectors.map((sector) => (
                  <SelectItem key={sector.value} value={sector.value}>
                    {sector.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FormField>

          <FormField label="Country" htmlFor="country">
            <Select
              value={formData.country}
              onValueChange={(value: string | null) => value && setFormData({ ...formData, country: value })}
            >
              <SelectTrigger
                id="country"
                className="border-[#EAEAEA] focus-visible:border-[#111111] focus-visible:ring-[#111111]/10 dark:border-white/10 dark:focus-visible:border-white/30"
              >
                <SelectValue placeholder="Select country" />
              </SelectTrigger>
              <SelectContent>
                {countries.map((country) => (
                  <SelectItem key={country.value} value={country.value}>
                    {country.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FormField>

          <FormField label="Employee Count" htmlFor="employee_count">
            <Input
              id="employee_count"
              type="number"
              value={formData.employee_count ?? ''}
              onChange={(e) =>
                setFormData({
                  ...formData,
                  employee_count: e.target.value ? parseInt(e.target.value, 10) : undefined,
                })
              }
              placeholder="50"
              min={1}
              className="border-[#EAEAEA] focus-visible:border-[#111111] focus-visible:ring-[#111111]/10 dark:border-white/10 dark:focus-visible:border-white/30"
            />
          </FormField>
        </div>

        <FormField
          label="Business Description"
          htmlFor="description"
          hint="The more detail you provide, the better the generated artifacts will be."
        >
          <Textarea
            id="description"
            value={formData.description}
            onChange={(e) => setFormData({ ...formData, description: e.target.value })}
            placeholder="Describe the client's business activities, the data they process, and any relevant compliance context..."
            rows={4}
            className="border-[#EAEAEA] focus-visible:border-[#111111] focus-visible:ring-[#111111]/10 dark:border-white/10 dark:focus-visible:border-white/30 resize-none"
          />
        </FormField>
      </section>

      {/* Tech Stack */}
      <section className="space-y-6">
        <SectionHeader
          title="Tech Stack"
          description="List the SaaS tools and processors the client uses. This helps identify data flows and DPA requirements."
        />

        <div className="flex gap-3">
          <Input
            value={newTechItem}
            onChange={(e) => setNewTechItem(e.target.value)}
            placeholder="e.g., Stripe, HubSpot, AWS"
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addTechItem()
              }
            }}
            className="border-[#EAEAEA] focus-visible:border-[#111111] focus-visible:ring-[#111111]/10 dark:border-white/10 dark:focus-visible:border-white/30"
          />
          <Button
            type="button"
            variant="outline"
            onClick={addTechItem}
            className="shrink-0 border-[#EAEAEA] hover:bg-muted/50 dark:border-white/10"
          >
            <Plus className="h-4 w-4" />
          </Button>
        </div>

        {formData.tech_stack.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {formData.tech_stack.map((item, index) => (
              <div
                key={item}
                className="animate-in fade-in slide-in-from-bottom-2 duration-200"
                style={{ animationDelay: `${index * 30}ms` }}
              >
                <Chip
                  selected
                  removable
                  onRemove={() => removeTechItem(item)}
                >
                  {item}
                </Chip>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Data Subjects */}
      <section className="space-y-6">
        <SectionHeader
          title="Data Subjects"
          description="Select the categories of individuals whose data the client processes."
        />

        <div className="flex flex-wrap gap-2">
          {commonDataSubjects.map((subject) => (
            <Chip
              key={subject}
              selected={formData.data_subjects.includes(subject)}
              onClick={() => toggleDataSubject(subject)}
            >
              {subject}
            </Chip>
          ))}
        </div>

        <div className="flex gap-3">
          <Input
            value={newDataSubject}
            onChange={(e) => setNewDataSubject(e.target.value)}
            placeholder="Add custom data subject category"
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addCustomDataSubject()
              }
            }}
            className="border-[#EAEAEA] focus-visible:border-[#111111] focus-visible:ring-[#111111]/10 dark:border-white/10 dark:focus-visible:border-white/30"
          />
          <Button
            type="button"
            variant="outline"
            onClick={addCustomDataSubject}
            className="shrink-0 border-[#EAEAEA] hover:bg-muted/50 dark:border-white/10"
          >
            <Plus className="h-4 w-4" />
          </Button>
        </div>

        {formData.data_subjects.filter((s) => !commonDataSubjects.includes(s)).length > 0 && (
          <div className="flex flex-wrap gap-2 pt-2">
            <span className="w-full text-xs uppercase tracking-[0.08em] text-muted-foreground mb-1">
              Custom Categories
            </span>
            {formData.data_subjects
              .filter((s) => !commonDataSubjects.includes(s))
              .map((subject, index) => (
                <div
                  key={subject}
                  className="animate-in fade-in slide-in-from-bottom-2 duration-200"
                  style={{ animationDelay: `${index * 30}ms` }}
                >
                  <Chip
                    selected
                    removable
                    onRemove={() => toggleDataSubject(subject)}
                  >
                    {subject}
                  </Chip>
                </div>
              ))}
          </div>
        )}
      </section>

      {/* Processing Purposes */}
      <section className="space-y-6">
        <SectionHeader
          title="Processing Purposes"
          description="Select the purposes for which the client processes personal data."
        />

        <div className="flex flex-wrap gap-2">
          {commonPurposes.map((purpose) => (
            <Chip
              key={purpose}
              selected={formData.processing_purposes.includes(purpose)}
              onClick={() => togglePurpose(purpose)}
            >
              {purpose}
            </Chip>
          ))}
        </div>

        <div className="flex gap-3">
          <Input
            value={newPurpose}
            onChange={(e) => setNewPurpose(e.target.value)}
            placeholder="Add custom processing purpose"
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addCustomPurpose()
              }
            }}
            className="border-[#EAEAEA] focus-visible:border-[#111111] focus-visible:ring-[#111111]/10 dark:border-white/10 dark:focus-visible:border-white/30"
          />
          <Button
            type="button"
            variant="outline"
            onClick={addCustomPurpose}
            className="shrink-0 border-[#EAEAEA] hover:bg-muted/50 dark:border-white/10"
          >
            <Plus className="h-4 w-4" />
          </Button>
        </div>

        {formData.processing_purposes.filter((p) => !commonPurposes.includes(p)).length > 0 && (
          <div className="flex flex-wrap gap-2 pt-2">
            <span className="w-full text-xs uppercase tracking-[0.08em] text-muted-foreground mb-1">
              Custom Purposes
            </span>
            {formData.processing_purposes
              .filter((p) => !commonPurposes.includes(p))
              .map((purpose, index) => (
                <div
                  key={purpose}
                  className="animate-in fade-in slide-in-from-bottom-2 duration-200"
                  style={{ animationDelay: `${index * 30}ms` }}
                >
                  <Chip
                    selected
                    removable
                    onRemove={() => togglePurpose(purpose)}
                  >
                    {purpose}
                  </Chip>
                </div>
              ))}
          </div>
        )}
      </section>

      {/* Form Actions */}
      <div className="flex items-center gap-4 pt-8 border-t border-[#EAEAEA] dark:border-white/10">
        <Button
          type="submit"
          disabled={isSubmitting || !formData.name.trim()}
          className="bg-[#111111] text-white hover:bg-[#111111]/90 rounded-md px-6 shadow-none dark:bg-white dark:text-[#111111] dark:hover:bg-white/90 transition-all duration-200"
        >
          {isSubmitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {client ? 'Update Client' : 'Create Client'}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => router.back()}
          disabled={isSubmitting}
          className="border-[#EAEAEA] hover:bg-muted/50 rounded-md px-6 shadow-none dark:border-white/10 transition-all duration-200"
        >
          Cancel
        </Button>
      </div>
    </form>
  )
}
