import { describe, it, expect, vi, beforeEach } from 'vitest'

/**
 * The console title template (ENT-269).
 *
 * The organisation comes first because it is the half that differs between
 * tabs, and a tab strip truncates from the end. "Ada Furniture Group, Feed"
 * still says which client you are looking at when it has been cut to fifteen
 * characters; "Feed, Ada Furniture Group" says "Feed…" three times over.
 *
 * The 404 case is the one worth testing rather than reasoning about. A foreign
 * slug must produce a title that tells the visitor nothing about whether the
 * organisation exists, and the structural reason it does is that the layout
 * only ever learns a name from the caller's own memberships. There is no name
 * to leak, so the assertion here is the stronger one: two different foreign
 * slugs produce byte-identical metadata.
 */
const currentSession = vi.fn()
const resolveOrg = vi.fn()

vi.mock('@/lib/auth/session', () => ({
  currentSession: () => currentSession(),
}))
vi.mock('@/lib/auth/org', () => ({
  resolveOrg: (...args: unknown[]) => resolveOrg(...args),
  orgPath: (slug: string, rest = '') => `/o/${slug}${rest}`,
}))

const { generateMetadata } = await import('@/app/(authed)/o/[org]/layout')

function args(slug: string) {
  return { params: Promise.resolve({ org: slug }) }
}

function member(orgName: string) {
  return {
    status: 'ok' as const,
    me: { memberships: [] },
    membership: { orgId: 'org-1', orgSlug: 'ada', orgName },
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  currentSession.mockResolvedValue({ accessToken: 'token' })
  resolveOrg.mockResolvedValue(member('Ada Furniture Group'))
})

describe('generateMetadata for a member', () => {
  it('names the organisation before the section', async () => {
    const meta = await generateMetadata(args('ada'))
    const title = meta.title as { template: string; default: string }

    expect(title.template).toBe('Ada Furniture Group, %s, Kindlast')
    expect(title.template.replace('%s', 'Feed')).toBe(
      'Ada Furniture Group, Feed, Kindlast',
    )
    expect(title.template.indexOf('Ada Furniture Group')).toBeLessThan(
      title.template.indexOf('%s'),
    )
  })

  it('still names the organisation on a page that sets no section', async () => {
    const meta = await generateMetadata(args('ada'))
    const title = meta.title as { default: string }

    expect(title.default).toBe('Ada Furniture Group, Kindlast')
  })

  it('falls back to the slug when core-api sends no name', async () => {
    // The dashboard heading already does this. A title reading ", Kindlast"
    // would be worse than a slug.
    resolveOrg.mockResolvedValue(member(''))

    const meta = await generateMetadata(args('ada'))
    const title = meta.title as { template: string; default: string }

    expect(title.template).toBe('ada, %s, Kindlast')
    expect(title.default).toBe('ada, Kindlast')
  })

  it('reads the organisation already resolved for the layout', async () => {
    await generateMetadata(args('ada'))
    expect(resolveOrg).toHaveBeenCalledWith('token', 'ada')
  })
})

describe('generateMetadata for a slug the caller does not belong to', () => {
  beforeEach(() => {
    resolveOrg.mockImplementation(async (_token: string, slug: string) => ({
      status: 'not-a-member',
      me: { memberships: [] },
      slug,
    }))
  })

  it('says nothing about the organisation', async () => {
    const meta = await generateMetadata(args('someone-elses-company'))
    const title = meta.title as { template: string; default: string }

    expect(title.default).toBe('Kindlast')
    expect(title.template).toBe('%s, Kindlast')
    expect(JSON.stringify(meta)).not.toContain('someone-elses-company')
  })

  it('cannot be told apart from a slug that does not exist at all', async () => {
    const foreign = await generateMetadata(args('someone-elses-company'))
    const absent = await generateMetadata(args('no-such-organisation'))

    expect(foreign).toEqual(absent)
  })
})

describe('generateMetadata when core-api is unreachable', () => {
  it('falls back to the generic title rather than guessing a name', async () => {
    resolveOrg.mockResolvedValue({ status: 'unavailable' })

    const meta = await generateMetadata(args('ada'))
    const title = meta.title as { template: string; default: string }

    expect(title.default).toBe('Kindlast')
    expect(title.template).toBe('%s, Kindlast')
  })
})

describe('generateMetadata with no session', () => {
  it('resolves nothing and titles nothing', async () => {
    // The layout redirects this request to sign-in. Metadata is generated
    // anyway, and calling core-api with no token would be a round trip for a
    // page nobody is going to see.
    currentSession.mockResolvedValue(null)

    const meta = await generateMetadata(args('ada'))
    const title = meta.title as { template: string; default: string }

    expect(title.default).toBe('Kindlast')
    expect(resolveOrg).not.toHaveBeenCalled()
  })
})
