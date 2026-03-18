import React from 'react'
import {
  Document,
  Page,
  View,
  Text,
  StyleSheet,
} from '@react-pdf/renderer'

const styles = StyleSheet.create({
  page: {
    padding: 40,
    fontFamily: 'Helvetica',
    fontSize: 11,
    lineHeight: 1.5,
  },
  coverPage: {
    padding: 40,
    fontFamily: 'Helvetica',
    display: 'flex',
    flexDirection: 'column',
    justifyContent: 'center',
    alignItems: 'center',
  },
  coverTitle: {
    fontSize: 28,
    fontFamily: 'Helvetica-Bold',
    marginBottom: 10,
  },
  coverSubtitle: {
    fontSize: 16,
    color: '#666',
    marginBottom: 30,
  },
  coverCompany: {
    fontSize: 20,
    fontFamily: 'Helvetica-Bold',
    marginBottom: 8,
  },
  coverDate: {
    fontSize: 12,
    color: '#999',
  },
  sectionTitle: {
    fontSize: 18,
    fontFamily: 'Helvetica-Bold',
    marginBottom: 12,
    color: '#1a1a1a',
  },
  summaryScore: {
    fontSize: 48,
    fontFamily: 'Helvetica-Bold',
    textAlign: 'center',
    marginBottom: 4,
  },
  summaryRisk: {
    fontSize: 14,
    textAlign: 'center',
    marginBottom: 16,
    textTransform: 'uppercase',
  },
  summaryText: {
    fontSize: 11,
    lineHeight: 1.6,
    marginBottom: 20,
  },
  findingCard: {
    marginBottom: 16,
    padding: 12,
    borderWidth: 1,
    borderColor: '#e0e0e0',
    borderRadius: 4,
  },
  findingHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginBottom: 6,
  },
  findingTitle: {
    fontSize: 12,
    fontFamily: 'Helvetica-Bold',
  },
  findingSeverity: {
    fontSize: 10,
    fontFamily: 'Helvetica-Bold',
    textTransform: 'uppercase',
    padding: '2 6',
    borderRadius: 2,
  },
  findingDescription: {
    fontSize: 10,
    color: '#444',
    marginBottom: 6,
  },
  findingRecommendation: {
    fontSize: 10,
    color: '#333',
    fontFamily: 'Helvetica-Oblique',
  },
  findingArticle: {
    fontSize: 9,
    color: '#666',
    marginTop: 4,
  },
  disclaimer: {
    fontSize: 9,
    color: '#888',
    fontFamily: 'Helvetica-Oblique',
    lineHeight: 1.5,
    borderTopWidth: 1,
    borderTopColor: '#ddd',
    paddingTop: 12,
    marginTop: 20,
  },
})

function getSeverityColor(severity: string): string {
  switch (severity) {
    case 'critical':
      return '#dc2626'
    case 'high':
      return '#ea580c'
    case 'medium':
      return '#ca8a04'
    case 'low':
      return '#16a34a'
    case 'pass':
      return '#059669'
    default:
      return '#666'
  }
}

function getScoreColor(score: number): string {
  if (score >= 90) return '#059669'
  if (score >= 70) return '#16a34a'
  if (score >= 50) return '#ca8a04'
  if (score >= 30) return '#ea580c'
  return '#dc2626'
}

interface FindingData {
  id: string
  category: string
  severity: 'critical' | 'high' | 'medium' | 'low' | 'pass'
  title: string
  description: string
  recommendation: string
  gdpr_article: string | null
}

interface ComplianceReportProps {
  companyName: string
  date: string
  overallScore: number
  riskLevel: 'low' | 'medium' | 'high' | 'critical'
  summary: string
  findings: FindingData[]
}

export function ComplianceReport({
  companyName,
  date,
  overallScore,
  riskLevel,
  summary,
  findings,
}: ComplianceReportProps) {
  return (
    <Document>
      {/* Cover Page */}
      <Page size="A4" style={styles.coverPage}>
        <Text style={styles.coverTitle}>GDPR Compliance Report</Text>
        <Text style={styles.coverSubtitle}>
          Powered by Kindlast
        </Text>
        <Text style={styles.coverCompany}>{companyName}</Text>
        <Text style={styles.coverDate}>Generated on {date}</Text>
      </Page>

      {/* Summary Page */}
      <Page size="A4" style={styles.page}>
        <Text style={styles.sectionTitle}>Compliance Summary</Text>
        <Text
          style={{
            ...styles.summaryScore,
            color: getScoreColor(overallScore),
          }}
        >
          {overallScore}/100
        </Text>
        <Text
          style={{
            ...styles.summaryRisk,
            color: getScoreColor(overallScore),
          }}
        >
          {riskLevel} Risk
        </Text>
        <Text style={styles.summaryText}>{summary}</Text>

        <View style={{ marginTop: 10 }}>
          <Text style={{ fontSize: 12, fontFamily: 'Helvetica-Bold', marginBottom: 8 }}>
            Findings Overview
          </Text>
          <Text style={{ fontSize: 10, color: '#666' }}>
            Critical: {findings.filter((f) => f.severity === 'critical').length}
            {'  |  '}
            High: {findings.filter((f) => f.severity === 'high').length}
            {'  |  '}
            Medium: {findings.filter((f) => f.severity === 'medium').length}
            {'  |  '}
            Low: {findings.filter((f) => f.severity === 'low').length}
            {'  |  '}
            Pass: {findings.filter((f) => f.severity === 'pass').length}
          </Text>
        </View>
      </Page>

      {/* Findings Pages */}
      {findings.length > 0 && (
        <Page size="A4" style={styles.page}>
          <Text style={styles.sectionTitle}>Detailed Findings</Text>
          {findings.map((finding) => (
            <View key={finding.id} style={styles.findingCard} wrap={false}>
              <View style={styles.findingHeader}>
                <Text style={styles.findingTitle}>{finding.title}</Text>
                <Text
                  style={{
                    ...styles.findingSeverity,
                    color: getSeverityColor(finding.severity),
                  }}
                >
                  {finding.severity}
                </Text>
              </View>
              <Text style={styles.findingDescription}>
                {finding.description}
              </Text>
              <Text style={styles.findingRecommendation}>
                Recommendation: {finding.recommendation}
              </Text>
              {finding.gdpr_article && (
                <Text style={styles.findingArticle}>
                  GDPR Reference: {finding.gdpr_article}
                </Text>
              )}
            </View>
          ))}
        </Page>
      )}

      {/* Disclaimer Page */}
      <Page size="A4" style={styles.page}>
        <Text style={styles.sectionTitle}>Legal Disclaimer</Text>
        <Text style={styles.disclaimer}>
          Kindlast provides AI-generated compliance guidance for educational and
          planning purposes. It is not a substitute for professional legal
          advice. For binding compliance determinations, consult a qualified data
          protection attorney or certified DPO.
        </Text>
        <Text style={{ ...styles.disclaimer, marginTop: 12, borderTopWidth: 0 }}>
          This report was generated automatically using artificial intelligence.
          While we strive for accuracy, the analysis may not capture all aspects
          of your compliance posture. Regulatory requirements may change over
          time. Always verify findings with current legislation and seek
          professional guidance for compliance decisions.
        </Text>
        <Text
          style={{
            fontSize: 9,
            color: '#aaa',
            marginTop: 40,
            textAlign: 'center',
          }}
        >
          Generated by Kindlast - kindlast.com
        </Text>
      </Page>
    </Document>
  )
}
