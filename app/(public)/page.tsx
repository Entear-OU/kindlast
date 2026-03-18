import { Hero } from '@/components/landing/hero'
import { Features } from '@/components/landing/features'
import { HowItWorks } from '@/components/landing/how-it-works'
import { Footer } from '@/components/landing/footer'
import Link from 'next/link'

export default function LandingPage() {
  return (
    <>
      <Hero />

      {/* Problem statement */}
      <section className="bg-muted/30 py-24 sm:py-32">
        <div className="mx-auto max-w-3xl px-6 text-center lg:px-8">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Why SMEs struggle with compliance
          </h2>
          <p className="mt-6 text-lg leading-8 text-muted-foreground">
            GDPR fines can reach 4% of annual turnover. The EU AI Act adds new
            obligations for any business using AI. Yet most SMEs lack the legal
            budget, in-house expertise, or time to figure out where they stand.
            Kindlast closes that gap with AI-powered analysis that turns
            regulatory complexity into a clear action plan.
          </p>
        </div>
      </section>

      <HowItWorks />

      <div id="features">
        <Features />
      </div>

      {/* Pricing preview */}
      <section className="bg-background py-24 sm:py-32">
        <div className="mx-auto max-w-3xl px-6 text-center lg:px-8">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Simple, transparent pricing
          </h2>
          <p className="mt-4 text-lg leading-8 text-muted-foreground">
            Start free. Upgrade when you need full findings, AI Act
            classification, and PDF exports.
          </p>
          <div className="mt-10">
            <Link
              href="/pricing"
              className="rounded-md bg-primary px-6 py-3 text-sm font-semibold text-primary-foreground shadow-sm hover:bg-primary/90 transition-colors"
            >
              View Pricing
            </Link>
          </div>
        </div>
      </section>

      <Footer />
    </>
  )
}
