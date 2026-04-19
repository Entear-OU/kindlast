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
import { Badge } from '@/components/ui/badge'
import { X, Plus, Loader2 } from 'lucide-react'
import type { Client } from '@/lib/types/database'
import type { CreateClientRequest, UpdateClientRequest } from '@/lib/api/clients'

interface ClientFormProps {
  client?: Client
  onSubmit: (data: CreateClientRequest | UpdateClientRequest) => Promise<void>
  isSubmitting?: boolean
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
    <form onSubmit={handleSubmit} className="space-y-8">
      {/* Basic Information */}
      <div className="space-y-4">
        <div>
          <h3 className="text-lg font-semibold">Basic Information</h3>
          <p className="text-sm text-muted-foreground">
            Core details about your client organization.
          </p>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="name">Organization Name *</Label>
            <Input
              id="name"
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              placeholder="Acme Corp"
              required
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="sector">Sector</Label>
            <Select
              value={formData.sector}
              onValueChange={(value: string | null) => value && setFormData({ ...formData, sector: value })}
            >
              <SelectTrigger id="sector">
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
          </div>

          <div className="space-y-2">
            <Label htmlFor="country">Country</Label>
            <Select
              value={formData.country}
              onValueChange={(value: string | null) => value && setFormData({ ...formData, country: value })}
            >
              <SelectTrigger id="country">
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
          </div>

          <div className="space-y-2">
            <Label htmlFor="employee_count">Employee Count</Label>
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
            />
          </div>
        </div>

        <div className="space-y-2">
          <Label htmlFor="description">Business Description</Label>
          <Textarea
            id="description"
            value={formData.description}
            onChange={(e) => setFormData({ ...formData, description: e.target.value })}
            placeholder="Describe the client's business activities, the data they process, and any relevant compliance context..."
            rows={4}
          />
          <p className="text-xs text-muted-foreground">
            The more detail you provide, the better the generated artifacts will be.
          </p>
        </div>
      </div>

      {/* Tech Stack */}
      <div className="space-y-4">
        <div>
          <h3 className="text-lg font-semibold">Tech Stack</h3>
          <p className="text-sm text-muted-foreground">
            List the SaaS tools and processors the client uses. This helps identify data flows and DPA requirements.
          </p>
        </div>

        <div className="flex gap-2">
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
          />
          <Button type="button" variant="outline" onClick={addTechItem}>
            <Plus className="h-4 w-4" />
          </Button>
        </div>

        {formData.tech_stack.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {formData.tech_stack.map((item) => (
              <Badge key={item} variant="secondary" className="gap-1">
                {item}
                <button
                  type="button"
                  onClick={() => removeTechItem(item)}
                  className="ml-1 hover:text-destructive"
                >
                  <X className="h-3 w-3" />
                </button>
              </Badge>
            ))}
          </div>
        )}
      </div>

      {/* Data Subjects */}
      <div className="space-y-4">
        <div>
          <h3 className="text-lg font-semibold">Data Subjects</h3>
          <p className="text-sm text-muted-foreground">
            Select the categories of individuals whose data the client processes.
          </p>
        </div>

        <div className="flex flex-wrap gap-2">
          {commonDataSubjects.map((subject) => (
            <Badge
              key={subject}
              variant={formData.data_subjects.includes(subject) ? 'default' : 'outline'}
              className="cursor-pointer"
              onClick={() => toggleDataSubject(subject)}
            >
              {subject}
            </Badge>
          ))}
        </div>

        <div className="flex gap-2">
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
          />
          <Button type="button" variant="outline" onClick={addCustomDataSubject}>
            <Plus className="h-4 w-4" />
          </Button>
        </div>

        {formData.data_subjects.filter((s) => !commonDataSubjects.includes(s)).length > 0 && (
          <div className="flex flex-wrap gap-2">
            {formData.data_subjects
              .filter((s) => !commonDataSubjects.includes(s))
              .map((subject) => (
                <Badge key={subject} variant="secondary" className="gap-1">
                  {subject}
                  <button
                    type="button"
                    onClick={() => toggleDataSubject(subject)}
                    className="ml-1 hover:text-destructive"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              ))}
          </div>
        )}
      </div>

      {/* Processing Purposes */}
      <div className="space-y-4">
        <div>
          <h3 className="text-lg font-semibold">Processing Purposes</h3>
          <p className="text-sm text-muted-foreground">
            Select the purposes for which the client processes personal data.
          </p>
        </div>

        <div className="flex flex-wrap gap-2">
          {commonPurposes.map((purpose) => (
            <Badge
              key={purpose}
              variant={formData.processing_purposes.includes(purpose) ? 'default' : 'outline'}
              className="cursor-pointer"
              onClick={() => togglePurpose(purpose)}
            >
              {purpose}
            </Badge>
          ))}
        </div>

        <div className="flex gap-2">
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
          />
          <Button type="button" variant="outline" onClick={addCustomPurpose}>
            <Plus className="h-4 w-4" />
          </Button>
        </div>

        {formData.processing_purposes.filter((p) => !commonPurposes.includes(p)).length > 0 && (
          <div className="flex flex-wrap gap-2">
            {formData.processing_purposes
              .filter((p) => !commonPurposes.includes(p))
              .map((purpose) => (
                <Badge key={purpose} variant="secondary" className="gap-1">
                  {purpose}
                  <button
                    type="button"
                    onClick={() => togglePurpose(purpose)}
                    className="ml-1 hover:text-destructive"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              ))}
          </div>
        )}
      </div>

      {/* Form Actions */}
      <div className="flex items-center gap-4 pt-4 border-t">
        <Button type="submit" disabled={isSubmitting || !formData.name.trim()}>
          {isSubmitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {client ? 'Update Client' : 'Create Client'}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => router.back()}
          disabled={isSubmitting}
        >
          Cancel
        </Button>
      </div>
    </form>
  )
}
