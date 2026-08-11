'use client'

import { useEffect, useRef, useState } from 'react'
import gsap from 'gsap'
import { ScrollTrigger } from 'gsap/ScrollTrigger'
import { PIPELINE_STAGES, TRACKED_SIGNAL } from './pipeline-stages'

/**
 * The scroll-driven pipeline on `/how-it-works`.
 *
 * One signal (`TRACKED_SIGNAL`) is carried through all four agents. The reader
 * scrolls the four stages; a sticky rail alongside them reports what has
 * happened to that one signal so far. The point of tracking a single object
 * rather than describing four capabilities is that the architecture only makes
 * sense as a sequence: the watcher's dedup key is why the analyst is not asked
 * twice, and the analyst's single proposed action is what the executor is
 * allowed to perform.
 *
 * Three constraints shape the implementation:
 *
 * 1. Everything is readable with no JavaScript at all. No initial state is set
 *    in CSS, so the server-rendered markup is already the final state. GSAP
 *    only ever moves elements away from that and back to it.
 * 2. `prefers-reduced-motion: reduce` gets no tweens whatsoever. The stage
 *    reveals and the scrubbed progress rail live inside a `gsap.matchMedia()`
 *    branch that simply never runs for those visitors.
 * 3. The ScrollTriggers that advance the *content* of the sticky rail are not
 *    animations, so they are created outside `matchMedia`. Swapping which
 *    status is on screen is not motion, and a reduced-motion visitor still
 *    wants the rail to keep up with what they are reading.
 */
export function AgentPipeline() {
  const rootRef = useRef<HTMLDivElement>(null)
  const [activeStage, setActiveStage] = useState(0)

  useEffect(() => {
    const root = rootRef.current
    if (!root) return

    // Without layout every panel measures as a zero-height box at the top of
    // the document, so every ScrollTrigger would report itself entered at
    // once and the rail would jump straight to the last stage. That is the
    // jsdom case, and it is also the case where there is nothing to scroll
    // through, so there is nothing worth wiring up either.
    if (root.getBoundingClientRect().height === 0) return

    gsap.registerPlugin(ScrollTrigger)

    const matchMedia = gsap.matchMedia()

    const ctx = gsap.context(() => {
      const panels = gsap.utils.toArray<HTMLElement>('[data-stage-panel]', root)

      // Content sync, deliberately outside the reduced-motion guard.
      panels.forEach((panel, index) => {
        ScrollTrigger.create({
          trigger: panel,
          start: 'top 55%',
          end: 'bottom 55%',
          onEnter: () => setActiveStage(index),
          onEnterBack: () => setActiveStage(index),
        })
      })

      matchMedia.add('(prefers-reduced-motion: no-preference)', () => {
        // Transform and opacity only: no layout property is ever tweened, so
        // none of this can cause a reflow mid-scroll.
        panels.forEach((panel) => {
          gsap.from(panel, {
            opacity: 0,
            y: 32,
            duration: 0.7,
            ease: 'power2.out',
            // Drop the inline styles once the reveal is done. A leftover
            // transform keeps the panel in its own compositing layer, which
            // costs nothing but softens the text on some displays.
            clearProps: 'opacity,transform',
            scrollTrigger: { trigger: panel, start: 'top 88%', once: true },
          })
        })

        const fill = root.querySelector<HTMLElement>('[data-progress-fill]')
        const list = root.querySelector<HTMLElement>('[data-stage-list]')
        if (fill && list) {
          gsap.fromTo(
            fill,
            { scaleY: 0 },
            {
              scaleY: 1,
              ease: 'none',
              scrollTrigger: {
                trigger: list,
                start: 'top 60%',
                end: 'bottom 65%',
                scrub: 0.4,
              },
            }
          )
        }
      })
    }, rootRef)

    return () => {
      matchMedia.revert()
      matchMedia.kill()
      ctx.revert()
    }
  }, [])

  const current = PIPELINE_STAGES[activeStage] ?? PIPELINE_STAGES[0]

  return (
    <div ref={rootRef} className="mx-auto max-w-5xl px-6 lg:px-8">
      <div className="lg:grid lg:grid-cols-[19rem_minmax(0,1fr)] lg:gap-14">

        {/* Sticky signal rail. The one dark object on the warm ground, which
            is also how the open-source repo card announces itself. */}
        <aside className="mb-12 lg:mb-0">
          <div className="lg:sticky lg:top-[6.5rem]">
            <div
              className="relative overflow-hidden rounded-3xl"
              style={{ backgroundColor: '#0D1B2A' }}
            >
              <div className="noise pointer-events-none absolute inset-0 opacity-[0.05]" aria-hidden="true" />
              <div
                className="pointer-events-none absolute inset-0"
                aria-hidden="true"
                style={{
                  background:
                    'radial-gradient(ellipse 60% 65% at 90% 6%, rgba(0,201,167,0.16) 0%, transparent 62%)',
                }}
              />

              <div className="relative p-7">
                <p className="text-[12px] font-bold uppercase tracking-[0.18em] text-white/30">
                  The signal we are following
                </p>

                <p className="mt-4 text-[1.0625rem] font-extrabold leading-[1.4] tracking-[-0.02em] text-white">
                  {TRACKED_SIGNAL.title}
                </p>

                <p className="mt-3 font-mono text-[12.5px] leading-[1.5] tracking-[-0.01em] text-[#00C9A7] break-all">
                  {TRACKED_SIGNAL.dedupKey}
                </p>

                <div
                  className="mt-6 pt-6"
                  style={{ borderTop: '1px solid rgba(255,255,255,0.08)' }}
                >
                  <p className="text-[12px] font-bold uppercase tracking-[0.18em] text-white/30">
                    State
                  </p>
                  <p
                    data-testid="signal-status"
                    className="mt-2.5 text-[1.375rem] font-black tracking-[-0.03em] text-white"
                  >
                    {current.signal.status}
                  </p>
                  <p className="mt-2.5 text-[14px] font-medium leading-[1.65] tracking-[-0.005em] text-white/45">
                    {current.signal.detail}
                  </p>
                </div>

                {/* Stage ticks. Decoration plus orientation, never the only
                    carrier of information, so it is hidden from assistive tech. */}
                <div className="mt-7 flex items-center gap-2" aria-hidden="true">
                  {PIPELINE_STAGES.map((stage, index) => (
                    <span
                      key={stage.id}
                      className="h-[3px] flex-1 rounded-full transition-colors duration-300"
                      style={{
                        backgroundColor:
                          index <= activeStage
                            ? '#00C9A7'
                            : 'rgba(255,255,255,0.12)',
                      }}
                    />
                  ))}
                </div>
              </div>
            </div>
          </div>
        </aside>

        {/* The four stages. A hairline runs the full height with a scrubbed
            teal fill on top of it, so the rail reads as progress rather than
            as a border. */}
        <div className="relative" data-stage-list>
          <span
            aria-hidden="true"
            className="pointer-events-none absolute left-0 top-2 bottom-2 hidden w-px sm:block"
            style={{ backgroundColor: 'rgba(13,27,42,0.09)' }}
          />
          <span
            aria-hidden="true"
            data-progress-fill
            className="pointer-events-none absolute left-0 top-2 bottom-2 hidden w-px origin-top sm:block"
            style={{ backgroundColor: '#00C9A7' }}
          />

          <ol className="space-y-20 sm:space-y-28">
            {PIPELINE_STAGES.map((stage, index) => (
              <li
                key={stage.id}
                id={stage.id}
                data-stage-panel
                className="relative sm:pl-12"
              >
                {/* Marker on the rail */}
                <span
                  aria-hidden="true"
                  className="absolute left-[-4.5px] top-[0.6rem] hidden h-[9px] w-[9px] rounded-full transition-colors duration-300 sm:block"
                  style={{
                    backgroundColor:
                      index <= activeStage ? '#00C9A7' : '#F5F4F0',
                    border:
                      index <= activeStage
                        ? '1px solid #00C9A7'
                        : '1px solid rgba(13,27,42,0.18)',
                  }}
                />

                <span
                  aria-hidden="true"
                  className="block select-none text-[3rem] font-black leading-none tracking-[-0.04em] sm:text-[3.5rem]"
                  style={{ color: 'rgba(13,27,42,0.1)' }}
                >
                  {stage.index}
                </span>
                <h2 className="mt-2 text-[2.25rem] font-black leading-none tracking-[-0.035em] text-[#0D1B2A] sm:text-[3rem]">
                  {stage.agent}
                </h2>

                <p className="mt-4 max-w-[38ch] text-[1.375rem] font-extrabold leading-[1.25] tracking-[-0.03em] text-[#0D1B2A] sm:text-[1.75rem] text-balance">
                  {stage.headline}
                </p>

                <div className="mt-6 max-w-[58ch] space-y-5">
                  {stage.body.map((paragraph) => (
                    <p
                      key={paragraph}
                      className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
                      style={{ color: 'rgba(13,27,42,0.5)' }}
                    >
                      {paragraph}
                    </p>
                  ))}
                </div>

                <dl
                  className="mt-8 grid gap-x-8 gap-y-5 pt-7 sm:grid-cols-3"
                  style={{ borderTop: '1px solid rgba(13,27,42,0.08)' }}
                >
                  {stage.facts.map((fact) => (
                    <div key={fact.label}>
                      <dt
                        className="text-[12px] font-bold uppercase tracking-[0.18em]"
                        style={{ color: 'rgba(13,27,42,0.3)' }}
                      >
                        {fact.label}
                      </dt>
                      <dd className="mt-2 text-[15px] font-semibold leading-[1.5] tracking-[-0.01em] text-[#0D1B2A]">
                        {fact.value}
                      </dd>
                    </div>
                  ))}
                </dl>

                {/* The per-stage outcome. On large screens the sticky rail
                    carries this, so it would only be a duplicate there. */}
                <div
                  className="mt-8 rounded-2xl p-5 lg:hidden"
                  style={{
                    border: '1px solid rgba(0,201,167,0.28)',
                    backgroundColor: 'rgba(0,201,167,0.06)',
                  }}
                >
                  <p className="text-[12px] font-bold uppercase tracking-[0.18em]" style={{ color: 'rgba(13,27,42,0.4)' }}>
                    Signal state: {stage.signal.status}
                  </p>
                  <p
                    className="mt-2 text-[15px] font-medium leading-[1.65] tracking-[-0.005em]"
                    style={{ color: 'rgba(13,27,42,0.5)' }}
                  >
                    {stage.signal.detail}
                  </p>
                </div>
              </li>
            ))}
          </ol>
        </div>

      </div>
    </div>
  )
}
