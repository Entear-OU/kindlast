import type { Metadata } from 'next'
import Link from 'next/link'
import { ArrowRight } from 'lucide-react'
import { Footer } from '@/components/landing/footer'
import { GitHubMark } from '@/components/icons/github-mark'
import { TechnicalGrid } from '@/components/landing/technical-grid'
import {
  REGISTERS,
  FINDING_ANATOMY,
  CORPUS_SOURCES,
  GUARANTEES,
} from '@/components/landing/capabilities'
import { TRACKED_SIGNAL } from '@/components/landing/pipeline-stages'
import { GITHUB_REPO_URL } from '@/lib/links'

export const metadata: Metadata = {
  title: 'Features: what Kindlast covers',
  description:
    'Three registers kept current, findings cited to the article they rest on, a regulatory corpus you can read, and an executor that writes nothing without your approval.',
}

/**
 * What the product covers.
 *
 * `/how-it-works` is the sequence and `/why` is the argument. This page is the
 * surface area: what is held, what is read, what is guaranteed. It was rewritten
 * because the version before it described a different product. Two of its six
 * cards (a 0-100 compliance score and audit-ready PDF export) matched nothing in
 * the tree, two mini-visuals presented invented numbers as product data, and the
 * whole thing read as a one-shot assessment on a site that spends three other
 * pages explaining a system which runs on a schedule.
 *
 * The structural fix is that the page now follows the shape of the system: the
 * registers it keeps, the finding it produces, the corpus it reads, and the
 * things it refuses to do. The specimen finding in the middle is the same one
 * `/how-it-works` follows end to end, so a reader crossing between the two pages
 * is looking at one example rather than two.
 */
export default function FeaturesPage() {
  return (
    <>
      {/* Opening + the registers. One plate, because the registers are not a
          separate topic from what the product covers: they are the answer. */}
      <section
        className="relative overflow-hidden py-24 sm:py-32"
        style={{ backgroundColor: '#F5F4F0' }}
      >
        <TechnicalGrid
          labels={[
            // Kept short on purpose. These are `whitespace-nowrap` and anchored
            // to the section edge, so a long label reaches into the text column
            // once the viewport narrows to a phone.
            { text: '[ ART. 30 ]', top: '9%', right: '2.5%', drift: -70 },
            { text: '[ REGISTERS ]', top: '70%', left: '2%', drift: -100 },
          ]}
        />

        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <div className="grid items-start gap-12 lg:grid-cols-2">
            <div>
              <p
                className="mb-4 text-[13px] font-bold uppercase tracking-[0.18em]"
                style={{ color: 'rgba(13,27,42,0.3)' }}
              >
                What it covers
              </p>
              <h1 className="max-w-[15ch] text-[2.75rem] font-black leading-[0.94] tracking-[-0.04em] text-[#0D1B2A] sm:text-[3.75rem] text-balance">
                Two regulations, held as a live record.
              </h1>
            </div>
            <div className="space-y-5 lg:pt-3">
              <p
                className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
                style={{ color: 'rgba(13,27,42,0.5)' }}
              >
                Most compliance tools produce a document. Kindlast keeps three
                registers, checks them against the GDPR and the EU AI Act every
                day, and tells you the moment one of them stops matching what
                your business actually does.
              </p>
              <p
                className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
                style={{ color: 'rgba(13,27,42,0.5)' }}
              >
                That difference is the product. A document is true on the day it
                is written. A register is either current or it is a finding.
              </p>
            </div>
          </div>

          {/* The registers. */}
          <h2
            className="mt-20 text-[13px] font-bold uppercase tracking-[0.18em]"
            style={{ color: 'rgba(13,27,42,0.3)' }}
          >
            What it keeps
          </h2>

          <div className="mt-8 grid gap-3.5 md:grid-cols-3">
            {REGISTERS.map((register) => (
              <div
                key={register.short}
                className="flex flex-col rounded-[1.5rem] bg-white p-7"
                style={{ border: '1px solid rgba(13,27,42,0.06)' }}
              >
                <p
                  className="font-mono text-[12px] font-semibold uppercase tracking-[0.14em]"
                  style={{ color: '#00A98C' }}
                >
                  {register.short}
                </p>
                <h3 className="mt-3 text-[1.375rem] font-extrabold leading-[1.2] tracking-[-0.025em] text-[#0D1B2A]">
                  {register.name}
                </h3>
                <p
                  className="mt-3.5 text-[1rem] font-medium leading-[1.72] tracking-[-0.005em]"
                  style={{ color: 'rgba(13,27,42,0.48)' }}
                >
                  {register.body}
                </p>

                {/* The watched claim, set apart. A register nothing looks at is
                    a table, and the whole argument of this page is that these
                    are looked at without being asked.

                    `mt-auto` on a wrapper rather than on the paragraph itself:
                    the paragraph carries an inline style for its rule colour,
                    and an inline `margin-top` there would beat the utility
                    class outright, leaving the three notes hanging at three
                    different heights. `pt-6` is the floor for the short card,
                    `mt-auto` bottom-aligns the other two onto it. */}
                <div className="mt-auto pt-6">
                  <p
                    className="border-l pl-4 text-[14px] font-medium leading-[1.65] tracking-[-0.005em]"
                    style={{
                      color: 'rgba(13,27,42,0.42)',
                      borderColor: 'rgba(0,201,167,0.45)',
                    }}
                  >
                    {register.watched}
                  </p>
                </div>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* Signature: one finding, taken apart.
          This replaces two decorative panels of invented data. A specimen with
          its columns named is the only thing on a capability page that a reader
          can actually go and verify. */}
      <section
        className="relative overflow-hidden py-24 sm:py-32"
        style={{ backgroundColor: '#0D1B2A' }}
      >
        <TechnicalGrid
          tone="light"
          labels={[
            { text: '[ SPECIMEN ]', top: '6%', right: '2.5%', drift: -60 },
            { text: '[ ONE ROW ]', top: '93%', left: '2%', drift: -40 },
          ]}
        />
        <div className="noise pointer-events-none absolute inset-0 opacity-[0.05]" aria-hidden="true" />

        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <p className="mb-5 text-[13px] font-bold uppercase tracking-[0.18em] text-white/35">
            What it produces
          </p>
          <h2 className="max-w-[18ch] text-[2.5rem] font-black leading-[0.95] tracking-[-0.035em] text-white sm:text-[3.25rem] text-balance">
            Anatomy of a finding.
          </h2>
          <p className="mt-7 max-w-[620px] text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em] text-white/45">
            This is the unit of work. Not a score, not a report: one row, with
            everything needed to decide on it in the time it takes to read an
            email. Below is a real one, taken apart, with the column each part
            comes from.
          </p>

          {/* The specimen itself, stated once at full width before it is
              dissected, so the fields below have something to refer back to. */}
          <div
            className="mt-12 rounded-2xl p-7"
            style={{
              border: '1px solid rgba(0,201,167,0.28)',
              backgroundColor: 'rgba(0,201,167,0.06)',
            }}
          >
            <p className="text-[12px] font-bold uppercase tracking-[0.18em] text-white/35">
              The finding
            </p>
            <p className="mt-3 text-[1.375rem] font-extrabold leading-[1.35] tracking-[-0.025em] text-white sm:text-[1.625rem] text-balance">
              {TRACKED_SIGNAL.title}
            </p>
          </div>

          <dl className="mt-14 grid gap-x-12 gap-y-10 sm:grid-cols-2">
            {FINDING_ANATOMY.map((field) => (
              <div key={field.column}>
                <dt className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                  <span className="text-[1.0625rem] font-extrabold tracking-[-0.02em] text-white">
                    {field.label}
                  </span>
                  {/* The column name is the check. Anyone can open the file in
                      the marginalia above and confirm the field is real. */}
                  <span className="font-mono text-[12px] tracking-[-0.01em] text-white/30">
                    {field.column}
                  </span>
                </dt>
                <dd className="mt-2.5 text-[15px] font-semibold leading-[1.55] tracking-[-0.01em]" style={{ color: '#00C9A7' }}>
                  {field.value}
                </dd>
                <dd className="mt-2.5 max-w-[46ch] text-[14px] font-medium leading-[1.7] tracking-[-0.005em] text-white/40">
                  {field.note}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </section>

      {/* The corpus, then the guarantees. Paired on one plate because they
          answer the same underlying question from opposite ends: what is this
          built on, and what can it not do to me. */}
      <section
        className="relative overflow-hidden py-24 sm:py-32"
        style={{ backgroundColor: '#F5F4F0' }}
      >
        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <div className="grid items-start gap-12 lg:grid-cols-2">
            <div>
              <p
                className="mb-4 text-[13px] font-bold uppercase tracking-[0.18em]"
                style={{ color: 'rgba(13,27,42,0.3)' }}
              >
                What it reads
              </p>
              <h2 className="max-w-[16ch] text-[2.5rem] font-black leading-[0.95] tracking-[-0.035em] text-[#0D1B2A] sm:text-[3.25rem] text-balance">
                The regulation itself, not a summary of it.
              </h2>
            </div>
            <div className="lg:pt-2">
              <p
                className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
                style={{ color: 'rgba(13,27,42,0.5)' }}
              >
                Every finding is anchored to a passage of source text that ships
                in the repository. When a citation looks wrong, you can open the
                article and settle it, which is not something a model output on
                its own ever lets you do.
              </p>
            </div>
          </div>

          <dl
            className="mt-14 grid gap-x-12 gap-y-9 pt-12 sm:grid-cols-2"
            style={{ borderTop: '1px solid rgba(13,27,42,0.08)' }}
          >
            {CORPUS_SOURCES.map((source) => (
              <div key={source.name}>
                <dt className="text-[1.0625rem] font-extrabold tracking-[-0.02em] text-[#0D1B2A]">
                  {source.name}
                </dt>
                <dd
                  className="mt-2 max-w-[44ch] text-[1rem] font-medium leading-[1.72] tracking-[-0.005em]"
                  style={{ color: 'rgba(13,27,42,0.48)' }}
                >
                  {source.detail}
                </dd>
              </div>
            ))}
          </dl>

          {/* The guarantees. Stated as limits, because a limit is the only
              capability claim on this page a buyer cannot verify by using the
              product, and so the only one that has to be falsifiable in code. */}
          <h2
            className="mt-24 max-w-[16ch] text-[2.5rem] font-black leading-[0.95] tracking-[-0.035em] text-[#0D1B2A] sm:text-[3.25rem] text-balance"
          >
            What it will not do.
          </h2>

          <dl className="mt-12 grid gap-x-12 gap-y-10 sm:grid-cols-2">
            {GUARANTEES.map((guarantee) => (
              <div key={guarantee.title}>
                <dt className="text-[1.0625rem] font-extrabold leading-[1.35] tracking-[-0.02em] text-[#0D1B2A]">
                  {guarantee.title}
                </dt>
                <dd
                  className="mt-2.5 max-w-[46ch] text-[1rem] font-medium leading-[1.72] tracking-[-0.005em]"
                  style={{ color: 'rgba(13,27,42,0.48)' }}
                >
                  {guarantee.body}
                </dd>
              </div>
            ))}
          </dl>

          <div className="mt-14">
            <a
              href={GITHUB_REPO_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2.5 rounded-full bg-[#0D1B2A] px-6 py-3 text-[15px] font-semibold tracking-[-0.01em] text-white transition-all duration-150 hover:bg-[#162537] active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7]"
            >
              <GitHubMark size={17} />
              Check any of this in the source
            </a>
          </div>
        </div>
      </section>

      {/* Onward. Deliberately one exit, not a menu. */}
      <section className="relative overflow-hidden py-24 sm:py-28" style={{ backgroundColor: '#0A141F' }}>
        <div className="noise pointer-events-none absolute inset-0 opacity-[0.05]" aria-hidden="true" />
        <div
          className="pointer-events-none absolute inset-0"
          aria-hidden="true"
          style={{
            background:
              'radial-gradient(ellipse 60% 60% at 85% 5%, rgba(0,201,167,0.14) 0%, transparent 62%)',
          }}
        />

        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <p className="mb-5 text-[13px] font-bold uppercase tracking-[0.18em]" style={{ color: 'rgba(0,201,167,0.75)' }}>
            The part that matters
          </p>
          <h2 className="max-w-[18ch] text-[2.5rem] font-black leading-[0.95] tracking-[-0.035em] text-white sm:text-[3.5rem] text-balance">
            A feature list is a promise. The pipeline is the proof.
          </h2>
          <p className="mt-7 max-w-[560px] text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em] text-white/45">
            None of the above is worth much if it only happens when you remember
            to ask. Four agents keep it current on a schedule, and none of them
            can change a record without your explicit approval.
          </p>

          <div className="mt-10">
            <Link
              href="/how-it-works"
              className="group inline-flex items-center gap-2.5 whitespace-nowrap rounded-full bg-white px-6 py-3 text-[15px] font-semibold tracking-[-0.01em] text-[#0D1B2A] transition-all duration-150 hover:bg-white/90 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7]"
            >
              Follow one finding end to end
              <ArrowRight
                className="h-4 w-4 transition-transform duration-200 group-hover:translate-x-0.5"
                strokeWidth={2.25}
                aria-hidden="true"
              />
            </Link>
          </div>
        </div>
      </section>

      <Footer />
    </>
  )
}
