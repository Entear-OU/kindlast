'use client'

import { useState, useEffect } from 'react'
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from '@/components/ui/card'
import { createPortalSession } from '@/lib/stripe/actions'

interface SubscriptionInfo {
  plan: string
  status: string
  current_period_end: string | null
}

interface ProfileInfo {
  company_name: string
  country: string
  industry: string | null
  employee_count: number | null
}

export default function SettingsPage() {
  const [subscription, setSubscription] = useState<SubscriptionInfo | null>(null)
  const [profile, setProfile] = useState<ProfileInfo | null>(null)
  const [billingLoading, setBillingLoading] = useState(false)

  useEffect(() => {
    async function loadData() {
      try {
        // These would be loaded via server component in production
        // For now, we'll use a fetch approach
        const res = await fetch('/api/settings')
        if (res.ok) {
          const data = await res.json()
          setSubscription(data.subscription)
          setProfile(data.profile)
        }
      } catch {
        // Silently handle - data will show as loading
      }
    }
    loadData()
  }, [])

  async function handleManageBilling() {
    setBillingLoading(true)
    try {
      const result = await createPortalSession()
      if (result.url) {
        window.location.href = result.url
      }
    } catch {
      // Handle error
    } finally {
      setBillingLoading(false)
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Settings</h1>
        <p className="text-muted-foreground">
          Manage your profile and subscription
        </p>
      </div>

      {/* Profile Section */}
      <Card>
        <CardHeader>
          <CardTitle>Business Profile</CardTitle>
          <CardDescription>
            Your company information used for compliance assessments
          </CardDescription>
        </CardHeader>
        <CardContent>
          {profile ? (
            <div className="space-y-3 text-sm">
              <div className="flex justify-between">
                <span className="text-muted-foreground">Company Name</span>
                <span className="font-medium">{profile.company_name}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-muted-foreground">Country</span>
                <span className="font-medium">{profile.country}</span>
              </div>
              {profile.industry && (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Industry</span>
                  <span className="font-medium">{profile.industry}</span>
                </div>
              )}
              {profile.employee_count && (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Employees</span>
                  <span className="font-medium">{profile.employee_count}</span>
                </div>
              )}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">Loading profile...</p>
          )}
        </CardContent>
        <CardFooter>
          <a
            href="/dashboard/onboarding"
            className="inline-flex h-8 items-center rounded-lg border px-3 text-sm font-medium transition-colors hover:bg-muted"
          >
            Edit Profile
          </a>
        </CardFooter>
      </Card>

      {/* Subscription Section */}
      <Card>
        <CardHeader>
          <CardTitle>Subscription</CardTitle>
          <CardDescription>
            Manage your Kindlast subscription and billing
          </CardDescription>
        </CardHeader>
        <CardContent>
          {subscription ? (
            <div className="space-y-3 text-sm">
              <div className="flex justify-between">
                <span className="text-muted-foreground">Plan</span>
                <span className="font-medium capitalize">
                  {subscription.plan}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-muted-foreground">Status</span>
                <span className="font-medium capitalize">
                  {subscription.status}
                </span>
              </div>
              {subscription.current_period_end && (
                <div className="flex justify-between">
                  <span className="text-muted-foreground">
                    Current Period Ends
                  </span>
                  <span className="font-medium">
                    {new Date(
                      subscription.current_period_end
                    ).toLocaleDateString()}
                  </span>
                </div>
              )}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">
              Loading subscription...
            </p>
          )}
        </CardContent>
        <CardFooter className="gap-2">
          {subscription?.plan === 'premium' ? (
            <button
              onClick={handleManageBilling}
              disabled={billingLoading}
              className="inline-flex h-8 items-center rounded-lg border px-3 text-sm font-medium transition-colors hover:bg-muted disabled:opacity-50"
            >
              {billingLoading ? 'Loading...' : 'Manage Billing'}
            </button>
          ) : (
            <a
              href="/pricing"
              className="inline-flex h-8 items-center rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
            >
              Upgrade to Premium
            </a>
          )}
        </CardFooter>
      </Card>
    </div>
  )
}
