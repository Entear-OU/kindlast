'use client'

import Link from 'next/link'
import { Home, List, Brain, Download, Settings, Building2, MessageSquare } from 'lucide-react'
import { cn } from '@/lib/utils'

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

interface SidebarNavProps {
  activePath?: string
}

export function SidebarNav({ activePath }: SidebarNavProps) {
  return (
    <nav className="flex flex-col gap-1 p-4">
      <div className="mb-4 px-2">
        <h2 className="text-lg font-semibold">Kindlast</h2>
      </div>
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
  )
}
