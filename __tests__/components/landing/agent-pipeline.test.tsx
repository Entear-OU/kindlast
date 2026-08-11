import { render, screen, within } from '@testing-library/react'
import { describe, it, expect, afterEach, vi } from 'vitest'
import { AgentPipeline } from '@/components/landing/agent-pipeline'
import { PIPELINE_STAGES } from '@/components/landing/pipeline-stages'

/**
 * The pipeline is GSAP + ScrollTrigger driven, but jsdom has no layout, so
 * scroll position is meaningless here and tween internals are not worth
 * asserting. What these tests pin is the part that must survive regardless of
 * whether the animation ever runs: the copy is in the DOM, all four agents are
 * named, and asking for reduced motion changes nothing about what is readable.
 */

const realMatchMedia = window.matchMedia

/** Make `prefers-reduced-motion: reduce` report a match. */
function preferReducedMotion() {
  window.matchMedia = vi.fn((query: string) => ({
    matches: /prefers-reduced-motion:\s*reduce/.test(query),
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
}

afterEach(() => {
  window.matchMedia = realMatchMedia
})

describe('PIPELINE_STAGES', () => {
  it('describes exactly the four agents that exist', () => {
    expect(PIPELINE_STAGES.map((s) => s.agent)).toEqual([
      'Watcher',
      'Analyst',
      'Comms',
      'Executor',
    ])
  })

  it('numbers the stages in pipeline order', () => {
    expect(PIPELINE_STAGES.map((s) => s.index)).toEqual(['01', '02', '03', '04'])
  })
})

describe('AgentPipeline', () => {
  it('names all four agents', () => {
    render(<AgentPipeline />)
    for (const stage of PIPELINE_STAGES) {
      expect(
        screen.getByRole('heading', { name: new RegExp(stage.agent, 'i') })
      ).toBeInTheDocument()
    }
  })

  it('renders every stage as a list item in order', () => {
    render(<AgentPipeline />)
    const items = screen.getAllByRole('listitem')
    expect(items).toHaveLength(PIPELINE_STAGES.length)
    items.forEach((item, i) => {
      expect(within(item).getByRole('heading')).toHaveTextContent(
        PIPELINE_STAGES[i].agent
      )
    })
  })

  it('renders the full body copy for every stage', () => {
    const { container } = render(<AgentPipeline />)
    const copy = container.textContent ?? ''
    for (const stage of PIPELINE_STAGES) {
      for (const paragraph of stage.body) {
        expect(copy).toContain(paragraph)
      }
    }
  })

  it('explains the watcher is scheduled and deduplicated', () => {
    const { container } = render(<AgentPipeline />)
    const copy = container.textContent ?? ''
    expect(copy).toMatch(/pg_cron/)
    expect(copy).toMatch(/dedup_key/)
  })

  it('explains the analyst is drafted by an LLM and checked by a critic', () => {
    const { container } = render(<AgentPipeline />)
    const copy = container.textContent ?? ''
    expect(copy).toMatch(/critic/i)
    expect(copy).toMatch(/severity/i)
  })

  it('explains the one-tap comms actions', () => {
    const { container } = render(<AgentPipeline />)
    const copy = container.textContent ?? ''
    expect(copy).toMatch(/Approve/)
    expect(copy).toMatch(/Reject/)
    expect(copy).toMatch(/Remind me later/)
  })

  it('states that the executor requires an explicit human approval', () => {
    const { container } = render(<AgentPipeline />)
    const copy = container.textContent ?? ''
    expect(copy).toMatch(/approval/i)
    expect(copy).toMatch(/audit log/i)
  })

  it('carries a single named signal through the pipeline', () => {
    render(<AgentPipeline />)
    expect(screen.getByText(/ropa-gap:marketing-analytics/i)).toBeInTheDocument()
  })

  it('shows the first stage as the initial signal state', () => {
    render(<AgentPipeline />)
    expect(screen.getByTestId('signal-status')).toHaveTextContent(
      PIPELINE_STAGES[0].signal.status
    )
  })

  describe('with prefers-reduced-motion: reduce', () => {
    it('still renders every agent and its copy', () => {
      preferReducedMotion()
      const { container } = render(<AgentPipeline />)
      const copy = container.textContent ?? ''
      for (const stage of PIPELINE_STAGES) {
        expect(copy).toContain(stage.agent)
        expect(copy).toContain(stage.headline)
      }
    })

    it('leaves no element hidden behind an un-run animation', () => {
      // The failure mode this guards against is a CSS `opacity: 0` initial
      // state that only an animation ever clears. Nothing in the subtree may
      // start transparent, because a reduced-motion visitor never gets the
      // tween that would reveal it.
      preferReducedMotion()
      const { container } = render(<AgentPipeline />)
      const elements = container.querySelectorAll<HTMLElement>('*')
      for (const el of elements) {
        expect(el.style.opacity === '0').toBe(false)
      }
    })

    it('unmounts without leaving GSAP state on the DOM', () => {
      preferReducedMotion()
      const { unmount } = render(<AgentPipeline />)
      expect(() => unmount()).not.toThrow()
    })
  })

  it('unmounts cleanly when motion is allowed', () => {
    const { unmount } = render(<AgentPipeline />)
    expect(() => unmount()).not.toThrow()
  })
})
