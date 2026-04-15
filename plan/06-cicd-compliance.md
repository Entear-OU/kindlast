# PRD 06 — CI/CD and Compliance Scanner

**Agent**: DevOps agent  
**DEPENDS ON**: `03-rag-service.md` (Kindlast API available), `04-api-gateway.md`  
**Produces**: Full CI/CD pipeline, Docker builds, compliance GitHub Action, PII scanner, Slack notifier  

---

## Overview

Three GitHub Actions workflows: (1) CI — test, lint, build on every PR; (2) CD — deploy to K8s on merge to main; (3) Compliance scan — PII detection and Kindlast RAG assessment on every PR touching Go/Python/TypeScript/SQL files.

---

## Directory structure

```
.github/
├── workflows/
│   ├── ci.yml                      # test + lint + docker build
│   ├── cd.yml                      # deploy to K8s
│   └── compliance-scan.yml         # PII + Kindlast assessment + Slack
└── scripts/
    ├── pii_scan.py                 # Presidio + regex PII detector
    ├── compliance_assess.py        # calls Kindlast API
    ├── pii_redactor.py             # generates fix suggestions
    └── slack_notify.py             # formats and sends Slack message
```

---

## Task 1 — CI workflow

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  pull_request:
    types: [opened, synchronize]
  push:
    branches: [main]

jobs:
  test-go:
    name: Test Go services
    runs-on: ubuntu-latest
    strategy:
      matrix:
        service: [gateway, rag]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
          cache-dependency-path: services/${{ matrix.service }}/go.sum

      - name: Download deps
        run: go mod download
        working-directory: services/${{ matrix.service }}

      - name: Lint
        uses: golangci/golangci-lint-action@v6
        with:
          working-directory: services/${{ matrix.service }}
          version: latest

      - name: Test
        run: go test ./... -race -coverprofile=coverage.out
        working-directory: services/${{ matrix.service }}

      - name: Coverage check
        run: |
          COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | tr -d '%')
          if (( $(echo "$COVERAGE < 70" | bc -l) )); then
            echo "Coverage $COVERAGE% below 70% threshold"
            exit 1
          fi
        working-directory: services/${{ matrix.service }}

  test-python:
    name: Test Python ingestion
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-python@v5
        with:
          python-version: '3.12'
          cache: 'pip'
          cache-dependency-path: services/ingestion/requirements.txt

      - name: Install
        run: pip install -r requirements.txt pytest pytest-cov ruff
        working-directory: services/ingestion

      - name: Lint
        run: ruff check src/
        working-directory: services/ingestion

      - name: Test
        run: pytest tests/ --cov=src --cov-report=term-missing --cov-fail-under=70
        working-directory: services/ingestion

  test-frontend:
    name: Test Frontend
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'
          cache-dependency-path: frontend/package-lock.json

      - name: Install
        run: npm ci
        working-directory: frontend

      - name: Type check
        run: npx tsc --noEmit
        working-directory: frontend

      - name: Lint
        run: npm run lint
        working-directory: frontend

      - name: Build
        run: npm run build
        working-directory: frontend

  docker-build:
    name: Build Docker images
    runs-on: ubuntu-latest
    needs: [test-go, test-python, test-frontend]
    strategy:
      matrix:
        include:
          - service: gateway
            dockerfile: infrastructure/docker/gateway.Dockerfile
          - service: rag
            dockerfile: infrastructure/docker/rag.Dockerfile
          - service: ingestion
            dockerfile: infrastructure/docker/ingestion.Dockerfile
          - service: frontend
            dockerfile: infrastructure/docker/frontend.Dockerfile
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3

      - name: Build
        uses: docker/build-push-action@v5
        with:
          context: .
          file: ${{ matrix.dockerfile }}
          push: false
          tags: kindlast/${{ matrix.service }}:${{ github.sha }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Security scan
        uses: docker/scout-action@v1
        with:
          command: cves
          image: kindlast/${{ matrix.service }}:${{ github.sha }}
          only-severities: critical
          exit-code: true
```

---

## Task 2 — CD workflow

Create `.github/workflows/cd.yml`:

```yaml
name: CD

on:
  push:
    branches: [main]

env:
  REGISTRY: ghcr.io
  IMAGE_PREFIX: ghcr.io/${{ github.repository_owner }}/kindlast

jobs:
  build-and-push:
    name: Build and push images
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    strategy:
      matrix:
        include:
          - service: gateway
            dockerfile: infrastructure/docker/gateway.Dockerfile
          - service: rag
            dockerfile: infrastructure/docker/rag.Dockerfile
          - service: ingestion
            dockerfile: infrastructure/docker/ingestion.Dockerfile
          - service: frontend
            dockerfile: infrastructure/docker/frontend.Dockerfile
    steps:
      - uses: actions/checkout@v4

      - name: Login to GHCR
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build and push
        uses: docker/build-push-action@v5
        with:
          context: .
          file: ${{ matrix.dockerfile }}
          push: true
          tags: |
            ${{ env.IMAGE_PREFIX }}-${{ matrix.service }}:${{ github.sha }}
            ${{ env.IMAGE_PREFIX }}-${{ matrix.service }}:latest

  deploy:
    name: Deploy to K8s
    runs-on: ubuntu-latest
    needs: build-and-push
    environment: production
    steps:
      - uses: actions/checkout@v4

      - name: Set up kubectl
        uses: azure/setup-kubectl@v3

      - name: Configure kubeconfig
        run: |
          echo "${{ secrets.KUBECONFIG }}" | base64 -d > /tmp/kubeconfig
          echo "KUBECONFIG=/tmp/kubeconfig" >> $GITHUB_ENV

      - name: Update image tags
        run: |
          # Update image tags in K8s manifests
          for service in gateway rag ingestion frontend; do
            kubectl set image deployment/$service-deployment \
              $service=${{ env.IMAGE_PREFIX }}-$service:${{ github.sha }} \
              -n kindlast-app --record || true
          done

      - name: Wait for rollout
        run: |
          kubectl rollout status deployment/api-gateway -n kindlast-app --timeout=300s
          kubectl rollout status deployment/rag-service -n kindlast-app --timeout=300s
          kubectl rollout status deployment/frontend -n kindlast-app --timeout=300s

      - name: Smoke test
        run: |
          API_URL="${{ secrets.API_URL }}"
          STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/healthz")
          if [ "$STATUS" != "200" ]; then
            echo "Smoke test failed: /healthz returned $STATUS"
            kubectl rollout undo deployment/api-gateway -n kindlast-app
            kubectl rollout undo deployment/rag-service -n kindlast-app
            exit 1
          fi
          echo "Smoke test passed"

      - name: Notify Slack on success
        if: success()
        run: |
          curl -X POST "${{ secrets.SLACK_DEPLOY_WEBHOOK }}" \
            -H 'Content-Type: application/json' \
            -d "{\"text\":\"Kindlast deployed successfully — ${{ github.sha }}\"}"

      - name: Notify Slack on failure
        if: failure()
        run: |
          curl -X POST "${{ secrets.SLACK_DEPLOY_WEBHOOK }}" \
            -H 'Content-Type: application/json' \
            -d "{\"text\":\":red_circle: Kindlast deploy failed — ${{ github.sha }} — rolled back\"}"
```

---

## Task 3 — Compliance scan workflow

Create `.github/workflows/compliance-scan.yml`:

```yaml
name: Compliance scan

on:
  pull_request:
    types: [opened, synchronize]
    paths:
      - '**.go'
      - '**.py'
      - '**.ts'
      - '**.tsx'
      - '**.sql'

jobs:
  scan:
    runs-on: ubuntu-latest
    permissions:
      pull-requests: write
      contents: read

    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Generate diff
        id: diff
        run: |
          git diff origin/${{ github.base_ref }}...HEAD \
            --diff-filter=AM \
            -- '*.go' '*.ts' '*.tsx' '*.py' '*.sql' \
            > diff.patch
          LINES=$(wc -l < diff.patch)
          echo "has_changes=$([ $LINES -gt 0 ] && echo true || echo false)" >> $GITHUB_OUTPUT

      - name: Set up Python
        if: steps.diff.outputs.has_changes == 'true'
        uses: actions/setup-python@v5
        with:
          python-version: '3.12'
          cache: 'pip'

      - name: Install scanner deps
        if: steps.diff.outputs.has_changes == 'true'
        run: |
          pip install presidio-analyzer presidio-anonymizer spacy requests
          python -m spacy download en_core_web_lg

      - name: Run PII scan
        id: pii
        if: steps.diff.outputs.has_changes == 'true'
        run: python .github/scripts/pii_scan.py diff.patch
        env:
          SCAN_OUTPUT: pii_findings.json

      - name: Run compliance assessment
        id: compliance
        if: steps.diff.outputs.has_changes == 'true'
        run: python .github/scripts/compliance_assess.py diff.patch pii_findings.json
        env:
          KINDLAST_API_KEY: ${{ secrets.KINDLAST_API_KEY }}
          KINDLAST_API_URL: ${{ secrets.KINDLAST_API_URL }}
          ASSESSMENT_OUTPUT: assessment.json

      - name: Post PR comment
        if: steps.diff.outputs.has_changes == 'true' && github.event_name == 'pull_request'
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');

            // skip if no assessment output
            if (!fs.existsSync('assessment.json')) {
              console.log('No assessment output — skipping comment');
              return;
            }

            const assessment = JSON.parse(fs.readFileSync('assessment.json', 'utf8'));
            const severity = assessment.severity;
            const pii = assessment.pii_findings || [];
            const findings = assessment.compliance_findings || [];

            const EMOJI = { BLOCK: '🔴', WARN: '🟡', INFO: '🔵', PASS: '✅' };

            let body = `<!-- kindlast-compliance -->\n`;
            body += `## ${EMOJI[severity]} Kindlast compliance scan — ${severity}\n\n`;

            if (severity === 'PASS') {
              body += `No compliance issues detected in this PR.\n`;
            } else {
              body += `| | |\n|---|---|\n`;
              body += `| PII findings | ${pii.length} |\n`;
              body += `| Compliance findings | ${findings.length} |\n`;
              body += `| Blocking | ${severity === 'BLOCK' ? 'Yes ⛔' : 'No'} |\n\n`;

              if (pii.length > 0) {
                body += `### PII detected\n`;
                pii.slice(0, 5).forEach(f => {
                  body += `- \`${f.file}:${f.line}\` — ${f.type} (${f.severity})\n`;
                });
                if (pii.length > 5) {
                  body += `- _...and ${pii.length - 5} more_\n`;
                }
                body += '\n';
              }

              findings.slice(0, 3).forEach(f => {
                const emoji = f.severity === 'BLOCK' ? '🔴' : f.severity === 'WARN' ? '🟡' : '🔵';
                body += `### ${emoji} ${f.title || 'Finding'}\n`;
                body += `${f.detail || ''}\n`;
                if (f.citation) {
                  body += `> _${f.citation}_\n`;
                }
                body += '\n';
              });
            }

            body += `---\n<sub>Powered by [Kindlast](https://kindlast.com) · Scan ID: ${Date.now()}</sub>`;

            // delete old compliance comments
            const { data: comments } = await github.rest.issues.listComments({
              owner: context.repo.owner,
              repo: context.repo.repo,
              issue_number: context.issue.number,
            });
            for (const c of comments) {
              if (c.body?.includes('<!-- kindlast-compliance -->')) {
                await github.rest.issues.deleteComment({
                  owner: context.repo.owner,
                  repo: context.repo.repo,
                  comment_id: c.id,
                });
              }
            }

            await github.rest.issues.createComment({
              owner: context.repo.owner,
              repo: context.repo.repo,
              issue_number: context.issue.number,
              body,
            });

            if (severity === 'BLOCK') {
              core.setFailed('Compliance scan blocked: critical issues found. See PR comment.');
            }

      - name: Notify Slack
        if: >
          steps.diff.outputs.has_changes == 'true' &&
          steps.compliance.outcome == 'success'
        run: python .github/scripts/slack_notify.py assessment.json
        env:
          SLACK_WEBHOOK: ${{ secrets.SLACK_COMPLIANCE_WEBHOOK }}
          PR_URL: ${{ github.event.pull_request.html_url }}
          PR_TITLE: ${{ github.event.pull_request.title }}
          AUTHOR: ${{ github.event.pull_request.user.login }}
```

---

## Task 4 — PII scanner

Create `.github/scripts/pii_scan.py`:

```python
#!/usr/bin/env python3
"""
Scans git diff for PII in added lines using regex patterns + Presidio NLP.
Outputs findings to SCAN_OUTPUT env var path.
"""
import sys, json, re, os
from presidio_analyzer import AnalyzerEngine
from presidio_analyzer.nlp_engine import NlpEngineProvider

CODE_PII_PATTERNS = [
    (r'["\']([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})["\']',
     'EMAIL', 'BLOCK'),
    (r'\b([A-Z]{2}\d{2}[A-Z0-9]{4}\d{7,19})\b',
     'IBAN', 'BLOCK'),
    (r'(?i)(password|passwd|secret|api_key)\s*[=:]\s*["\'][^"\']{6,}["\']',
     'HARDCODED_SECRET', 'BLOCK'),
    (r'["\'](\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})["\']',
     'IP_ADDRESS', 'WARN'),
    (r'(?i)(log|print|fmt\.Print|console\.log)\s*\(.*\b(email|password|token|ssn)\b',
     'PERSONAL_DATA_LOG', 'WARN'),
    (r'(?i)SELECT\s+.*\b(email|phone|ssn|dob|date_of_birth|national_id)\b.*\s+FROM',
     'PERSONAL_DATA_QUERY', 'INFO'),
]

def extract_added_lines(diff_path: str) -> list[dict]:
    added = []
    current_file = None
    line_num = 0
    with open(diff_path) as f:
        for line in f:
            if line.startswith('+++ '):
                current_file = line[4:].strip().lstrip('b/')
                line_num = 0
            elif line.startswith('@@'):
                m = re.search(r'\+(\d+)', line)
                line_num = int(m.group(1)) if m else 0
            elif line.startswith('+') and not line.startswith('+++'):
                added.append({
                    'file': current_file,
                    'line': line_num,
                    'content': line[1:].rstrip()
                })
                line_num += 1
            elif not line.startswith('-'):
                line_num += 1
    return added

def scan(diff_path: str) -> list[dict]:
    added_lines = extract_added_lines(diff_path)
    findings = []

    # regex scan
    for entry in added_lines:
        for pattern, pii_type, severity in CODE_PII_PATTERNS:
            for match in re.finditer(pattern, entry['content']):
                findings.append({
                    'file': entry['file'],
                    'line': entry['line'],
                    'type': pii_type,
                    'severity': severity,
                    'match': match.group(0)[:80],
                    'detector': 'regex'
                })

    # Presidio NLP scan on combined added text
    if added_lines:
        provider = NlpEngineProvider(nlp_configuration={
            'nlp_engine_name': 'spacy',
            'models': [{'lang_code': 'en', 'model_name': 'en_core_web_lg'}]
        })
        analyzer = AnalyzerEngine(nlp_engine=provider.create_engine())
        combined = '\n'.join(e['content'] for e in added_lines)
        results = analyzer.analyze(
            text=combined,
            entities=['EMAIL_ADDRESS', 'PHONE_NUMBER', 'PERSON', 'IBAN_CODE'],
            language='en',
            score_threshold=0.75
        )
        for r in results:
            findings.append({
                'type': r.entity_type,
                'severity': 'WARN',
                'score': round(r.score, 2),
                'detector': 'presidio'
            })

    return findings

if __name__ == '__main__':
    diff_path = sys.argv[1]
    findings = scan(diff_path)
    output = {
        'findings': findings,
        'count': len(findings),
        'has_blocks': any(f['severity'] == 'BLOCK' for f in findings)
    }
    out_path = os.environ.get('SCAN_OUTPUT', 'pii_findings.json')
    with open(out_path, 'w') as f:
        json.dump(output, f, indent=2)
    
    has_findings = len(findings) > 0
    if gha_output := os.environ.get('GITHUB_OUTPUT'):
        with open(gha_output, 'a') as f:
            f.write(f"has_findings={'true' if has_findings else 'false'}\n")
    
    print(f"PII scan: {len(findings)} findings ({output['has_blocks']} blocks)")
```

---

## Task 5 — Compliance assessment script

Create `.github/scripts/compliance_assess.py`:

```python
#!/usr/bin/env python3
"""
Calls the Kindlast API to assess compliance issues in a code diff.
Combines PII findings with AI-powered regulatory assessment.
"""
import sys, json, os, requests

def assess(diff_path: str, pii_path: str) -> dict:
    with open(diff_path) as f:
        diff_content = f.read()
    with open(pii_path) as f:
        pii = json.load(f)

    # extract only added lines
    added_lines = [
        line[1:] for line in diff_content.split('\n')
        if line.startswith('+') and not line.startswith('+++')
    ]

    if not added_lines and not pii['findings']:
        return {'severity': 'PASS', 'pii_findings': [], 'compliance_findings': []}

    query = f"""Review this code change for GDPR and EU AI Act compliance issues.

PII detector found: {json.dumps(pii['findings'][:10], indent=2)}

Added code lines:
```
{chr(10).join(added_lines[:150])}
```

Identify:
1. Personal data processed without apparent lawful basis (cite GDPR Article 6)
2. Data logged/stored requiring DPIA review (cite GDPR Article 35)  
3. Missing consent mechanisms for personal data collection
4. Data retention without deletion logic (cite GDPR Article 5(1)(e))
5. EU AI Act concerns if ML or AI code is present

Return max 5 findings. For each finding cite the specific article.
Return ONLY a JSON array of findings with fields: title, detail, severity (BLOCK|WARN|INFO), citation.
No prose outside the JSON array."""

    api_url = os.environ.get('KINDLAST_API_URL', '')
    api_key = os.environ.get('KINDLAST_API_KEY', '')

    findings = []
    if api_url and api_key:
        try:
            resp = requests.post(
                f'{api_url}/v1/compliance/assess',
                headers={
                    'Authorization': f'Bearer {api_key}',
                    'Content-Type': 'application/json'
                },
                json={'query': query, 'context': 'code_review', 'max_citations': 5},
                timeout=45
            )
            if resp.ok:
                data = resp.json()
                raw = data.get('answer', '[]')
                # strip any markdown fences
                raw = raw.strip().strip('```json').strip('```').strip()
                findings = json.loads(raw)
        except Exception as e:
            print(f'Kindlast API error: {e}', file=sys.stderr)

    # determine overall severity
    pii_has_block = pii['has_blocks']
    finding_block = any(f.get('severity') == 'BLOCK' for f in findings)
    finding_warn = any(f.get('severity') == 'WARN' for f in findings)

    if pii_has_block or finding_block:
        severity = 'BLOCK'
    elif pii['count'] > 0 or finding_warn:
        severity = 'WARN'
    elif findings:
        severity = 'INFO'
    else:
        severity = 'PASS'

    return {
        'severity': severity,
        'pii_findings': pii['findings'],
        'compliance_findings': findings,
    }

if __name__ == '__main__':
    result = assess(sys.argv[1], sys.argv[2])
    out_path = os.environ.get('ASSESSMENT_OUTPUT', 'assessment.json')
    with open(out_path, 'w') as f:
        json.dump(result, f, indent=2)
    
    if gha := os.environ.get('GITHUB_OUTPUT'):
        with open(gha, 'a') as f:
            f.write(f"severity={result['severity']}\n")
    
    print(f"Compliance assessment: {result['severity']}")
```

---

## Task 6 — Slack notifier

Create `.github/scripts/slack_notify.py`:

```python
#!/usr/bin/env python3
"""
Sends compliance scan results to Slack. 
Only sends for BLOCK and WARN — not INFO or PASS.
"""
import sys, json, os, requests

SEVERITY_EMOJI = {'BLOCK': ':red_circle:', 'WARN': ':large_yellow_circle:',
                  'INFO': ':large_blue_circle:', 'PASS': ':white_check_mark:'}

def build_message(assessment: dict, pr_url: str, pr_title: str, author: str) -> dict:
    severity = assessment['severity']
    findings = assessment.get('compliance_findings', [])
    pii = assessment.get('pii_findings', [])

    blocks = [
        {
            'type': 'header',
            'text': {
                'type': 'plain_text',
                'text': f"{SEVERITY_EMOJI[severity]} Compliance scan — {severity}"
            }
        },
        {
            'type': 'section',
            'fields': [
                {'type': 'mrkdwn', 'text': f'*PR:*\n<{pr_url}|{pr_title[:80]}>'},
                {'type': 'mrkdwn', 'text': f'*Author:*\n{author}'},
                {'type': 'mrkdwn', 'text': f'*PII findings:*\n{len(pii)}'},
                {'type': 'mrkdwn', 'text': f'*Compliance findings:*\n{len(findings)}'},
            ]
        },
        {'type': 'divider'},
    ]

    if pii:
        pii_text = '\n'.join(
            f"• `{f.get('file', '?')}:{f.get('line', '?')}` — {f['type']} ({f['severity']})"
            for f in pii[:5]
        )
        blocks.append({'type': 'section',
                       'text': {'type': 'mrkdwn', 'text': f'*PII detected:*\n{pii_text}'}})

    for f in findings[:3]:
        emoji = {'BLOCK': ':red_circle:', 'WARN': ':large_yellow_circle:'}.get(
            f.get('severity', ''), ':grey_question:')
        text = f"{emoji} *{f.get('title', 'Finding')}*\n{f.get('detail', '')}"
        if f.get('citation'):
            text += f"\n> _{f['citation']}_"
        blocks.append({'type': 'section', 'text': {'type': 'mrkdwn', 'text': text}})

    blocks.append({
        'type': 'actions',
        'elements': [
            {'type': 'button', 'text': {'type': 'plain_text', 'text': 'View PR'},
             'url': pr_url,
             'style': 'danger' if severity == 'BLOCK' else 'primary'},
        ]
    })

    return {'blocks': blocks}

if __name__ == '__main__':
    assessment = json.load(open(sys.argv[1]))
    severity = assessment.get('severity', 'PASS')

    # only alert on BLOCK and WARN
    if severity in ('PASS', 'INFO'):
        print(f'Skipping Slack — severity is {severity}')
        sys.exit(0)

    message = build_message(
        assessment,
        pr_url=os.environ.get('PR_URL', ''),
        pr_title=os.environ.get('PR_TITLE', 'Unknown PR'),
        author=os.environ.get('AUTHOR', 'unknown'),
    )

    webhook = os.environ.get('SLACK_WEBHOOK', '')
    if not webhook:
        print('No SLACK_WEBHOOK set — skipping')
        sys.exit(0)

    resp = requests.post(webhook, json=message, timeout=10)
    resp.raise_for_status()
    print(f'Slack notified: {severity}')
```

---

## Task 7 — Required GitHub secrets

Document all required secrets in `docs/github-secrets.md`:

```markdown
# Required GitHub Secrets

## All workflows
| Secret | Description |
|---|---|
| `KUBECONFIG` | base64-encoded kubeconfig for production cluster |
| `SLACK_DEPLOY_WEBHOOK` | Slack webhook for deployment notifications |

## Compliance scan
| Secret | Description |
|---|---|
| `KINDLAST_API_KEY` | API key for Kindlast compliance assessment (dogfooding) |
| `KINDLAST_API_URL` | Base URL of Kindlast API (https://api.kindlast.com) |
| `SLACK_COMPLIANCE_WEBHOOK` | Slack webhook for #compliance-alerts channel |

## CD workflow
| Secret | Description |
|---|---|
| `API_URL` | Production API URL for smoke test |
```

---

## Final acceptance criteria

### CI
- [ ] All Go tests pass with ≥70% coverage
- [ ] Python tests pass with ≥70% coverage
- [ ] TypeScript type-check passes with zero errors
- [ ] All Docker images build successfully
- [ ] `docker scout cves` shows no CRITICAL vulnerabilities in any image

### CD
- [ ] Merge to `main` triggers automatic deployment
- [ ] Deployment rolls back automatically if smoke test fails
- [ ] Slack deploy notification sent on success and failure
- [ ] New image tag visible in K8s: `kubectl get deployment -n kindlast-app -o wide`

### Compliance scan
- [ ] PR adding `email := "user@example.com"` triggers BLOCK finding
- [ ] BLOCK finding posts comment to PR and prevents merge
- [ ] BLOCK finding posts to `#compliance-alerts` Slack channel
- [ ] PASS finding posts PR comment with green status, no Slack notification
- [ ] Re-running scan on same PR deletes previous compliance comment and posts fresh one
- [ ] Scan only triggers when Go/Python/TypeScript/SQL files change
