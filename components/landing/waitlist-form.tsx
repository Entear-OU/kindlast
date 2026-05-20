import Link from 'next/link'
import { ArrowRight } from 'lucide-react'

interface WaitlistFormProps {
  className?: string
  size?: 'default' | 'large'
  variant?: 'default' | 'inverted'
  label?: string
}

export function WaitlistForm({
  className = '',
  size = 'default',
  variant = 'default',
  label = 'Join the waitlist',
}: WaitlistFormProps) {
  const py = size === 'large' ? 'py-[1.1rem]' : 'py-3.5'
  const px = size === 'large' ? 'px-10' : 'px-8'
  const textSize = size === 'large' ? 'text-[17px]' : 'text-[15px]'

  const cls =
    variant === 'inverted'
      ? `inline-flex items-center gap-2.5 rounded-full bg-[#00C9A7] ${py} ${px} ${textSize} font-bold tracking-[-0.01em] text-[#0D1B2A] transition-all duration-150 hover:bg-[#00b898] active:scale-[0.97]`
      : `inline-flex items-center gap-2.5 rounded-full bg-[#0D1B2A] ${py} ${px} ${textSize} font-bold tracking-[-0.01em] text-white shadow-[0_4px_24px_-4px_rgba(13,27,42,0.35)] transition-all duration-150 hover:bg-[#162537] active:scale-[0.97]`

  return (
    <div className={className}>
      <Link href="https://tally.so/r/zxZaaM" className={cls}>
        {label}
        <ArrowRight className={size === 'large' ? 'h-5 w-5' : 'h-4 w-4'} />
      </Link>
    </div>
  )
}
