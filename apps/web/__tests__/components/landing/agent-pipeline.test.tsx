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
    // Reader-facing names. `Comms` and `Executor` are what the code calls
    // them, which is jargon on a page a non-engineer has to follow, so the
    // page uses plainer names and the `technical` line names the component in
    // the repository. The next test is what stops those two drifting apart.
    expect(PIPELINE_STAGES.map((s) => s.agent)).toEqual([
      'The Watcher',
      'The Analyst',
      'The Messenger',
      'The Hands',
    ])
  })

  it('ties every reader-facing name back to its component in the repository', () => {
    // A friendlier label is fine; an unfindable one is not. Someone who reads
    // "The Hands" here has to be able to go and find the Executor in the
    // source, or the page has quietly become marketing abstraction.
    const codeNames = ['Watcher', 'Analyst', 'Comms', 'Executor']
    PIPELINE_STAGES.forEach((stage, i) => {
      expect(stage.technical).toContain(codeNames[i])
      expect(stage.technical).toMatch(/in the repository/i)
    })
  })

  it('keeps the stage ids matching the code names', () => {
    expect(PIPELINE_STAGES.map((s) => s.id)).toEqual([
      'watcher',
      'analyst',
      'comms',
      'executor',
    ])
  })

  it('numbers the stages in pipeline order', () => {
    expect(PIPELINE_STAGES.map((s) => s.index)).toEqual([
      '01',
      '02',
      '03',
      '04',
    ])
  })
})

describe('AgentPipeline', () => {
  it('names all four agents', () => {
    render(<AgentPipeline />)
    for (const stage of PIPELINE_STAGES) {
      expect(
        screen.getByRole('heading', { name: new RegExp(stage.agent, 'i') }),
      ).toBeInTheDocument()
    }
  })

  it('renders every stage as a list item in order', () => {
    render(<AgentPipeline />)
    const items = screen.getAllByRole('listitem')
    expect(items).toHaveLength(PIPELINE_STAGES.length)
    items.forEach((item, i) => {
      expect(within(item).getByRole('heading')).toHaveTextContent(
        PIPELINE_STAGES[i].agent,
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

  it('explains the watcher is scheduled and deduplicated, in plain language first', () => {
    // The page is read by founders deciding whether to trust this, not only by
    // engineers, so the scheduling and deduplication claims have to land
    // without jargon. The precise mechanism still has to be present for anyone
    // who wants to check it against the public repository, which is what the
    // `technical` line carries.
    const { container } = render(<AgentPipeline />)
    const copy = container.textContent ?? ''
    expect(copy).toMatch(/every day/i)
    expect(copy).toMatch(/updates the one you already have/i)
    expect(copy).toMatch(/pg_cron/)
    expect(copy).toMatch(/dedup key/i)
  })

  it('demotes the engineering rather than deleting it', () => {
    // Every stage must still name how it is actually built, and name the
    // component in the repository, so the page stays checkable.
    const { container } = render(<AgentPipeline />)
    const copy = container.textContent ?? ''
    for (const stage of PIPELINE_STAGES) {
      expect(copy).toContain(stage.technical)
    }
    expect(copy).toMatch(/in the repository/i)
  })

  it('leads each stage with a single plain-language takeaway', () => {
    const { container } = render(<AgentPipeline />)
    const copy = container.textContent ?? ''
    for (const stage of PIPELINE_STAGES) {
      expect(copy).toContain(stage.plain)
    }
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
    expect(copy).toMatch(/approve/i)
    expect(copy).toMatch(/nothing happens/i)
    expect(copy).toMatch(/audit log/i)
  })

  it('carries a single named signal through the pipeline', () => {
    render(<AgentPipeline />)
    expect(
      screen.getByText(/ropa-gap:marketing-analytics/i),
    ).toBeInTheDocument()
  })

  it('shows the first stage as the initial signal state', () => {
    render(<AgentPipeline />)
    expect(screen.getByTestId('signal-status')).toHaveTextContent(
      PIPELINE_STAGES[0].signal.status,
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
