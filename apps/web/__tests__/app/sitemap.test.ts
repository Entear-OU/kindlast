import { describe, it, expect } from 'vitest'
import sitemap from '@/app/sitemap'

/**
 * ENT-190 turned `/how-it-works` and `/features` from in-page anchors into
 * real routes. An anchor is invisible to a crawler as a destination, so the
 * sitemap has to list them or the two pages the site is now built around never
 * get indexed separately.
 */
describe('sitemap', () => {
  const urls = () => sitemap().map((entry) => entry.url)

  it('lists the three public marketing routes', () => {
    const paths = urls().map((url) => new URL(url).pathname)
    expect(paths).toContain('/')
    expect(paths).toContain('/how-it-works')
    expect(paths).toContain('/features')
  })

  it('keeps the login route', () => {
    expect(urls().map((url) => new URL(url).pathname)).toContain('/login')
  })

  it('lists no route that no longer exists', () => {
    const paths = urls().map((url) => new URL(url).pathname)
    expect(paths).not.toContain('/pricing')
    // Open source stayed a section on `/`, so there is nothing to index here.
    expect(paths).not.toContain('/open-source')
  })

  it('ranks the pipeline explainer above the capability detail', () => {
    const byPath = Object.fromEntries(
      sitemap().map((entry) => [new URL(entry.url).pathname, entry])
    )
    expect(byPath['/how-it-works'].priority).toBeGreaterThan(
      byPath['/features'].priority as number
    )
  })
})
