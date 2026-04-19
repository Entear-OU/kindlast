'use client'

import Link from 'next/link'
import { Home, List, Brain, Download, Settings, Menu, Building2, MessageSquare } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'

interface NavItem {
  label: string
  href: string
  icon: React.ComponentType<{ className?: string }>
  premium?: boolean
}

const navItems: NavItem[] = [
  { label: 'Dashboard', href: '/dashboard', icon: Home },
  { label: 'Compliance Q&A', href: '/dashboard/query', icon: MessageSquare },
  { label: 'Clients', href: '/dashboard/clients', icon: Building2, premium: true },
  { label: 'Findings', href: '/dashboard/findings', icon: List },
  { label: 'AI Act', href: '/dashboard/ai-act', icon: Brain, premium: true },
  { label: 'Export', href: '/dashboard/export', icon: Download, premium: true },
  { label: 'Settings', href: '/dashboard/settings', icon: Settings },
]

interface MobileNavProps {
  activePath?: string
}

export function MobileNav({ activePath }: MobileNavProps) {
  return (
    <Sheet>
      <SheetTrigger
        render={
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            aria-label="Open menu"
          />
        }
      >
        <Menu className="h-5 w-5" />
      </SheetTrigger>
      <SheetContent side="left" className="w-64 p-0">
        <SheetHeader className="border-b px-4 py-4">
          <SheetTitle>Kindlast</SheetTitle>
        </SheetHeader>
        <nav className="flex flex-col gap-1 p-4">
          <ul className="flex flex-col gap-1" role="list">
            {navItems.map((item) => {
              const isActive = activePath === item.href
              const Icon = item.icon
              return (
                <li key={item.href}>
                  <Link
                    href={item.href}
                    className={cn(
                      'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
                      'hover:bg-accent hover:text-accent-foreground',
                      isActive && 'active bg-accent text-accent-foreground'
                    )}
                  >
                    <Icon className="h-4 w-4" />
                    <span>{item.label}</span>
                    {item.premium && (
                      <span className="ml-auto rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
                        Premium
                      </span>
                    )}
                  </Link>
                </li>
              )
            })}
          </ul>
        </nav>
      </SheetContent>
    </Sheet>
  )
}
