# GitHub Secrets Configuration

This document describes all GitHub repository secrets required for Kindlast CI/CD workflows.

## Quick Reference

| Secret Name | Required | Workflows |
|-------------|----------|-----------|
| `GITHUB_TOKEN` | Auto | cd.yml |
| `KUBECONFIG` | Yes | cd.yml |
| `API_URL` | Yes | cd.yml |
| `SLACK_DEPLOY_WEBHOOK` | Yes | cd.yml |
| `KINDLAST_API_KEY` | Yes | compliance-scan.yml |
| `KINDLAST_API_URL` | Yes | compliance-scan.yml |
| `SLACK_COMPLIANCE_WEBHOOK` | Yes | compliance-scan.yml |

---

## Secrets by Category

### Infrastructure

#### `KUBECONFIG`

- **Description**: Base64-encoded kubeconfig file for accessing the production Kubernetes cluster
- **Used in**: `cd.yml` (Deploy to K8s job)
- **Required**: Yes
- **How to obtain**:
  1. Export your kubeconfig from your Kubernetes provider (GKE, EKS, AKS, etc.)
  2. Ensure it contains credentials for the production cluster
  3. Base64 encode the file:
     ```bash
     cat ~/.kube/config | base64 -w 0
     ```
  4. Copy the output as the secret value
- **Security notes**:
  - Use a service account with minimal required permissions
  - Consider using short-lived tokens where supported
  - Rotate credentials regularly

#### `API_URL`

- **Description**: Production API URL used for smoke testing after deployment
- **Used in**: `cd.yml` (Smoke test step)
- **Required**: Yes
- **How to obtain**:
  - Use your production API endpoint (e.g., `https://api.kindlast.com`)
  - Must be accessible from GitHub Actions runners
- **Example value**: `https://api.kindlast.com`

### Docker Registry

#### `GITHUB_TOKEN`

- **Description**: Automatically provided by GitHub Actions for authenticating with GitHub Container Registry (GHCR)
- **Used in**: `cd.yml` (Login to GHCR step)
- **Required**: Auto-provided
- **How to obtain**: This secret is automatically available in all GitHub Actions workflows. No manual configuration required.
- **Security notes**:
  - Automatically scoped to the repository
  - Permissions controlled via `permissions` block in workflow

### Notifications (Slack)

#### `SLACK_DEPLOY_WEBHOOK`

- **Description**: Slack incoming webhook URL for deployment notifications
- **Used in**: `cd.yml` (Notify Slack on success/failure steps)
- **Required**: Yes
- **How to obtain**:
  1. Go to [Slack API: Incoming Webhooks](https://api.slack.com/messaging/webhooks)
  2. Create a new Slack app or use an existing one
  3. Enable Incoming Webhooks
  4. Add a new webhook to your workspace
  5. Choose the channel for deployment notifications (e.g., `#deployments`)
  6. Copy the webhook URL
- **Example value**: `https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX`

#### `SLACK_COMPLIANCE_WEBHOOK`

- **Description**: Slack incoming webhook URL for compliance scan alerts
- **Used in**: `compliance-scan.yml` (Notify Slack step)
- **Required**: Yes
- **How to obtain**:
  1. Follow the same process as `SLACK_DEPLOY_WEBHOOK`
  2. Choose the channel for compliance alerts (e.g., `#compliance-alerts`)
  3. Copy the webhook URL
- **Security notes**:
  - Consider using a dedicated channel for compliance alerts
  - Only BLOCK and WARN findings trigger notifications (not INFO or PASS)

### Kindlast API (Dogfooding)

#### `KINDLAST_API_KEY`

- **Description**: API key for accessing the Kindlast compliance assessment API (used for dogfooding the product in CI)
- **Used in**: `compliance-scan.yml` (Run compliance assessment step)
- **Required**: Yes
- **How to obtain**:
  1. Log into the Kindlast dashboard
  2. Navigate to Settings > API Keys
  3. Generate a new API key with compliance assessment permissions
  4. Copy the key value
- **Security notes**:
  - Use a dedicated API key for CI/CD
  - Monitor usage to detect anomalies
  - Rotate periodically

#### `KINDLAST_API_URL`

- **Description**: Base URL of the Kindlast API for compliance assessments
- **Used in**: `compliance-scan.yml` (Run compliance assessment step)
- **Required**: Yes
- **How to obtain**:
  - Use the production Kindlast API URL
- **Example value**: `https://api.kindlast.com`

---

## Setup Instructions

### Adding Secrets in GitHub

1. Navigate to your repository on GitHub
2. Go to **Settings** > **Secrets and variables** > **Actions**
3. Click **New repository secret**
4. Enter the secret name (e.g., `KUBECONFIG`)
5. Paste the secret value
6. Click **Add secret**

### Environment-Specific Secrets

The `cd.yml` workflow uses the `production` environment. To configure environment-specific secrets:

1. Go to **Settings** > **Environments**
2. Click **New environment** and name it `production`
3. Add environment-specific secrets under the environment
4. Configure protection rules (required reviewers, deployment branches, etc.)

### Verifying Secrets

After adding secrets, verify they are configured correctly:

1. Trigger a workflow run manually or via a test PR
2. Check the workflow logs for authentication errors
3. Secrets are masked in logs (shown as `***`)

---

## Security Best Practices

### General Guidelines

1. **Principle of Least Privilege**: Use credentials with only the permissions necessary for each workflow
2. **Rotate Regularly**: Update secrets periodically, especially API keys and service account credentials
3. **Audit Access**: Review who has access to repository settings and secrets
4. **Use Environments**: Leverage GitHub Environments for production deployments to add approval gates

### Secret-Specific Recommendations

| Secret | Rotation Frequency | Additional Security |
|--------|-------------------|---------------------|
| `KUBECONFIG` | 90 days | Use service account, limit namespace access |
| `KINDLAST_API_KEY` | 90 days | Monitor usage, use dedicated CI key |
| `SLACK_*_WEBHOOK` | As needed | Regenerate if compromised |

### What NOT to Store as Secrets

- Secrets that can be derived from environment variables
- Values that change frequently (use workflow inputs instead)
- Large files (use encrypted artifacts or external secret managers)

---

## Troubleshooting

### Common Issues

#### "Secret not found" Error

- Verify the secret name matches exactly (case-sensitive)
- Check if the secret is defined at repository vs. environment level
- Ensure the workflow has permission to access the secret

#### Kubernetes Authentication Failures

- Verify the kubeconfig is correctly base64-encoded
- Check that the cluster endpoint is accessible from GitHub Actions
- Ensure the service account has required RBAC permissions

#### Slack Notifications Not Sending

- Verify the webhook URL is correct and active
- Check if the Slack app is still installed in the workspace
- Test the webhook manually:
  ```bash
  curl -X POST -H 'Content-Type: application/json' \
    -d '{"text":"Test message"}' \
    https://hooks.slack.com/services/YOUR/WEBHOOK/URL
  ```

#### Kindlast API Errors

- Verify the API key is valid and not expired
- Check the API URL is correct and accessible
- Review API rate limits and usage quotas

---

## References

- [GitHub Docs: Encrypted Secrets](https://docs.github.com/en/actions/security-guides/encrypted-secrets)
- [GitHub Docs: Using Environments](https://docs.github.com/en/actions/deployment/targeting-different-environments/using-environments-for-deployment)
- [Slack API: Incoming Webhooks](https://api.slack.com/messaging/webhooks)
- [Kubernetes: Configure Access to Multiple Clusters](https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/)
