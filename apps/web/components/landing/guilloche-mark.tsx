'use client'

import { useEffect, useRef } from 'react'
import gsap from 'gsap'

/**
 * The guilloche rosette watermark, turning slowly like a lathe.
 *
 * This replaced a 5.7 MB generated video of the same idea. The figure is a
 * spirograph, so rotating it in code is seamless by construction (there is no
 * loop point to match), resolution-independent, and costs 1.6 KB instead of
 * several megabytes.
 *
 * Motion is decorative, so it is the first thing to go under
 * `prefers-reduced-motion`: the mark stays, it simply stops turning.
 */
export function GuillocheMark({
  className,
  durationSeconds = 240,
}: {
  className?: string
  /** One full turn. Deliberately glacial: at 9% opacity this should read as
   *  ambience, not as something asking to be looked at. */
  durationSeconds?: number
}) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return

    const ctx = gsap.context(() => {
      const mm = gsap.matchMedia()
      mm.add('(prefers-reduced-motion: no-preference)', () => {
        gsap.to(el, {
          rotation: 360,
          duration: durationSeconds,
          ease: 'none',
          repeat: -1,
          transformOrigin: '50% 50%',
        })
      })
      return () => mm.revert()
    }, el)

    return () => ctx.revert()
  }, [durationSeconds])

  return (
    <div
      ref={ref}
      aria-hidden="true"
      className={className}
      style={{
        backgroundImage: 'url(/patterns/guilloche-rosette.svg)',
        backgroundSize: 'contain',
        backgroundRepeat: 'no-repeat',
        willChange: 'transform',
      }}
    />
  )
}
