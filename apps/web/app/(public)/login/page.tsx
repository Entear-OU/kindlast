import { redirect } from 'next/navigation'

/**
 * The old Supabase sign-in form lived here.
 *
 * Kept as a redirect rather than deleted, because `/login` is in bookmarks, in
 * links that have been shared, and in every `redirect('/login')` the console
 * pages still carry. A 404 for someone trying to sign in is the worst possible
 * answer; a redirect costs one hop and nobody notices.
 *
 * Delete it once nothing arrives here any more.
 */
export default function LoginPage() {
  redirect('/sign-in')
}
