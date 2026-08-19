import path from 'node:path'
import type { NextConfig } from 'next'

const nextConfig: NextConfig = {
  /**
   * The image the compose stack runs is a production build, so the build has
   * to produce something that runs without the repository around it.
   *
   * `standalone` writes `.next/standalone`, holding a `server.js` and only the
   * files the traced module graph actually reached. Without it the runtime
   * image has to carry the whole of `node_modules`, most of which exists to
   * compile output that is already compiled. `apps/web/Dockerfile` copies
   * exactly the three things this emits.
   *
   * Not on Vercel, though. Vercel's build pipeline runs its own output
   * tracing and packages the result itself; with `standalone` set, its build
   * fails on a missing `.next/next-server.js.nft.json`, because standalone
   * relocates the trace files it expects to read in place. Vercel sets
   * `VERCEL=1` in every build, the Docker build sets nothing, so each
   * environment gets the output mode its packager expects.
   */
  output: process.env.VERCEL ? undefined : 'standalone',

  /**
   * The trace root is the monorepo root, not this workspace.
   *
   * Bun hoists a workspace's dependencies into the root `node_modules`, so a
   * trace rooted at `apps/web` walks above its own root to find `next` and
   * `react`. Next then infers a root of its own, and an inferred root that
   * moves with the directory the build ran in is the sort of thing that works
   * on a laptop and drops files in an image. Naming it makes the copied tree
   * deterministic.
   */
  outputFileTracingRoot: path.join(import.meta.dirname, '..', '..'),

  /**
   * `@swc/helpers` is traced by its CommonJS half and required by its ESM one.
   *
   * Next 16.3.1's `require-hook` resolves
   * `@swc/helpers/esm/_interop_require_default.js` at runtime, while the trace
   * only reaches `cjs/`. The package ships both, so the image ends up holding
   * a `@swc/helpers` with `cjs` and `package.json` and no `esm`, and the
   * container exits 1 on its first line with a module-not-found for a file
   * that is present in `node_modules` on disk.
   *
   * It is invisible everywhere except the built image: lint, typecheck, the
   * unit suite and `next dev` all pass, because every one of them reads the
   * real `node_modules` rather than the traced copy. Caught by the compose
   * stack, which is the reason ENT-241 put the console in it.
   *
   * Naming the whole package rather than the one file, because the runtime
   * picks the helper it needs per module and the next bump will need a
   * different one.
   */
  outputFileTracingIncludes: {
    '**': [
      '../../node_modules/.bun/@swc+helpers@*/node_modules/@swc/helpers/**',
    ],
  },
}

export default nextConfig
