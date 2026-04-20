'use server'

import { redirect } from 'next/navigation'
import {
  loginAndStore,
  registerAndStore,
  clearGatewayTokens,
  GatewayAuthError,
} from '@/lib/api'

export async function signUp(formData: FormData) {
  const email = formData.get('email') as string
  const password = formData.get('password') as string
  const fullName = formData.get('full_name') as string | null

  if (!email || !password) {
    return { error: 'Email and password are required' }
  }

  try {
    await registerAndStore({
      email,
      password,
      full_name: fullName || undefined,
    })
  } catch (error) {
    if (error instanceof GatewayAuthError) {
      return { error: error.message }
    }
    return { error: 'Registration failed. Please try again.' }
  }

  redirect('/dashboard')
}

export async function signIn(formData: FormData) {
  const email = formData.get('email') as string
  const password = formData.get('password') as string

  if (!email || !password) {
    return { error: 'Email and password are required' }
  }

  try {
    await loginAndStore({ email, password })
  } catch (error) {
    if (error instanceof GatewayAuthError) {
      // Provide user-friendly error messages
      if (error.status === 401) {
        return { error: 'Invalid email or password' }
      }
      return { error: error.message }
    }
    return { error: 'Login failed. Please try again.' }
  }

  redirect('/dashboard')
}

export async function signInWithGoogle() {
  // OAuth support requires Gateway-side implementation
  // For now, return an error indicating OAuth is not yet supported
  return { error: 'Google sign-in is not yet available. Please use email/password.' }
}

export async function signOut() {
  await clearGatewayTokens()
  redirect('/login')
}
