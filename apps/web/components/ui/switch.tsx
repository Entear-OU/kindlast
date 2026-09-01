'use client'

import * as React from 'react'
import { Switch as SwitchPrimitive } from '@base-ui/react/switch'

import { cn } from '@/lib/utils'

/**
 * A switch, for a setting that takes effect as it is flipped.
 *
 * Base UI's root renders a real `role="switch"` with `aria-checked`, so a
 * screen reader announces the state rather than the word "checkbox", and a
 * test can address it by role. Pass `name` to submit it with a form; unchecked
 * switches submit nothing, matching a native checkbox.
 */
function Switch({
  className,
  ...props
}: React.ComponentProps<typeof SwitchPrimitive.Root>) {
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      className={cn(
        'peer inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border border-transparent bg-input p-px transition-colors outline-none',
        'focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50',
        'data-[checked]:bg-primary',
        'disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    >
      <SwitchPrimitive.Thumb
        data-slot="switch-thumb"
        className={cn(
          'block size-4 rounded-full bg-background shadow-sm transition-transform',
          'data-[checked]:translate-x-4',
        )}
      />
    </SwitchPrimitive.Root>
  )
}

export { Switch }
