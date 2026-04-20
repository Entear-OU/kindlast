import { createBrowserClient } from '@supabase/ssr'

// Check if Supabase is configured
const supabaseUrl = process.env.NEXT_PUBLIC_SUPABASE_URL || process.env.SUPABASE_URL
const supabaseKey = process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY || process.env.SUPABASE_ANON_KEY

export function createClient() {
  if (!supabaseUrl || !supabaseKey) {
    // Return a stub client when Supabase is not configured
    // This allows the app to run without Supabase for local development
    return null
  }

  return createBrowserClient(supabaseUrl, supabaseKey)
}
