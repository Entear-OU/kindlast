'use client'

import { useState } from 'react'
import { ArrowRight, CheckCircle2, Loader2 } from 'lucide-react'
import { toast } from 'sonner'

interface WaitlistFormProps {
  className?: string
  size?: 'default' | 'large'
  placeholder?: string
  variant?: 'default' | 'inverted'
}

export function WaitlistForm({
  className = '',
  size = 'default',
  placeholder = 'Enter your work email',
  variant = 'default',
}: WaitlistFormProps) {
  const [email, setEmail] = useState('')
  const [loading, setLoading] = useState(false)
  const [done, setDone] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!email) return
    setLoading(true)

    // TODO: wire up actual backend (e.g. POST /api/waitlist)
    await new Promise((r) => setTimeout(r, 700))

    setLoading(false)
    setDone(true)
    toast.success("You're on the list!", {
      description: "We'll notify you as soon as early access opens.",
    })
    setEmail('')
  }

  if (done) {
    return (
      <div className={`flex items-center gap-2.5 ${className}`}>
        <span className={`flex h-8 w-8 items-center justify-center rounded-full ${variant === 'inverted' ? 'bg-white/20' : 'bg-primary/10'}`}>
          <CheckCircle2
            className={`h-4 w-4 ${variant === 'inverted' ? 'text-white' : 'text-primary'}`}
            strokeWidth={2.5}
          />
        </span>
        <span className={`text-[15px] font-semibold tracking-[-0.01em] ${variant === 'inverted' ? 'text-white' : 'text-foreground'}`}>
          You&apos;re on the list — we&apos;ll be in touch.
        </span>
      </div>
    )
  }

  const py = size === 'large' ? 'py-[1.05rem]' : 'py-3.5'
  const textSize = size === 'large' ? 'text-[16px]' : 'text-[15px]'

  const inputClasses =
    variant === 'inverted'
      ? `flex-1 min-w-0 rounded-full border border-white/25 bg-white/15 backdrop-blur-sm ${py} px-5 ${textSize} font-medium tracking-[-0.01em] text-white placeholder:text-white/45 outline-none focus:border-white/50 focus:bg-white/20 focus:ring-2 focus:ring-white/20 transition-all duration-150`
      : `flex-1 min-w-0 rounded-full border border-black/[0.1] bg-white ${py} px-5 ${textSize} font-medium tracking-[-0.01em] text-foreground placeholder:text-foreground/35 outline-none focus:border-primary/40 focus:ring-2 focus:ring-primary/12 transition-all duration-150`

  const btnClasses =
    variant === 'inverted'
      ? `inline-flex items-center justify-center gap-2 rounded-full bg-white ${py} px-7 ${textSize} font-bold tracking-[-0.01em] text-primary transition-all duration-150 hover:bg-white/90 active:scale-[0.97] disabled:opacity-70 whitespace-nowrap`
      : `inline-flex items-center justify-center gap-2 rounded-full bg-primary ${py} px-7 ${textSize} font-bold tracking-[-0.01em] text-white shadow-[0_4px_20px_-4px_rgba(80,168,90,0.45)] transition-all duration-150 hover:bg-primary/90 active:scale-[0.97] disabled:opacity-70 whitespace-nowrap`

  return (
    <form onSubmit={handleSubmit} className={`flex flex-col sm:flex-row gap-2.5 ${className}`}>
      <input
        type="email"
        required
        placeholder={placeholder}
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        className={inputClasses}
      />
      <button type="submit" disabled={loading} className={btnClasses}>
        {loading ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : (
          <>
            Join the waitlist
            <ArrowRight className="h-4 w-4" />
          </>
        )}
      </button>
    </form>
  )
}
