'use client'

interface FindingFiltersProps {
  selectedSeverity: string
  selectedCategory: string
  onSeverityChange: (severity: string) => void
  onCategoryChange: (category: string) => void
}

const severities = [
  { value: '', label: 'All Severities' },
  { value: 'critical', label: 'Critical' },
  { value: 'high', label: 'High' },
  { value: 'medium', label: 'Medium' },
  { value: 'low', label: 'Low' },
  { value: 'pass', label: 'Pass' },
]

const categories = [
  { value: '', label: 'All Categories' },
  { value: 'lawful_basis', label: 'Lawful Basis' },
  { value: 'consent', label: 'Consent' },
  { value: 'data_subject_rights', label: 'Data Subject Rights' },
  { value: 'privacy_policy', label: 'Privacy Policy' },
  { value: 'data_security', label: 'Data Security' },
  { value: 'breach_notification', label: 'Breach Notification' },
  { value: 'data_processing_records', label: 'Data Processing Records' },
  { value: 'dpo_requirement', label: 'DPO Requirement' },
  { value: 'cross_border_transfers', label: 'Cross-Border Transfers' },
  { value: 'cookie_compliance', label: 'Cookie Compliance' },
  { value: 'children_data', label: 'Children Data' },
  { value: 'data_minimization', label: 'Data Minimization' },
]

export function FindingFilters({
  selectedSeverity,
  selectedCategory,
  onSeverityChange,
  onCategoryChange,
}: FindingFiltersProps) {
  return (
    <div className="flex flex-wrap gap-3">
      <select
        value={selectedSeverity}
        onChange={(e) => onSeverityChange(e.target.value)}
        className="rounded-lg border border-border bg-background px-3 py-1.5 text-sm"
        aria-label="Filter by severity"
      >
        {severities.map((s) => (
          <option key={s.value} value={s.value}>
            {s.label}
          </option>
        ))}
      </select>

      <select
        value={selectedCategory}
        onChange={(e) => onCategoryChange(e.target.value)}
        className="rounded-lg border border-border bg-background px-3 py-1.5 text-sm"
        aria-label="Filter by category"
      >
        {categories.map((c) => (
          <option key={c.value} value={c.value}>
            {c.label}
          </option>
        ))}
      </select>
    </div>
  )
}
