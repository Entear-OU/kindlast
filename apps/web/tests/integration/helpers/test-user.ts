import { randomUUID } from 'node:crypto'

import type { SupabaseClient } from '@supabase/supabase-js'

/**
 * Test-user lifecycle helpers. Users are created via the Auth admin API
 * (service-role client) so each suite owns ephemeral identities that don't
 * collide with manual local-dev users.
 *
 * Email is namespaced (`*@kindlast.test`) so cleanup can filter without risk
 * of touching real accounts.
 */

export interface TestUser {
  id: string
  email: string
  password: string
}

export async function signUpTestUser(admin: SupabaseClient): Promise<TestUser> {
  const id = randomUUID()
  const email = `test-${id}@kindlast.test`
  const password = `pw-${id}`

  const { data, error } = await admin.auth.admin.createUser({
    email,
    password,
    email_confirm: true,
  })

  if (error || !data.user) {
    throw new Error(`signUpTestUser failed: ${error?.message ?? 'no user returned'}`)
  }

  return { id: data.user.id, email, password }
}

export async function deleteTestUser(
  admin: SupabaseClient,
  userId: string,
): Promise<void> {
  const { error } = await admin.auth.admin.deleteUser(userId)
  if (error) {
    throw new Error(`deleteTestUser(${userId}) failed: ${error.message}`)
  }
}
