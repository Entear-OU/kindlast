import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'

import {
  CitationLink,
  ObligationList,
} from '@/components/corpus/obligation-list'

/**
 * The obligations list and its citations (ENT-207).
 *
 * The assertions worth having are all about the citation, because it is the
 * thing the product's claim rests on and every way of getting it wrong renders
 * a page that looks fine.
 */

const ropa = {
  slug: 'gdpr-art-30-ropa',
  title: 'Records of Processing Activities',
  summary: 'Controllers must maintain a written record of processing.',
  severity: 'high',
  topicTags: ['ropa', 'documentation'],
  citation: {
    label: 'GDPR Art. 30',
    url: 'https://eur-lex.europa.eu/eli/reg/2016/679/oj#art_30',
    celex: '32016R0679',
    kind: 'article',
    article: 30,
  },
}

describe('ObligationList', () => {
  it('links the citation out to the publisher, not inward', () => {
    // The corpus stores no verbatim Official Journal text, so this link is
    // where the authoritative wording actually is. A customer checking a claim
    // should land on the law rather than on our copy of it.
    render(
      <ObligationList obligations={[ropa]} hrefFor={(s) => `/o/acme/${s}`} />,
    )

    const citation = screen.getByRole('link', { name: 'GDPR Art. 30' })
    expect(citation).toHaveAttribute('href', ropa.citation.url)
    expect(citation).toHaveAttribute('target', '_blank')
    // Without noopener the opened page gets a handle on this one.
    expect(citation.getAttribute('rel')).toContain('noopener')
  })

  it('names the severity in words rather than by colour alone', () => {
    // Colour-only or icon-only severity is the accessibility mistake this kind
    // of list makes most often.
    render(
      <ObligationList obligations={[ropa]} hrefFor={(s) => `/o/acme/${s}`} />,
    )
    expect(screen.getByText('high')).toBeInTheDocument()
  })

  it('links the title to the obligation page', () => {
    render(
      <ObligationList obligations={[ropa]} hrefFor={(s) => `/o/acme/${s}`} />,
    )

    expect(
      screen.getByRole('link', { name: 'Records of Processing Activities' }),
    ).toHaveAttribute('href', '/o/acme/gdpr-art-30-ropa')
  })
})

describe('CitationLink', () => {
  it('renders the stored label rather than rebuilding one', () => {
    // The label comes from `analyst_citation_label`, the same function that
    // named the citation on every finding. Rebuilding it here would diverge the
    // first time a regulation needed a special case, and one provision reading
    // two ways is how a customer decides the product does not know what it is
    // talking about.
    render(
      <CitationLink
        citation={{
          label: 'EU AI Act Annex III',
          url: 'https://example.test/anx_III',
          celex: '32024R1689',
          kind: 'annex',
          annex: 'III',
        }}
      />,
    )

    expect(screen.getByRole('link')).toHaveTextContent('EU AI Act Annex III')
  })

  it('shows the label unlinked when there is no URL', () => {
    // The corpus has no anchor for every provision, and a link that 404s on a
    // regulator's website is worse than no link.
    render(<CitationLink citation={{ label: 'GDPR Art. 30' }} />)

    expect(screen.getByText('GDPR Art. 30')).toBeInTheDocument()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('renders nothing at all when there is no citation', () => {
    const { container } = render(<CitationLink citation={undefined} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('shows a raw CELEX label as it is, because that means something', () => {
    // A label reading `32016R0679 Art. 30` is the citation helper falling back
    // because the corpus holds no document under that CELEX. Rendering it
    // faithfully is how somebody notices; prettifying it here would hide the
    // exact condition ENT-207 existed to fix.
    render(<CitationLink citation={{ label: '32016R0679 Art. 30' }} />)
    expect(screen.getByText('32016R0679 Art. 30')).toBeInTheDocument()
  })
})
