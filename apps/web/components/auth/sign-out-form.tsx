import { csrfToken } from '@/lib/auth/csrf'

/**
 * Sign out.
 *
 * A form that POSTs, never a link. A `GET /auth/logout` is the same bug class
 * as a one-tap link in an email: prefetchers, mail scanners and security
 * appliances all issue GETs, and any of them would end a session someone is in
 * the middle of using (§1.7).
 *
 * The CSRF token is minted here, in a server component, so the cookie and the
 * rendered field are always the same value.
 */
export async function SignOutForm({
  className,
  children,
}: {
  className?: string
  children: React.ReactNode
}) {
  const token = await csrfToken()

  return (
    <form action="/auth/logout" method="post" className={className}>
      <input type="hidden" name="csrf" value={token} />
      {children}
    </form>
  )
}
