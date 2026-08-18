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
   */
  output: 'standalone',

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
}

export default nextConfig
