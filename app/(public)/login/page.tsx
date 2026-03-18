'use client'

import { useState } from 'react'
import { signIn, signUp } from '@/lib/auth/actions'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

export default function LoginPage() {
  const [activeTab, setActiveTab] = useState<'login' | 'signup'>('login')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  async function handleSubmit(formData: FormData) {
    setError(null)
    setLoading(true)

    try {
      const action = activeTab === 'login' ? signIn : signUp
      const result = await action(formData)
      if (result?.error) {
        setError(result.error)
      }
    } catch {
      setError('An unexpected error occurred')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center px-4">
      <Card className="w-full max-w-md">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl font-bold">Kindlast</CardTitle>
          <CardDescription>
            AI-powered GDPR & EU AI Act compliance
          </CardDescription>
        </CardHeader>
        <CardContent>
          {/* Tabs */}
          <div className="mb-6" role="tablist">
            <div className="grid w-full grid-cols-2 gap-1 rounded-lg bg-muted p-1">
              <button
                role="tab"
                aria-selected={activeTab === 'login'}
                className={`rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                  activeTab === 'login'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
                onClick={() => {
                  setActiveTab('login')
                  setError(null)
                }}
              >
                Login
              </button>
              <button
                role="tab"
                aria-selected={activeTab === 'signup'}
                className={`rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                  activeTab === 'signup'
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
                onClick={() => {
                  setActiveTab('signup')
                  setError(null)
                }}
              >
                Sign Up
              </button>
            </div>
          </div>

          {/* Error message */}
          {error && (
            <div className="mb-4 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
              {error}
            </div>
          )}

          {/* Email/Password form */}
          <form action={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                name="email"
                type="email"
                placeholder="you@example.com"
                required
                disabled={loading}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                name="password"
                type="password"
                placeholder="••••••••"
                required
                minLength={6}
                disabled={loading}
              />
            </div>
            <Button type="submit" className="w-full" disabled={loading}>
              {loading
                ? 'Loading...'
                : activeTab === 'login'
                  ? 'Sign In'
                  : 'Create Account'}
            </Button>
          </form>

        </CardContent>
      </Card>
    </div>
  )
}
