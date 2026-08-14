import { redirect } from 'next/navigation'

/**
 * The old Supabase sign-in form lived here.
 *
 * Kept as a redirect rather than deleted, because `/login` is in bookmarks and
 * in links that have been shared. A 404 for someone trying to sign in is the
 * worst possible answer; a redirect costs one hop and nobody notices.
 *
 * The console pages that used to `redirect('/login')` are gone (ENT-200), so
 * inbound traffic is now external only. That makes this cheaper to keep than
 * to reason about deleting: leave it until the logs say nothing arrives.
 */
export default function LoginPage() {
  redirect('/sign-in')
}
