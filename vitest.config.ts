import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./vitest.setup.ts'],
    globals: true,
    css: false,
    // Git worktrees are checked out *inside* the repo under `.claude/worktrees`,
    // so without this the runner picks up every worktree's copy of the suite as
    // well as our own. That both doubles the run and fails on whatever branch
    // state those checkouts happen to be in.
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      '**/.next/**',
      '**/.claude/worktrees/**',
    ],
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, '.'),
    },
  },
})
