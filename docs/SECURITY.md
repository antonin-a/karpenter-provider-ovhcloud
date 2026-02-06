# Security Guide

This document explains how to create secure, restricted API credentials for Karpenter on OVHcloud.

## Principle of Least Privilege

Karpenter manages node pools in your MKS cluster. To minimize security risks, **always create API credentials that are restricted to your specific cluster**, not project-wide or account-wide credentials.

## Required API Permissions

Karpenter needs the following API permissions to function:

| Method | API Path | Purpose |
|--------|----------|---------|
| GET | `/cloud/project/{serviceName}/kube/{kubeId}` | Get cluster info (region auto-detection) |
| GET | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool` | List node pools |
| GET | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*` | Get pool details |
| POST | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool` | Create new node pools |
| PUT | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*` | Update pools (scale up/down) |
| DELETE | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*` | Delete node pools |
| GET | `/cloud/project/{serviceName}/kube/{kubeId}/flavors` | List available instance types |
| GET | `/cloud/project/{serviceName}/capabilities/kube/*` | Get MKS capabilities (optional) |

## Creating Restricted Credentials

### Step 1: Create API Keys via OVHcloud Console

#### Quick Method (Pre-filled URL)

Use this URL with pre-filled permissions (replace `{serviceName}` (your OVHcloud/Openstack ProjectID) and `{kubeId}` (your MKS cluster ID) with your values):

```
https://api.ovh.com/createToken/?GET=/cloud/project/{serviceName}/kube/{kubeId}&GET=/cloud/project/{serviceName}/kube/{kubeId}/nodepool&GET=/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*&POST=/cloud/project/{serviceName}/kube/{kubeId}/nodepool&PUT=/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*&DELETE=/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*&GET=/cloud/project/{serviceName}/kube/{kubeId}/flavors&GET=/cloud/project/{serviceName}/capabilities/kube/*
```

Or use the helper script to generate this URL for you:
```bash
./hack/create-ovh-credentials.sh
```

The URL pre-fills the permissions. You still need to fill in:
- **Application name**: `karpenter-mks-{kubeId}` (use your actual cluster ID)
- **Application description**: `Karpenter autoscaler for MKS cluster {kubeId}`
- **Validity**: Change from `1 day` to `Unlimited` for production use

#### Manual Method

Go to the OVHcloud token creation page:

| Region | URL |
|--------|-----|
| EU | https://api.ovh.com/createToken/ |
| CA | https://ca.api.ovh.com/createToken/ |
| US | https://us.api.ovh.com/createToken/ |

Fill in the form:

| Field | Value |
|-------|-------|
| **Application name** | `karpenter-mks-{kubeId}` |
| **Application description** | `Karpenter autoscaler for MKS cluster {kubeId}` |
| **Validity** | Unlimited (recommended for production) |

Then add the following rights (replace `{serviceName}` and `{kubeId}` with your values):

| Method | Path |
|--------|------|
| GET | `/cloud/project/{serviceName}/kube/{kubeId}` |
| GET | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool` |
| GET | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*` |
| POST | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool` |
| PUT | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*` |
| DELETE | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*` |
| GET | `/cloud/project/{serviceName}/kube/{kubeId}/flavors` |
| GET | `/cloud/project/{serviceName}/capabilities/kube/*` |

Click **Create** and save the three credentials displayed:
- **Application Key** (AK)
- **Application Secret** (AS)
- **Consumer Key** (CK)

> ⚠️ **Important**: The Application Secret is only shown once. Save it immediately.

### Step 2: Create the Kubernetes Secret

```bash
kubectl create namespace karpenter

kubectl create secret generic ovh-credentials -n karpenter \
  --from-literal=applicationKey=YOUR_APPLICATION_KEY \
  --from-literal=applicationSecret=YOUR_APPLICATION_SECRET \
  --from-literal=consumerKey=YOUR_CONSUMER_KEY
```

## Permission Scope Comparison

| Scope | Example Path | Risk Level | Recommendation |
|-------|--------------|------------|----------------|
| All projects | `/cloud/*` | 🔴 **Critical** | Never use |
| Entire project | `/cloud/project/{serviceName}/*` | 🟠 **High** | Avoid - includes VMs, storage, network |
| All MKS clusters | `/cloud/project/{serviceName}/kube/*` | 🟡 **Medium** | Not recommended |
| Single cluster | `/cloud/project/{serviceName}/kube/{kubeId}/*` | 🟢 **Minimal** | **Recommended** |

## Finding Your Service Name and Kube ID

### Service Name (Project ID)

1. Go to [OVHcloud Control Panel](https://www.ovh.com/manager/#/public-cloud/)
2. Select your Public Cloud project
3. The **Project ID** is in the URL or in Project Settings

Or via API:
```bash
curl https://eu.api.ovh.com/1.0/cloud/project
```

### Kube ID (Cluster ID)

1. Go to your MKS cluster in the Control Panel
2. The **Cluster ID** is shown in the cluster details

Or via API:
```bash
curl https://eu.api.ovh.com/1.0/cloud/project/{serviceName}/kube
```

Or from your cluster (if Karpenter is already running):
```bash
kubectl get nodes -o jsonpath='{.items[0].metadata.annotations.cluster\.x-k8s\.io/cluster-name}'
```

## Automated Script

Use the helper script to create credentials automatically:

```bash
./hack/create-ovh-credentials.sh
```

See [hack/create-ovh-credentials.sh](../hack/create-ovh-credentials.sh) for details.

## Security Recommendations

### 1. One Credential Set Per Cluster

Don't share credentials between clusters. Each MKS cluster should have its own API credentials.

### 2. Regular Rotation

Rotate your credentials periodically (every 90 days recommended):

```bash
# Create new consumer key
# Update Kubernetes secret
kubectl create secret generic ovh-credentials -n karpenter \
  --from-literal=applicationKey=$APP_KEY \
  --from-literal=applicationSecret=$APP_SECRET \
  --from-literal=consumerKey=$NEW_CONSUMER_KEY \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart Karpenter to pick up new credentials
kubectl rollout restart deployment -n karpenter karpenter
```

### 3. Secret Management in Production

For production environments, consider using:

- **[External Secrets Operator](https://help.ovhcloud.com/csm/fr-secret-manager-external-secret-operator?id=kb_article_view&sysparm_article=KB0074303)** with OVHcloud Secret Manager backend
- **HashiCorp Vault** with the Vault Agent Injector
- **Sealed Secrets** for GitOps workflows

### 4. Audit Logging

Enable API audit logging in your OVHcloud account to track all API calls made by Karpenter.

### 5. Never Commit Credentials

- Add credential files to `.gitignore`
- Use environment variables or secret managers
- Enable secret scanning in your repository

## Troubleshooting

### "Client::Forbidden" Error

Your credentials don't have the required permissions. Verify:
1. The Consumer Key is validated
2. The access rules include all required paths
3. The `{serviceName}` and `{kubeId}` in the rules match your cluster

### "Client::Unauthorized" Error

The credentials are invalid or expired. Create a new Consumer Key.

### Testing Credentials

Test your credentials using the [OVHcloud CLI](https://github.com/ovh/ovhcloud-cli):

```bash
# Install OVHcloud CLI (see https://github.com/ovh/ovhcloud-cli for other methods)
brew install ovh/tap/ovhcloud-cli   # macOS
# or download from GitHub releases

# Login with your credentials
ovhcloud login

# Test with a simple authorized API call
ovhcloud cloud kube get $KUBE_ID --cloud-project $SERVICE_NAME
```

## References

- [OVH API Documentation](https://api.ovh.com/)
- [OVH API Console](https://eu.api.ovh.com/console/)
- [Creating OVH API Credentials](https://docs.ovh.com/gb/en/api/first-steps-with-ovh-api/)
