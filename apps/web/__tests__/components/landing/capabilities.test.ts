import { describe, it, expect } from 'vitest'
import {
  REGISTERS,
  FINDING_ANATOMY,
  CORPUS_SOURCES,
  GUARANTEES,
} from '@/components/landing/capabilities'
import { TRACKED_SIGNAL } from '@/components/landing/pipeline-stages'

/**
 * The capability data behind `/features`.
 *
 * These tests exist because the page they feed makes claims about a public
 * repository. The previous version of this page advertised a 0-100 compliance
 * score and audit-ready PDF export, neither of which was ever implemented: the
 * marketing component was the only file in the tree that mentioned either. On a
 * site whose entire argument is "read the source, the code is the one telling
 * the truth", that is the most expensive kind of copy to get wrong.
 *
 * So the shape enforced here is deliberate. Every capability has to name the
 * thing in the system that backs it, and nothing is allowed on the page without
 * one.
 */
describe('capabilities data', () => {
  describe('REGISTERS', () => {
    it('covers the three registers the product actually keeps', () => {
      const names = REGISTERS.map((r) => r.short)
      expect(names).toContain('ROPA')
      expect(names).toContain('DSAR log')
      expect(names).toContain('AI system register')
    })

    it('says for each register what the agents do to it', () => {
      for (const register of REGISTERS) {
        expect(register.name.length).toBeGreaterThan(0)
        expect(register.body.length).toBeGreaterThan(40)
        // The watched claim: a register nothing watches is just a table.
        expect(register.watched.length).toBeGreaterThan(20)
      }
    })
  })

  describe('FINDING_ANATOMY', () => {
    it('lists only fields a finding row actually carries', () => {
      // Each entry names the column it is drawn from, so the page can be
      // checked against `lib/feed/findings.ts` rather than trusted.
      const columns = FINDING_ANATOMY.map((f) => f.column)
      expect(columns).toContain('regulatory_obligation')
      expect(columns).toContain('severity')
      expect(columns).toContain('effort_estimate')
      expect(columns).toContain('proposed_action')
      expect(columns).toContain('citation_url')
    })

    it('gives every field a label and a concrete example value', () => {
      for (const field of FINDING_ANATOMY) {
        expect(field.label.length).toBeGreaterThan(0)
        expect(field.value.length).toBeGreaterThan(0)
        expect(field.column).toMatch(/^[a-z_]+$/)
      }
    })

    it('uses the same specimen finding as the pipeline explainer', () => {
      // `/how-it-works` follows one signal end to end. If `/features` dissects
      // a different one, a reader moving between the two pages is looking at
      // two unrelated examples and the continuity is lost for no reason.
      const values = FINDING_ANATOMY.map((f) => f.value).join(' ')
      expect(values).toMatch(/Article 30/)
      expect(TRACKED_SIGNAL.dedupKey).toBe('ropa-gap:marketing-analytics')
    })
  })

  describe('CORPUS_SOURCES', () => {
    it('names the regulatory sources that are in the repository', () => {
      const names = CORPUS_SOURCES.map((s) => s.name).join(' ')
      expect(names).toMatch(/GDPR/)
      expect(names).toMatch(/AI Act/)
      expect(names).toMatch(/EDPB/)
      expect(names).toMatch(/enforcement/i)
    })

    it('quantifies each source without quoting a figure that goes stale', () => {
      for (const source of CORPUS_SOURCES) {
        expect(source.detail.length).toBeGreaterThan(15)
      }
      // Article counts are fixed properties of the regulations. Fine counts,
      // enforcement tallies and deadline dates are not, and have already been
      // pulled off this site once for exactly that reason.
      const copy = CORPUS_SOURCES.map((s) => `${s.name} ${s.detail}`).join(' ')
      expect(copy).not.toMatch(/€/)
      expect(copy).not.toMatch(/\b\d+%/)
    })
  })

  describe('GUARANTEES', () => {
    it('states the approval gate, the audit log, isolation and self-hosting', () => {
      const copy = GUARANTEES.map((g) => `${g.title} ${g.body}`).join(' ')
      expect(copy).toMatch(/approv/i)
      expect(copy).toMatch(/audit log/i)
      expect(copy).toMatch(/row-level security/i)
      expect(copy).toMatch(/self-host/i)
    })

    it('makes no claim that the data never reaches a model provider', () => {
      // The old copy said "Data never leaves the secure pipeline", which is not
      // true in the shape a reader would take it: an LLM drafts every finding,
      // so a provider is in the loop. Self-hosting is the honest answer to the
      // question that sentence was trying to answer.
      const copy = GUARANTEES.map((g) => `${g.title} ${g.body}`).join(' ')
      expect(copy).not.toMatch(/never leaves/i)
    })
  })
})
