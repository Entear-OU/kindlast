'use client'

import { useEffect, useRef } from 'react'
import gsap from 'gsap'
import { ScrollTrigger } from 'gsap/ScrollTrigger'

/**
 * A ruled hairline grid with crosshair nodes and bracketed margin labels.
 *
 * This is the flat counterpart of the WebGL lattice in the hero: the same
 * structure, seen head on instead of in perspective, so scrolling out of the
 * hero reads as the mesh settling rather than as one motif being swapped for
 * another.
 *
 * The bracketed labels are the point. On a compliance product the ambient
 * marginalia should be the actual operating facts (when the Watcher runs, which
 * article a finding cites, what the licence is), not decorative glyphs. They are
 * set in the mono face the product already uses for identifiers, at watermark
 * weight, so they read as instrumentation rather than as copy competing with
 * the headline.
 *
 * Entirely decorative: hidden from assistive technology, never interactive, and
 * the parallax is dropped under reduced motion while the grid stays.
 */

gsap.registerPlugin(ScrollTrigger)

export type GridLabel = {
  text: string
  /** Percentage offsets within the section. */
  top: string
  left?: string
  right?: string
  /** Travel over the section's scroll, in px. Negative moves up. */
  drift?: number
}

export const DEFAULT_LABELS: GridLabel[] = [
  { text: '[ WATCHER · 06:00 UTC ]', top: '9%', left: '2%', drift: -60 },
  { text: '[ GDPR ART. 30 ]', top: '31%', right: '2.5%', drift: -110 },
  { text: '[ DEDUP KEY ]', top: '56%', left: '2%', drift: -80 },
  { text: '[ AI ACT ANNEX III ]', top: '74%', right: '2%', drift: -140 },
  { text: '[ AGPL-3.0 ]', top: '91%', left: '3%', drift: -50 },
]

export function TechnicalGrid({
  labels = DEFAULT_LABELS,
  tone = 'dark',
  /** Grid pitch in px. */
  cell = 132,
  className,
}: {
  labels?: GridLabel[]
  /** `dark` rules sit on the warm ground; `light` rules sit on a navy plate. */
  tone?: 'dark' | 'light'
  cell?: number
  className?: string
}) {
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const root = rootRef.current
    if (!root) return

    const ctx = gsap.context(() => {
      const mm = gsap.matchMedia()
      mm.add('(prefers-reduced-motion: no-preference)', () => {
        gsap.utils
          .toArray<HTMLElement>('[data-grid-label]', root)
          .forEach((el) => {
            gsap.to(el, {
              y: Number(el.dataset.drift ?? 0),
              ease: 'none',
              scrollTrigger: {
                trigger: root,
                start: 'top bottom',
                end: 'bottom top',
                scrub: 0.6,
              },
            })
          })
      })
      return () => mm.revert()
    }, root)

    return () => ctx.revert()
  }, [labels])

  const rule =
    tone === 'dark' ? 'rgba(13,27,42,0.055)' : 'rgba(255,255,255,0.055)'
  const node =
    tone === 'dark' ? 'rgba(13,27,42,0.16)' : 'rgba(255,255,255,0.18)'
  const labelColor = tone === 'dark' ? 'text-[#0D1B2A]/25' : 'text-white/25'

  return (
    <div
      ref={rootRef}
      aria-hidden="true"
      className={`pointer-events-none absolute inset-0 select-none overflow-hidden ${className ?? ''}`}
    >
      {/* Ruled grid. Two repeating linear gradients rather than an SVG, so it
          costs nothing and scales with the section. */}
      <div
        className="absolute inset-0"
        style={{
          backgroundImage: `linear-gradient(to right, ${rule} 1px, transparent 1px), linear-gradient(to bottom, ${rule} 1px, transparent 1px)`,
          backgroundSize: `${cell}px ${cell}px`,
        }}
      />

      {/* Crosshair nodes on the intersections, dotted at the same pitch so they
          land exactly where the rules cross. */}
      <div
        className="absolute inset-0"
        style={{
          backgroundImage: `radial-gradient(circle, ${node} 1px, transparent 1.4px)`,
          backgroundSize: `${cell}px ${cell}px`,
          backgroundPosition: `-0.5px -0.5px`,
        }}
      />

      {/* Bracketed marginalia: the real operating facts, drifting on scroll. */}
      {labels.map((l) => (
        <span
          key={l.text}
          data-grid-label
          data-drift={l.drift ?? 0}
          className={`absolute whitespace-nowrap font-mono text-[10px] font-medium uppercase tracking-[0.18em] ${labelColor} sm:text-[11px]`}
          style={{ top: l.top, left: l.left, right: l.right }}
        >
          {l.text}
        </span>
      ))}
    </div>
  )
}
