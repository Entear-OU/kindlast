import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock next/headers
vi.mock('next/headers', () => ({
  cookies: vi.fn(() => ({
    getAll: vi.fn(() => []),
    set: vi.fn(),
  })),
  headers: vi.fn(() => ({
    get: vi.fn(() => 'http://localhost:3000'),
  })),
}))

// Mock next/navigation
const mockRedirect = vi.fn()
vi.mock('next/navigation', () => ({
  redirect: mockRedirect,
}))

// Mock supabase server client
const mockSignUp = vi.fn()
const mockSignInWithPassword = vi.fn()
const mockSignInWithOAuth = vi.fn()
const mockSignOut = vi.fn()

vi.mock('@/lib/supabase/server', () => ({
  createClient: vi.fn(() =>
    Promise.resolve({
      auth: {
        signUp: mockSignUp,
        signInWithPassword: mockSignInWithPassword,
        signInWithOAuth: mockSignInWithOAuth,
        signOut: mockSignOut,
      },
    })
  ),
}))

describe('Auth Actions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('signUp', () => {
    it('calls supabase.auth.signUp with correct email and password', async () => {
      const { signUp } = await import('@/lib/auth/actions')

      mockSignUp.mockResolvedValue({ data: { user: { id: '123' } }, error: null })

      const formData = new FormData()
      formData.set('email', 'test@example.com')
      formData.set('password', 'password123')

      await signUp(formData)

      expect(mockSignUp).toHaveBeenCalledWith({
        email: 'test@example.com',
        password: 'password123',
      })
    })

    it('redirects to /onboarding on successful signup', async () => {
      const { signUp } = await import('@/lib/auth/actions')

      mockSignUp.mockResolvedValue({ data: { user: { id: '123' } }, error: null })

      const formData = new FormData()
      formData.set('email', 'test@example.com')
      formData.set('password', 'password123')

      await signUp(formData)

      expect(mockRedirect).toHaveBeenCalledWith('/onboarding')
    })

    it('returns error message on signup failure', async () => {
      const { signUp } = await import('@/lib/auth/actions')

      mockSignUp.mockResolvedValue({
        data: { user: null },
        error: { message: 'User already registered' },
      })

      const formData = new FormData()
      formData.set('email', 'test@example.com')
      formData.set('password', 'password123')

      const result = await signUp(formData)

      expect(result).toEqual({ error: 'User already registered' })
      expect(mockRedirect).not.toHaveBeenCalled()
    })
  })

  describe('signIn', () => {
    it('calls supabase.auth.signInWithPassword with correct params', async () => {
      const { signIn } = await import('@/lib/auth/actions')

      mockSignInWithPassword.mockResolvedValue({
        data: { user: { id: '123' } },
        error: null,
      })

      const formData = new FormData()
      formData.set('email', 'test@example.com')
      formData.set('password', 'password123')

      await signIn(formData)

      expect(mockSignInWithPassword).toHaveBeenCalledWith({
        email: 'test@example.com',
        password: 'password123',
      })
    })

    it('redirects to /onboarding on successful sign in', async () => {
      const { signIn } = await import('@/lib/auth/actions')

      mockSignInWithPassword.mockResolvedValue({
        data: { user: { id: '123' } },
        error: null,
      })

      const formData = new FormData()
      formData.set('email', 'test@example.com')
      formData.set('password', 'password123')

      await signIn(formData)

      expect(mockRedirect).toHaveBeenCalledWith('/onboarding')
    })

    it('returns error message on sign in failure', async () => {
      const { signIn } = await import('@/lib/auth/actions')

      mockSignInWithPassword.mockResolvedValue({
        data: { user: null },
        error: { message: 'Invalid login credentials' },
      })

      const formData = new FormData()
      formData.set('email', 'test@example.com')
      formData.set('password', 'wrong')

      const result = await signIn(formData)

      expect(result).toEqual({ error: 'Invalid login credentials' })
      expect(mockRedirect).not.toHaveBeenCalled()
    })
  })

  describe('signInWithGoogle', () => {
    it('calls supabase.auth.signInWithOAuth with google provider', async () => {
      const { signInWithGoogle } = await import('@/lib/auth/actions')

      mockSignInWithOAuth.mockResolvedValue({
        data: { url: 'https://accounts.google.com/oauth' },
        error: null,
      })

      await signInWithGoogle()

      expect(mockSignInWithOAuth).toHaveBeenCalledWith({
        provider: 'google',
        options: {
          redirectTo: expect.stringContaining('/auth/callback'),
        },
      })
    })

    it('redirects to OAuth URL on success', async () => {
      const { signInWithGoogle } = await import('@/lib/auth/actions')

      mockSignInWithOAuth.mockResolvedValue({
        data: { url: 'https://accounts.google.com/oauth' },
        error: null,
      })

      await signInWithGoogle()

      expect(mockRedirect).toHaveBeenCalledWith('https://accounts.google.com/oauth')
    })

    it('returns error on OAuth failure', async () => {
      const { signInWithGoogle } = await import('@/lib/auth/actions')

      mockSignInWithOAuth.mockResolvedValue({
        data: { url: null },
        error: { message: 'OAuth error' },
      })

      const result = await signInWithGoogle()

      expect(result).toEqual({ error: 'OAuth error' })
    })
  })

  describe('signOut', () => {
    it('calls supabase.auth.signOut', async () => {
      const { signOut } = await import('@/lib/auth/actions')

      mockSignOut.mockResolvedValue({ error: null })

      await signOut()

      expect(mockSignOut).toHaveBeenCalled()
    })

    it('redirects to /login after sign out', async () => {
      const { signOut } = await import('@/lib/auth/actions')

      mockSignOut.mockResolvedValue({ error: null })

      await signOut()

      expect(mockRedirect).toHaveBeenCalledWith('/login')
    })
  })
})
