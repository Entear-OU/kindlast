# Frontend Dockerfile
# Next.js application with standalone output mode

# ====================
# Dependencies Stage
# ====================
FROM node:20-alpine AS deps

# Install libc6-compat for Alpine compatibility
RUN apk add --no-cache libc6-compat

WORKDIR /app

# Copy package files
COPY client/package.json client/pnpm-lock.yaml* ./

# Install pnpm and dependencies
RUN corepack enable pnpm \
    && pnpm install --frozen-lockfile

# ====================
# Builder Stage
# ====================
FROM node:20-alpine AS builder

WORKDIR /app

# Copy dependencies from deps stage
COPY --from=deps /app/node_modules ./node_modules
COPY client/ .

# Disable telemetry during build
ENV NEXT_TELEMETRY_DISABLED=1

# Set production environment for build optimization
ENV NODE_ENV=production

# Build the application
RUN corepack enable pnpm \
    && pnpm run build

# ====================
# Runner Stage
# ====================
FROM node:20-alpine AS runner

WORKDIR /app

# Set production environment
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME="0.0.0.0"

# Create non-root user for security
RUN addgroup --system --gid 1001 nodejs \
    && adduser --system --uid 1001 nextjs

# Copy public assets
COPY --from=builder /app/public ./public

# Set correct permissions for prerender cache
RUN mkdir .next \
    && chown nextjs:nodejs .next

# Copy standalone build output
# Automatically leverages output traces to only copy necessary files
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

# Switch to non-root user
USER nextjs

# Expose the application port
EXPOSE 3000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3000/api/health || exit 1

CMD ["node", "server.js"]
