import type { Metadata } from 'next'

export const metadata: Metadata = {
  title: 'Compliance Q&A | Kindlast',
  description:
    'Ask questions about GDPR and EU AI Act compliance. Get cited, grounded answers from primary regulatory sources.',
}

export default function QueryLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return <>{children}</>
}
