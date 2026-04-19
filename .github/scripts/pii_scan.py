#!/usr/bin/env python3
"""
Scans git diff for PII in added lines using regex patterns + Presidio NLP.
Outputs findings to SCAN_OUTPUT env var path.
"""
import sys
import json
import re
import os
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
    """Extract added lines from a git diff file."""
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
    """Scan diff for PII using regex and Presidio NLP."""
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
