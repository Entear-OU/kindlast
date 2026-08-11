import type { Metadata } from 'next'
import Image from 'next/image'
import { AgentPipeline } from '@/components/landing/agent-pipeline'
import { Footer } from '@/components/landing/footer'
import { GitHubMark } from '@/components/icons/github-mark'
import { GITHUB_REPO_URL } from '@/lib/links'

export const metadata: Metadata = {
  title: 'How it works: the Kindlast agent pipeline',
  description:
    'Four agents, described as they are actually built. A scheduled watcher, an analyst checked by a deterministic critic, an email outbox, and an executor that only ever runs on an explicit human approval.',
}

/**
 * The centrepiece of the public site.
 *
 * This page is deliberately not an abstraction of the product. It describes the
 * four agents as they are implemented, down to pg_cron and the dedup key,
 * because the audience is technical founders in a regulated market and the
 * repository is public: anything vaguer than the code would be checkable and
 * wrong within a week.
 *
 * The whole page exists to land one pair of claims that only make sense
 * together, so they are the headline rather than a conclusion at the bottom.
 */
export default function HowItWorksPage() {
  return (
    <>
      {/* Statement. A dark plate, so the page opens the way the home page does
          and the header can stay transparent across both. The image is an
          empty colonnade under hard directional light: institutional, patient,
          nobody in the room. That is the Watcher, without illustrating it. */}
      <section className="relative overflow-hidden bg-[#0A141F] pt-32 pb-24 sm:pt-40 sm:pb-32">
        <Image
          src="/imagery/colonnade.webp"
          alt=""
          aria-hidden="true"
          fill
          priority
          sizes="100vw"
          className="object-cover"
        />
        <div
          className="pointer-events-none absolute inset-0"
          aria-hidden="true"
          style={{
            background: [
              'linear-gradient(180deg, rgba(10,20,31,0.80) 0%, rgba(10,20,31,0.62) 40%, rgba(10,20,31,0.94) 100%)',
              'linear-gradient(90deg, rgba(10,20,31,0.88) 0%, rgba(10,20,31,0.35) 70%, transparent 100%)',
            ].join(', '),
          }}
        />
        <div className="noise pointer-events-none absolute inset-0 opacity-[0.05]" aria-hidden="true" />

        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <p className="mb-5 text-[13px] font-bold uppercase tracking-[0.18em] text-white/40">
            The architecture
          </p>

          <h1 className="max-w-[18ch] text-[2.75rem] font-black leading-[0.94] tracking-[-0.04em] text-white sm:text-[4.25rem] text-balance">
            It runs without being asked.
            <br />
            <span style={{ color: '#00C9A7' }}>
              It never acts without approval.
            </span>
          </h1>

          <div className="mt-9 max-w-[620px] space-y-5">
            <p className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em] text-white/55">
              Those two promises pull in opposite directions, and holding both is
              the entire design. An agent that waits to be prompted is a form. An
              agent that acts on its own is a liability you have to audit. So
              Kindlast splits the work across four agents, and puts the only
              irreversible step behind a human.
            </p>
            <p className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em] text-white/55">
              What follows is the pipeline as it is actually built. To keep it
              concrete, one real finding is carried the whole way through: a
              marketing analytics tool that is processing personal data with no
              record of processing behind it.
            </p>
          </div>
        </div>
      </section>

      {/* The pipeline */}
      <section className="pb-28 sm:pb-36" style={{ backgroundColor: '#F5F4F0' }}>
        <AgentPipeline />
      </section>

      {/* Close */}
      <section className="relative overflow-hidden py-24 sm:py-32" style={{ backgroundColor: '#0D1B2A' }}>
        <div className="noise pointer-events-none absolute inset-0 opacity-[0.05]" aria-hidden="true" />
        <div
          className="pointer-events-none absolute inset-0"
          aria-hidden="true"
          style={{
            background:
              'radial-gradient(ellipse 60% 55% at 50% 108%, rgba(0,201,167,0.13) 0%, transparent 65%)',
          }}
        />

        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <h2 className="max-w-[16ch] text-[2.5rem] font-black leading-[0.95] tracking-[-0.035em] text-white sm:text-[3.5rem] text-balance">
            Do not take our word for any of this.
          </h2>
          <p className="mt-7 max-w-[560px] text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em] text-white/45">
            Every detector, every critic rule, every prompt, and every row-level
            security policy described on this page is in the repository. If a
            claim here and the code disagree, the code is the one telling the
            truth, and you can open an issue about it.
          </p>

          <div className="mt-10">
            <a
              href={GITHUB_REPO_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2.5 rounded-full bg-white px-6 py-3 text-[15px] font-semibold tracking-[-0.01em] text-[#0D1B2A] transition-all duration-150 hover:bg-white/90 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7]"
            >
              <GitHubMark size={17} />
              Read the source
            </a>
          </div>
        </div>
      </section>

      <Footer />
    </>
  )
}
