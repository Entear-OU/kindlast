'use client'

import { useEffect, useRef, useState } from 'react'
import * as THREE from 'three'

/**
 * A slow-drifting wireframe lattice, rendered over the hero plate.
 *
 * This is an overlay, never the content. The photographic plate underneath is
 * the LCP element and the fallback, so anything that goes wrong here (no WebGL,
 * a lost context, reduced motion) simply leaves the reader with the still
 * image. The canvas stays fully transparent until a frame has actually been
 * drawn, so a failed init cannot flash a black rectangle over the hero.
 *
 * The form is deliberate rather than decorative: a receding grid plane with a
 * gentle wave through it, in the same hairline language as the guilloche and
 * the intaglio macro elsewhere on the page. It reads as structure under the
 * city rather than as an effect laid on top of it.
 */

const GRID = 44          // lines per axis
const SPAN = 60          // world units across
const TEAL = 0x00c9a7

function prefersReducedMotion() {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

export function HeroLattice() {
  const hostRef = useRef<HTMLDivElement>(null)
  const [running, setRunning] = useState(false)

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    if (prefersReducedMotion()) return

    let renderer: THREE.WebGLRenderer
    try {
      renderer = new THREE.WebGLRenderer({ alpha: true, antialias: true })
    } catch {
      // No WebGL. The plate underneath is already the finished hero.
      return
    }

    let frame = 0
    let disposed = false

    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
    renderer.setSize(host.clientWidth, host.clientHeight)
    renderer.setClearColor(0x000000, 0)
    host.appendChild(renderer.domElement)

    const scene = new THREE.Scene()
    // Fog does the fading, so distant lines dissolve into the plate instead of
    // ending on a hard edge at the horizon.
    scene.fog = new THREE.Fog(0x0a141f, 26, 68)

    const camera = new THREE.PerspectiveCamera(
      52,
      host.clientWidth / Math.max(host.clientHeight, 1),
      0.1,
      200
    )
    camera.position.set(0, 6.5, 26)
    camera.lookAt(0, 0, -6)

    // Grid as one LineSegments buffer: two draw calls would be wasteful for
    // something this simple, and the vertex count is small enough to displace
    // on the CPU each frame.
    const positions: number[] = []
    const step = SPAN / GRID
    const half = SPAN / 2
    for (let i = 0; i <= GRID; i++) {
      const p = -half + i * step
      for (let j = 0; j < GRID; j++) {
        const a = -half + j * step
        const b = a + step
        positions.push(p, 0, a, p, 0, b) // lines along Z
        positions.push(a, 0, p, b, 0, p) // lines along X
      }
    }

    const geometry = new THREE.BufferGeometry()
    const attr = new THREE.Float32BufferAttribute(positions, 3)
    geometry.setAttribute('position', attr)
    const base = Float32Array.from(positions)

    const material = new THREE.LineBasicMaterial({
      color: TEAL,
      transparent: true,
      opacity: 0.22,
    })
    const lattice = new THREE.LineSegments(geometry, material)
    scene.add(lattice)

    // Pointer parallax, damped. Stored as a target the camera eases toward, so
    // a fast flick does not snap the whole scene.
    const target = { x: 0, y: 0 }
    const eased = { x: 0, y: 0 }
    const onPointer = (e: PointerEvent) => {
      target.x = (e.clientX / window.innerWidth - 0.5) * 2
      target.y = (e.clientY / window.innerHeight - 0.5) * 2
    }
    window.addEventListener('pointermove', onPointer, { passive: true })

    const onResize = () => {
      if (disposed || !host.clientWidth) return
      renderer.setSize(host.clientWidth, host.clientHeight)
      camera.aspect = host.clientWidth / Math.max(host.clientHeight, 1)
      camera.updateProjectionMatrix()
    }
    window.addEventListener('resize', onResize)

    // Pause entirely when the hero is scrolled out of view. There is no reason
    // to hold the GPU busy for something nobody is looking at.
    let visible = true
    const io = new IntersectionObserver(
      ([entry]) => {
        visible = entry.isIntersecting
      },
      { threshold: 0 }
    )
    io.observe(host)

    const clock = new THREE.Clock()
    let painted = false

    const tick = () => {
      frame = requestAnimationFrame(tick)
      if (!visible) return

      const t = clock.getElapsedTime()
      const arr = attr.array as Float32Array

      // Two crossed waves, very low amplitude. Enough to read as living
      // structure, not enough to become an effect.
      for (let i = 0; i < arr.length; i += 3) {
        const x = base[i]
        const z = base[i + 2]
        arr[i + 1] =
          Math.sin(x * 0.16 + t * 0.34) * 0.75 +
          Math.cos(z * 0.13 - t * 0.22) * 0.55
      }
      attr.needsUpdate = true

      eased.x += (target.x - eased.x) * 0.03
      eased.y += (target.y - eased.y) * 0.03
      camera.position.x = eased.x * 2.4
      camera.position.y = 6.5 - eased.y * 1.1
      camera.lookAt(0, 0, -6)

      lattice.rotation.y = t * 0.012

      renderer.render(scene, camera)

      if (!painted) {
        painted = true
        setRunning(true)
      }
    }
    tick()

    return () => {
      disposed = true
      cancelAnimationFrame(frame)
      io.disconnect()
      window.removeEventListener('pointermove', onPointer)
      window.removeEventListener('resize', onResize)
      geometry.dispose()
      material.dispose()
      renderer.dispose()
      if (renderer.domElement.parentNode === host) {
        host.removeChild(renderer.domElement)
      }
    }
  }, [])

  return (
    <div
      ref={hostRef}
      aria-hidden="true"
      className={[
        'pointer-events-none absolute inset-0 transition-opacity duration-[1200ms] ease-out',
        running ? 'opacity-100' : 'opacity-0',
      ].join(' ')}
    />
  )
}
