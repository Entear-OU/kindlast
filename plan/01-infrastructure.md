# PRD 01 — Infrastructure

**Agent**: Infra agent  
**DEPENDS ON**: Nothing — start here  
**Produces**: Working K8s cluster, all databases running, secrets wired  

---

## Overview

Stand up the full Kindlast infrastructure: four Kubernetes namespaces, StatefulSets for Qdrant/Redis/PostgreSQL, Deployments scaffolded for app services, External Secrets Operator, and the observability stack. Everything should be idempotent — running `kubectl apply` twice produces the same result.

---

## Task 1 — Repository scaffold

Create the directory structure exactly as follows:

```
infrastructure/
├── k8s/
│   ├── namespaces.yaml
│   ├── network-policies.yaml
│   ├── app/
│   │   ├── api-gateway-deployment.yaml
│   │   ├── rag-service-deployment.yaml
│   │   ├── frontend-deployment.yaml
│   │   └── hpa.yaml
│   ├── data/
│   │   ├── qdrant-statefulset.yaml
│   │   ├── redis-statefulset.yaml
│   │   └── postgres-statefulset.yaml
│   ├── ingestion/
│   │   ├── ingestion-cronjob.yaml
│   │   └── reconcile-cronjob.yaml
│   ├── observability/
│   │   ├── prometheus-deployment.yaml
│   │   └── grafana-deployment.yaml
│   └── secrets/
│       ├── secret-store.yaml
│       └── external-secrets.yaml
├── docker/
│   ├── gateway.Dockerfile
│   ├── rag.Dockerfile
│   ├── ingestion.Dockerfile
│   └── frontend.Dockerfile
└── scripts/
    ├── dev-up.sh         # starts docker-compose for local dev
    └── seed-qdrant.sh    # creates collections on fresh cluster
```

### Acceptance criteria
- [x] Directory structure matches exactly
- [x] `ls -R infrastructure/` shows all files

---

## Task 2 — Namespaces and network policies

Create `infrastructure/k8s/namespaces.yaml`:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: kindlast-app
  labels:
    app.kubernetes.io/part-of: kindlast
---
apiVersion: v1
kind: Namespace
metadata:
  name: kindlast-data
  labels:
    app.kubernetes.io/part-of: kindlast
---
apiVersion: v1
kind: Namespace
metadata:
  name: kindlast-ingestion
  labels:
    app.kubernetes.io/part-of: kindlast
---
apiVersion: v1
kind: Namespace
metadata:
  name: kindlast-observability
  labels:
    app.kubernetes.io/part-of: kindlast
```

Create `infrastructure/k8s/network-policies.yaml`. Rules:
- `kindlast-app` pods can reach `kindlast-data` on ports 5432 (postgres), 6379 (redis), 6333 (qdrant)
- `kindlast-ingestion` pods can reach `kindlast-data` on same ports
- `kindlast-ingestion` pods can reach internet (egress) for Firecrawl, OpenAI, Cohere APIs
- `kindlast-data` pods accept inbound only from `kindlast-app` and `kindlast-ingestion`
- `kindlast-observability` can scrape metrics from all namespaces on port 9090
- No namespace can reach `kindlast-data` from the internet

```yaml
# Example: block all ingress to data namespace except from app and ingestion
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: data-ingress-policy
  namespace: kindlast-data
spec:
  podSelector: {}
  policyTypes:
  - Ingress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: kindlast-app
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: kindlast-ingestion
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: kindlast-observability
```

Write all four namespace NetworkPolicies following this pattern.

### Acceptance criteria
- [x] `kubectl apply -f k8s/namespaces.yaml` creates all four namespaces *(file created)*
- [x] `kubectl apply -f k8s/network-policies.yaml` applies without error *(file created)*
- [ ] A pod in `kindlast-app` can curl Qdrant on port 6333 *(requires running cluster)*
- [ ] A pod in `kindlast-app` cannot curl `kindlast-ingestion` services *(requires running cluster)*

---

## Task 3 — Qdrant StatefulSet

Create `infrastructure/k8s/data/qdrant-statefulset.yaml`:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: qdrant
  namespace: kindlast-data
spec:
  serviceName: qdrant-headless
  replicas: 2
  selector:
    matchLabels:
      app: qdrant
  template:
    metadata:
      labels:
        app: qdrant
    spec:
      containers:
      - name: qdrant
        image: qdrant/qdrant:v1.8.0
        ports:
        - containerPort: 6333
          name: http
        - containerPort: 6335
          name: p2p
        env:
        - name: QDRANT__CLUSTER__ENABLED
          value: "true"
        - name: QDRANT__CLUSTER__P2P__PORT
          value: "6335"
        - name: QDRANT__SERVICE__API_KEY
          valueFrom:
            secretKeyRef:
              name: qdrant-credentials
              key: api-key
        resources:
          requests:
            memory: "2Gi"
            cpu: "500m"
          limits:
            memory: "4Gi"
            cpu: "2000m"
        livenessProbe:
          httpGet:
            path: /healthz
            port: 6333
          initialDelaySeconds: 30
          periodSeconds: 15
          failureThreshold: 3
        readinessProbe:
          httpGet:
            path: /readyz
            port: 6333
          initialDelaySeconds: 10
          periodSeconds: 10
          failureThreshold: 2
        volumeMounts:
        - name: qdrant-storage
          mountPath: /qdrant/storage
  volumeClaimTemplates:
  - metadata:
      name: qdrant-storage
    spec:
      accessModes: ["ReadWriteOnce"]
      storageClassName: local-path
      resources:
        requests:
          storage: 20Gi
---
apiVersion: v1
kind: Service
metadata:
  name: qdrant
  namespace: kindlast-data
spec:
  selector:
    app: qdrant
  ports:
  - port: 6333
    targetPort: 6333
    name: http
  - port: 6335
    targetPort: 6335
    name: p2p
---
apiVersion: v1
kind: Service
metadata:
  name: qdrant-headless
  namespace: kindlast-data
spec:
  clusterIP: None
  selector:
    app: qdrant
  ports:
  - port: 6333
    name: http
  - port: 6335
    name: p2p
```

### Acceptance criteria
- [x] Qdrant StatefulSet manifest created with 2 replicas *(file created)*
- [ ] `kubectl get pods -n kindlast-data` shows 2 qdrant pods Running *(requires running cluster)*
- [ ] `kubectl exec` into qdrant-0 and curl `localhost:6333/healthz` returns 200 *(requires running cluster)*
- [ ] Cluster mode is active: `curl qdrant:6333/cluster` shows 2 peers *(requires running cluster)*

---

## Task 4 — Redis StatefulSet (Sentinel mode)

Create `infrastructure/k8s/data/redis-statefulset.yaml` with:
- 1 master + 2 replicas via StatefulSet
- Sentinel sidecar for automatic failover
- PVC per pod: 5Gi
- Password auth via secret

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redis
  namespace: kindlast-data
spec:
  serviceName: redis-headless
  replicas: 3
  selector:
    matchLabels:
      app: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      initContainers:
      - name: config
        image: redis:7-alpine
        command: ["sh", "-c"]
        args:
        - |
          # configure master vs replica based on pod ordinal
          if [ "$(hostname)" = "redis-0" ]; then
            cp /config/master.conf /data/redis.conf
          else
            cp /config/replica.conf /data/redis.conf
          fi
        volumeMounts:
        - name: redis-config
          mountPath: /config
        - name: redis-data
          mountPath: /data
      containers:
      - name: redis
        image: redis:7-alpine
        command: ["redis-server", "/data/redis.conf"]
        env:
        - name: REDIS_PASSWORD
          valueFrom:
            secretKeyRef:
              name: redis-credentials
              key: password
        ports:
        - containerPort: 6379
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          exec:
            command: ["redis-cli", "ping"]
          initialDelaySeconds: 15
          periodSeconds: 10
        volumeMounts:
        - name: redis-data
          mountPath: /data
      volumes:
      - name: redis-config
        configMap:
          name: redis-config
  volumeClaimTemplates:
  - metadata:
      name: redis-data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 5Gi
```

Also create the ConfigMap with `master.conf` and `replica.conf` content. Master config includes:
- `maxmemory 512mb`
- `maxmemory-policy allkeys-lru`
- `requirepass ${REDIS_PASSWORD}`
- `save 60 1000` (RDB persistence)

Replica config adds:
- `replicaof redis-0.redis-headless.kindlast-data.svc.cluster.local 6379`
- `masterauth ${REDIS_PASSWORD}`

### Acceptance criteria
- [x] Redis StatefulSet manifest created with 3 replicas + ConfigMap *(file created)*
- [ ] 3 Redis pods Running in `kindlast-data` *(requires running cluster)*
- [ ] `redis-cli -h redis-0... ping` returns PONG *(requires running cluster)*
- [ ] Deleting `redis-0` pod causes `redis-1` to become master within 60s *(requires running cluster)*

---

## Task 5 — PostgreSQL StatefulSet

Create `infrastructure/k8s/data/postgres-statefulset.yaml`:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: kindlast-data
spec:
  serviceName: postgres-headless
  replicas: 2
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
      - name: postgres
        image: postgres:16-alpine
        env:
        - name: POSTGRES_DB
          value: kindlast
        - name: POSTGRES_USER
          value: kindlast
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: postgres-credentials
              key: password
        - name: PGDATA
          value: /var/lib/postgresql/data/pgdata
        ports:
        - containerPort: 5432
        resources:
          requests:
            memory: "512Mi"
            cpu: "250m"
          limits:
            memory: "2Gi"
            cpu: "1000m"
        livenessProbe:
          exec:
            command: ["pg_isready", "-U", "kindlast"]
          initialDelaySeconds: 30
          periodSeconds: 15
        volumeMounts:
        - name: postgres-data
          mountPath: /var/lib/postgresql/data
  volumeClaimTemplates:
  - metadata:
      name: postgres-data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 10Gi
```

### Database schema

Create `infrastructure/k8s/data/postgres-init.sql`. Run this as a K8s Job after StatefulSet is ready:

```sql
-- Users and auth
CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT UNIQUE NOT NULL,
  email_hash TEXT NOT NULL,           -- sha256 for logging (never store plain in logs)
  plan TEXT NOT NULL DEFAULT 'free',  -- free | premium | api
  stripe_customer_id TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Subscriptions
CREATE TABLE subscriptions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES users(id) ON DELETE CASCADE,
  stripe_subscription_id TEXT UNIQUE,
  status TEXT NOT NULL,               -- active | cancelled | past_due
  current_period_end TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Parent chunks (large context for generation)
CREATE TABLE parent_chunks (
  id TEXT PRIMARY KEY,                -- deterministic UUID from doc_id + chunk_index
  doc_id TEXT NOT NULL,
  source_url TEXT NOT NULL,
  text TEXT NOT NULL,
  document_title TEXT,
  scraped_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_parent_chunks_doc_id ON parent_chunks(doc_id);

-- Ingestion log (per-document, per-run)
CREATE TABLE ingestion_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  doc_id TEXT NOT NULL,
  source_url TEXT NOT NULL,
  chunk_count INT,
  content_hash TEXT,
  status TEXT NOT NULL,               -- success | failed | skipped
  error_message TEXT,
  run_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_ingestion_log_doc_id ON ingestion_log(doc_id);
CREATE INDEX idx_ingestion_log_run_at ON ingestion_log(run_at);

-- Query audit log
CREATE TABLE query_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES users(id),
  query_hash TEXT NOT NULL,           -- sha256 of normalized query
  provider_used TEXT,                 -- claude | gpt-4o
  cache_hit BOOLEAN DEFAULT FALSE,
  chunk_count INT,
  latency_ms INT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- User feedback
CREATE TABLE response_feedback (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  query_hash TEXT NOT NULL,
  user_id UUID REFERENCES users(id),
  rating SMALLINT CHECK (rating IN (-1, 1)),
  comment TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Dead letter queue for failed ingestion
CREATE TABLE ingestion_dead_letter (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source_url TEXT NOT NULL,
  failure_count INT DEFAULT 1,
  last_error TEXT,
  last_attempted_at TIMESTAMPTZ DEFAULT NOW(),
  resolved BOOLEAN DEFAULT FALSE
);
```

### Acceptance criteria
- [x] PostgreSQL StatefulSet manifest created *(file created)*
- [x] Init SQL file created with 7 tables and indexes *(file created)*
- [ ] PostgreSQL pod Running and `pg_isready` passes *(requires running cluster)*
- [ ] Init SQL runs successfully via K8s Job *(requires running cluster)*
- [ ] All tables exist: `\dt` shows 7 tables *(requires running cluster)*
- [ ] `EXPLAIN SELECT * FROM parent_chunks WHERE doc_id = '...'` uses the index *(requires running cluster)*

---

## Task 6 — External Secrets Operator

Install ESO via Helm:

```bash
helm repo add external-secrets https://charts.external-secrets.io
helm install external-secrets external-secrets/external-secrets \
  -n external-secrets \
  --create-namespace \
  --set installCRDs=true
```

Create `infrastructure/k8s/secrets/secret-store.yaml` pointing to HashiCorp Vault (or for local dev, use a Kubernetes SecretStore pointing to real K8s secrets):

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ClusterSecretStore
metadata:
  name: vault-backend
spec:
  provider:
    vault:
      server: "http://vault.vault.svc.cluster.local:8200"
      path: "secret"
      version: "v2"
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "kindlast"
          serviceAccountRef:
            name: "kindlast-vault-auth"
            namespace: "kindlast-app"
```

Create `infrastructure/k8s/secrets/external-secrets.yaml` with ExternalSecret resources for:

```yaml
# AI provider keys — used by both app and ingestion namespaces
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: ai-provider-keys
  namespace: kindlast-app
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend
    kind: ClusterSecretStore
  target:
    name: ai-provider-keys
  data:
  - secretKey: anthropic-api-key
    remoteRef:
      key: kindlast/ai-providers
      property: anthropic_api_key
  - secretKey: openai-api-key
    remoteRef:
      key: kindlast/ai-providers
      property: openai_api_key
  - secretKey: cohere-api-key
    remoteRef:
      key: kindlast/ai-providers
      property: cohere_api_key
  - secretKey: firecrawl-api-key
    remoteRef:
      key: kindlast/ai-providers
      property: firecrawl_api_key
```

Repeat for `kindlast-ingestion` namespace. Also create ExternalSecrets for:
- `postgres-credentials` (password)
- `redis-credentials` (password)
- `qdrant-credentials` (api-key)
- `jwt-secret` (jwt_secret)
- `stripe-keys` (secret_key, webhook_secret, premium_price_id)

### Acceptance criteria
- [x] ClusterSecretStore manifest created for Vault *(file created)*
- [x] ExternalSecret manifests created for all secrets *(file created)*
- [ ] ESO pods Running in `external-secrets` namespace *(requires Helm install)*
- [ ] `kubectl get externalsecret -n kindlast-app` shows all secrets Synced *(requires running cluster)*
- [ ] `kubectl get secret ai-provider-keys -n kindlast-app -o jsonpath='{.data.anthropic-api-key}'` returns a base64 value *(requires running cluster)*

---

## Task 7 — Observability stack

Create `infrastructure/k8s/observability/prometheus-deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: prometheus
  namespace: kindlast-observability
spec:
  replicas: 1
  selector:
    matchLabels:
      app: prometheus
  template:
    spec:
      containers:
      - name: prometheus
        image: prom/prometheus:v2.50.0
        args:
        - '--config.file=/etc/prometheus/prometheus.yml'
        - '--storage.tsdb.retention.time=30d'
        ports:
        - containerPort: 9090
        volumeMounts:
        - name: config
          mountPath: /etc/prometheus
        - name: data
          mountPath: /prometheus
      volumes:
      - name: config
        configMap:
          name: prometheus-config
      - name: data
        emptyDir: {}
```

Prometheus config (`prometheus.yml`) scrapes:
- All pods with annotation `prometheus.io/scrape: "true"` across all namespaces
- Qdrant metrics endpoint at `:6333/metrics`
- Redis exporter
- Node exporter (if available)

Create Grafana deployment with pre-configured dashboards for:
- RAG service: p50/p95/p99 query latency, cache hit rate, provider usage
- Ingestion: documents processed/failed per run, chunk count per source
- Infrastructure: pod CPU/memory, PVC usage, error rates

### Acceptance criteria
- [x] Prometheus deployment manifest created with ConfigMap, PVC, RBAC *(file created)*
- [x] Grafana deployment manifest created with datasource provisioning *(file created)*
- [ ] Prometheus accessible at `http://prometheus.kindlast-observability:9090` *(requires running cluster)*
- [ ] Grafana accessible at `http://grafana.kindlast-observability:3000` *(requires running cluster)*
- [ ] RAG service metrics appearing in Prometheus within 60s of service start *(requires running cluster)*
- [ ] Alert fires when ingestion CronJob fails *(requires running cluster)*

---

## Task 8 — Dockerfiles

### Go services (gateway + rag) — scratch base

`infrastructure/docker/gateway.Dockerfile`:

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY services/gateway/go.mod services/gateway/go.sum ./
RUN go mod download
COPY services/gateway/ .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s -X main.version=$(git describe --tags --always)" \
    -o gateway ./cmd/gateway

FROM scratch
COPY --from=builder /app/gateway /gateway
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8080
USER 1001
ENTRYPOINT ["/gateway"]
```

`infrastructure/docker/rag.Dockerfile` — identical pattern, swap `gateway` for `rag`.

### Python ingestion — slim base

`infrastructure/docker/ingestion.Dockerfile`:

```dockerfile
FROM python:3.12-slim AS base

# install OS deps needed by unstructured and presidio
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgomp1 tesseract-ocr poppler-utils \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# deps first for layer caching
COPY services/ingestion/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt \
    && python -m spacy download en_core_web_lg \
    && pip uninstall -y pip setuptools wheel

# non-root user
RUN useradd -r -u 1001 -g root ingestion
USER ingestion

COPY --chown=ingestion:root services/ingestion/ .

CMD ["python", "-m", "src.main"]
```

### Next.js frontend — standalone mode

`infrastructure/docker/frontend.Dockerfile`:

```dockerfile
FROM node:20-alpine AS deps
WORKDIR /app
COPY frontend/package*.json ./
RUN npm ci

FROM node:20-alpine AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY frontend/ .
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
RUN addgroup -S nextjs && adduser -S nextjs -G nextjs
COPY --from=builder --chown=nextjs:nextjs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nextjs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nextjs /app/public ./public
USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
```

**Note**: `frontend/next.config.js` must include `output: 'standalone'`.

### Acceptance criteria
- [x] gateway.Dockerfile created (scratch base, Go 1.23) *(file created)*
- [x] rag.Dockerfile created (scratch base, Go 1.23) *(file created)*
- [x] ingestion.Dockerfile created (python:3.12-slim, spaCy) *(file created)*
- [x] frontend.Dockerfile created (node:20-alpine, standalone) *(file created)*
- [x] All Dockerfiles use non-root user (UID 1001) *(configured)*
- [ ] `docker build -f infrastructure/docker/gateway.Dockerfile -t kindlast/gateway .` succeeds, image <15MB *(requires service code)*
- [ ] `docker build -f infrastructure/docker/ingestion.Dockerfile -t kindlast/ingestion .` succeeds *(requires service code)*
- [ ] `docker build -f infrastructure/docker/frontend.Dockerfile -t kindlast/frontend .` succeeds *(requires service code)*
- [ ] `docker scout cves kindlast/gateway` — no CRITICAL vulnerabilities *(requires built image)*

---

## Task 9 — Local development docker-compose

Create `docker-compose.yml` at repo root for local development:

```yaml
version: '3.9'

services:
  qdrant:
    image: qdrant/qdrant:v1.8.0
    ports:
      - "6333:6333"
    volumes:
      - qdrant_data:/qdrant/storage

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    command: redis-server --maxmemory 256mb --maxmemory-policy allkeys-lru

  postgres:
    image: postgres:16-alpine
    ports:
      - "5432:5432"
    environment:
      POSTGRES_DB: kindlast
      POSTGRES_USER: kindlast
      POSTGRES_PASSWORD: localdev
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./infrastructure/k8s/data/postgres-init.sql:/docker-entrypoint-initdb.d/init.sql

  gateway:
    build:
      context: .
      dockerfile: infrastructure/docker/gateway.Dockerfile
    ports:
      - "8080:8080"
    env_file: .env.local
    depends_on: [postgres, redis]

  rag:
    build:
      context: .
      dockerfile: infrastructure/docker/rag.Dockerfile
    ports:
      - "8081:8081"
    env_file: .env.local
    depends_on: [qdrant, redis]

  ingestion:
    build:
      context: .
      dockerfile: infrastructure/docker/ingestion.Dockerfile
    env_file: .env.local
    depends_on: [qdrant, postgres]
    profiles: ["ingestion"]   # only start when explicitly requested

  frontend:
    build:
      context: .
      dockerfile: infrastructure/docker/frontend.Dockerfile
    ports:
      - "3000:3000"
    environment:
      NEXT_PUBLIC_API_URL: http://localhost:8080
    depends_on: [gateway]

volumes:
  qdrant_data:
  postgres_data:
```

Create `scripts/dev-up.sh`:
```bash
#!/bin/bash
set -e
echo "Starting Kindlast local dev environment..."
cp .env.example .env.local
docker compose up -d qdrant redis postgres
echo "Waiting for databases..."
sleep 5
bash scripts/seed-qdrant.sh
docker compose up -d gateway rag frontend
echo "Ready at http://localhost:3000"
```

Create `scripts/seed-qdrant.sh` — creates Qdrant collections:
```bash
#!/bin/bash
# Create collection for OpenAI embeddings (primary)
curl -X PUT http://localhost:6333/collections/kindlast_openai_prod \
  -H 'Content-Type: application/json' \
  -d '{
    "vectors": {"size": 3072, "distance": "Cosine"},
    "sparse_vectors": {"bm25": {"modifier": "idf"}},
    "replication_factor": 1
  }'

# Create collection for Cohere embeddings (fallback)
curl -X PUT http://localhost:6333/collections/kindlast_cohere_prod \
  -H 'Content-Type: application/json' \
  -d '{
    "vectors": {"size": 1024, "distance": "Cosine"},
    "sparse_vectors": {"bm25": {"modifier": "idf"}},
    "replication_factor": 1
  }'

echo "Qdrant collections created"
```

### Acceptance criteria
- [x] docker-compose.yml created at repo root *(file created)*
- [x] dev-up.sh script created *(file created)*
- [x] seed-qdrant.sh script created *(file created)*
- [ ] `bash scripts/dev-up.sh` starts all services without error *(requires service code)*
- [ ] `curl http://localhost:8080/healthz` returns `{"status":"ok"}` *(requires service code)*
- [ ] `curl http://localhost:3000` returns the Next.js homepage *(requires service code)*
- [ ] Qdrant collections exist: `curl http://localhost:6333/collections` shows both *(requires running containers)*
