#!/usr/bin/env python3
"""
Calls the Kindlast API to assess compliance issues in a code diff.
Combines PII findings with AI-powered regulatory assessment.
"""
import sys
import json
import os
import requests


def assess(diff_path: str, pii_path: str) -> dict:
    """Assess compliance issues in a code diff."""
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
