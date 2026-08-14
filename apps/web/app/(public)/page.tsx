import { Hero } from '@/components/landing/hero'
import { TechnicalGrid } from '@/components/landing/technical-grid'
import { CapabilitySummary } from '@/components/landing/capability-summary'
import { OpenSource } from '@/components/landing/open-source'
import { Footer } from '@/components/landing/footer'

/**
 * The home page after ENT-190.
 *
 * The site is three routes now. This one has a single job: say what Kindlast
 * is, why the problem is worth solving, and give the reader two honest exits
 * (the pipeline on `/how-it-works`, the surface area on `/features`). The
 * capability detail and the agent architecture both moved out, and the
 * waitlist is gone entirely.
 *
 * Open source stays a section here rather than becoming a fourth route: the
 * full story already lives in the repository, and a marketing page about
 * having a repository is worse than the repository.
 */
export default function LandingPage() {
  return (
    <>
      <Hero />

      {/* Problem, stated in numbers.
          The ruled grid is the hero's WebGL lattice seen head on, so leaving
          the hero reads as the mesh settling rather than as a change of motif.
          Its marginalia are real operating facts, not decoration. */}
      <section className="relative overflow-hidden py-24 sm:py-32" style={{ backgroundColor: '#F5F4F0' }}>
        <TechnicalGrid
          labels={[
            { text: '[ GDPR · IN FORCE 2018 ]', top: '10%', left: '2%', drift: -70 },
            { text: '[ ART. 83 · PENALTIES ]', top: '34%', right: '2.5%', drift: -120 },
            { text: '[ AI ACT · ANNEX III ]', top: '68%', left: '2%', drift: -90 },
            { text: '[ DEADLINE · 2026-08-02 ]', top: '88%', right: '3%', drift: -50 },
          ]}
        />
        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">

          {/* The headline stat trio is gone. It began as external facts (max
              fine, fine threshold, Annex III deadline), which went stale as the
              dates arrived and the figures moved, and its replacement (Daily,
              Two, Zero) restated claims the hero and the pipeline already make
              better. A number on a page has to earn its size, and these did
              not. */}
          <div className="grid items-start gap-12 lg:grid-cols-2">
            <div>
              <p
                className="mb-4 text-[13px] font-bold uppercase tracking-[0.18em]"
                style={{ color: 'rgba(13,27,42,0.3)' }}
              >
                The reality
              </p>
              <h2 className="text-[3rem] font-black leading-none tracking-[-0.035em] text-[#0D1B2A] sm:text-[3.75rem] text-balance">
                Why compliance
                <br />
                quietly rots
              </h2>
            </div>
            <div className="space-y-5 lg:pt-2">
              <p
                className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
                style={{ color: 'rgba(13,27,42,0.5)' }}
              >
                Most teams lack the legal budget, the in-house expertise, or the
                time to work out where they stand. GDPR has been in force since
                2018 and fines are accelerating. The EU AI Act adds a second wave
                of obligations on top.
              </p>
              <p
                className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
                style={{ color: 'rgba(13,27,42,0.5)' }}
              >
                Compliance fails quietly, and it fails on the days nobody
                remembered to check. Kindlast turns that regulatory surface into
                a plain-language action plan your team can act on.
              </p>
            </div>
          </div>

        </div>
      </section>

      <CapabilitySummary />

      <OpenSource />

      <Footer />
    </>
  )
}
