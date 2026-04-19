#!/usr/bin/env python3
"""
Sends compliance scan results to Slack.
Only sends for BLOCK and WARN - not INFO or PASS.
"""
import sys
import json
import os
import requests

SEVERITY_EMOJI = {
    'BLOCK': ':red_circle:',
    'WARN': ':large_yellow_circle:',
    'INFO': ':large_blue_circle:',
    'PASS': ':white_check_mark:'
}


def build_message(assessment: dict, pr_url: str, pr_title: str, author: str) -> dict:
    """Build Slack message blocks from assessment results."""
    severity = assessment['severity']
    findings = assessment.get('compliance_findings', [])
    pii = assessment.get('pii_findings', [])

    blocks = [
        {
            'type': 'header',
            'text': {
                'type': 'plain_text',
                'text': f"{SEVERITY_EMOJI[severity]} Compliance scan - {severity}"
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
            f"- `{f.get('file', '?')}:{f.get('line', '?')}` - {f['type']} ({f['severity']})"
            for f in pii[:5]
        )
        blocks.append({
            'type': 'section',
            'text': {'type': 'mrkdwn', 'text': f'*PII detected:*\n{pii_text}'}
        })

    for f in findings[:3]:
        emoji = {
            'BLOCK': ':red_circle:',
            'WARN': ':large_yellow_circle:'
        }.get(f.get('severity', ''), ':grey_question:')
        text = f"{emoji} *{f.get('title', 'Finding')}*\n{f.get('detail', '')}"
        if f.get('citation'):
            text += f"\n> _{f['citation']}_"
        blocks.append({'type': 'section', 'text': {'type': 'mrkdwn', 'text': text}})

    blocks.append({
        'type': 'actions',
        'elements': [
            {
                'type': 'button',
                'text': {'type': 'plain_text', 'text': 'View PR'},
                'url': pr_url,
                'style': 'danger' if severity == 'BLOCK' else 'primary'
            },
        ]
    })

    return {'blocks': blocks}


if __name__ == '__main__':
    assessment = json.load(open(sys.argv[1]))
    severity = assessment.get('severity', 'PASS')

    # only alert on BLOCK and WARN
    if severity in ('PASS', 'INFO'):
        print(f'Skipping Slack - severity is {severity}')
        sys.exit(0)

    message = build_message(
        assessment,
        pr_url=os.environ.get('PR_URL', ''),
        pr_title=os.environ.get('PR_TITLE', 'Unknown PR'),
        author=os.environ.get('AUTHOR', 'unknown'),
    )

    webhook = os.environ.get('SLACK_WEBHOOK', '')
    if not webhook:
        print('No SLACK_WEBHOOK set - skipping')
        sys.exit(0)

    resp = requests.post(webhook, json=message, timeout=10)
    resp.raise_for_status()
    print(f'Slack notified: {severity}')
