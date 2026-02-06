<p align="center">
  <img src="docs/images/logo.png" alt="Karpenter Provider OVHcloud" width="400">
</p>

<p align="center">
  <a href="https://github.com/ovh/karpenter-provider-ovhcloud/actions"><img src="https://img.shields.io/github/actions/workflow/status/ovh/karpenter-provider-ovhcloud/ci.yaml?branch=main" alt="Build Status"></a>
  <a href="https://github.com/ovh/karpenter-provider-ovhcloud/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License"></a>
  <a href="https://goreportcard.com/report/github.com/ovh/karpenter-provider-ovhcloud"><img src="https://goreportcard.com/badge/github.com/ovh/karpenter-provider-ovhcloud" alt="Go Report Card"></a>
  <a href="https://github.com/ovh/karpenter-provider-ovhcloud/stargazers"><img src="https://img.shields.io/github/stars/ovh/karpenter-provider-ovhcloud" alt="GitHub Stars"></a>
  <a href="https://github.com/ovh/karpenter-provider-ovhcloud/issues"><img src="https://img.shields.io/badge/contributions-welcome-brightgreen.svg" alt="Contributions Welcome"></a>
</p>

<p align="center">
  <b>Autoscale OVHcloud MKS cluster nodes efficiently and cost-effectively.</b>
</p>

---

## Overview

Karpenter is an open-source node provisioning project built for Kubernetes. This provider enables Karpenter to work with **OVHcloud Managed Kubernetes Service (MKS)**.

Karpenter improves the efficiency and cost of running workloads on Kubernetes clusters by:

- **Watching** for pods that the Kubernetes scheduler has marked as unschedulable
- **Evaluating** scheduling constraints (resource requests, nodeselectors, affinities, tolerations, and topology spread constraints)
- **Provisioning** nodes via OVHcloud MKS node pools that meet the requirements
- **Removing** nodes when they are no longer needed

## Scope & Deployment Mode

> **This is a Karpenter provider for OVHcloud Managed Kubernetes Service (MKS).**

### Deployment Mode: Self-hosted

This provider is available in **self-hosted mode**. You deploy and manage Karpenter yourself on your MKS cluster using Helm.

| Mode | Status | Description |
|------|--------|-------------|
| Self-hosted | ✅ Available | Deploy Karpenter yourself via Helm |

### Cluster Compatibility

This provider uses the MKS Node Pool APIs and is designed exclusively for OVHcloud Managed Kubernetes Service.

| Cluster Type | Supported |
|--------------|-----------|
| OVHcloud MKS (Managed Kubernetes) | ✅ Yes |
| Self-managed Kubernetes on OVHcloud | ❌ No |

## Features

| Feature | Status |
|---------|--------|
| Automatic node provisioning via MKS Node Pools | ✅ |
| Cost-aware instance selection | ✅ |
| All OVHcloud instance types (B, C, R, T series) | ✅ |
| GPU instance support (T series) | ✅ |
| Monthly billing option (gen2 instances only) | ✅ |
| Anti-affinity placement | ✅ |
| Auto-detection of region and cluster ID | ✅ |
| Spot instances | ❌ Not available on OVHcloud |

## Prerequisites

Before installing Karpenter, ensure you have:

- An **OVHcloud account** with a Public Cloud project
- An **MKS cluster** (Managed Kubernetes Service)
- **OVH API credentials** with appropriate permissions (see [Security](#api-permissions-security))
- **kubectl** configured to access your cluster
- **Helm** v3.x installed

## API Permissions (Security)

Karpenter requires OVH API credentials to manage node pools. For security, **always use credentials scoped to your specific cluster**.

### Minimum Required Permissions

| Method | API Path | Purpose |
|--------|----------|---------|
| GET | `/cloud/project/{serviceName}/kube/{kubeId}` | Cluster info |
| GET | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool` | List pools |
| GET | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*` | Pool details |
| POST | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool` | Create pools |
| PUT | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*` | Scale pools |
| DELETE | `/cloud/project/{serviceName}/kube/{kubeId}/nodepool/*` | Delete pools |
| GET | `/cloud/project/{serviceName}/kube/{kubeId}/flavors` | Instance types |
| GET | `/cloud/project/{serviceName}/capabilities/kube/*` | Capabilities |

> ⚠️ **Security Best Practice**: Never use project-wide or account-wide API credentials.
> See [docs/SECURITY.md](docs/SECURITY.md) for detailed instructions on creating restricted credentials.

## Installation

### 1. Create OVH API Credentials

Follow the [Security Guide](docs/SECURITY.md) to create restricted API credentials.

### 2. Create Kubernetes Secret

```bash
kubectl create namespace karpenter

kubectl create secret generic ovh-credentials -n karpenter \
  --from-literal=applicationKey=YOUR_APP_KEY \
  --from-literal=applicationSecret=YOUR_APP_SECRET \
  --from-literal=consumerKey=YOUR_CONSUMER_KEY
```

### 3. Install via Helm

```bash
helm install karpenter ./charts \
  --namespace karpenter \
  --set ovh.serviceName=YOUR_PROJECT_ID
```

> **Note**: `kubeId` and `region` are auto-detected from the cluster. You only need to provide `serviceName` (your OVHcloud project ID).

## Quick Start

### 1. Create an OVHNodeClass

```yaml
apiVersion: karpenter.ovhcloud.sh/v1alpha1
kind: OVHNodeClass
metadata:
  name: default
spec:
  serviceName: "your-project-id"       # OVHcloud Public Cloud project ID
  kubeId: "your-cluster-id"            # Optional: auto-detected
  region: "EU-WEST-PAR"                # Optional: auto-detected
  credentialsSecretRef:
    name: ovh-credentials
    namespace: karpenter
  monthlyBilled: false                 # Hourly billing (gen2 instances only: b2, c2, d2, r2)
  antiAffinity: false                  # Spread across hypervisors
```

### 2. Create a NodePool

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: default
spec:
  template:
    spec:
      nodeClassRef:
        group: karpenter.ovhcloud.sh
        kind: OVHNodeClass
        name: default
      requirements:
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["b3-8", "b3-16", "b3-32"]  # OVHcloud flavor names
  disruption:
    consolidationPolicy: WhenEmpty
    consolidateAfter: 30s
```

### 3. Deploy a Workload

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inflate
spec:
  replicas: 5
  selector:
    matchLabels:
      app: inflate
  template:
    metadata:
      labels:
        app: inflate
    spec:
      containers:
        - name: inflate
          image: public.ecr.aws/eks-distro/kubernetes/pause:3.7
          resources:
            requests:
              cpu: "1"
              memory: "1Gi"
```

### 4. Watch Nodes Scale

```bash
kubectl get nodes -w
```

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `OVH_ENDPOINT` | No | `ovh-eu` | OVH API endpoint (ovh-eu, ovh-ca, ovh-us) |
| `OVH_SERVICE_NAME` | **Yes** | - | OVHcloud Public Cloud project ID |
| `OVH_KUBE_ID` | No | Auto-detected | MKS cluster ID |
| `OVH_REGION` | No | Auto-detected | OVHcloud region (EU-WEST-PAR, GRA7, etc.) |

### OVHcloud Instance Types

Karpenter supports all OVHcloud instance types available in MKS:

| Series | Type | Use Case |
|--------|------|----------|
| B2/B3 | General Purpose | Balanced workloads |
| C2/C3 | Compute Optimized | CPU-intensive workloads |
| R2/R3 | Memory Optimized | Memory-intensive workloads |
| T1/T2 | GPU | Machine learning, AI |

## Known Limitations

| Limitation | Description |
|------------|-------------|
| MKS clusters only | Does not support self-managed Kubernetes on OVHcloud |
| No spot instances | OVHcloud doesn't offer spot/preemptible instances |
| Max 100 node pools | MKS limit per cluster |
| Pool-based scaling | Individual node deletion not supported; Karpenter scales pools |
| Async pool creation | Node creation has ~2 min latency |
| Monthly billing | Only available on gen2 instances (b2, c2, d2, r2) |
| Savings Plans | Must be managed manually via OVHcloud Console; Karpenter is not Savings Plan-aware |

## Troubleshooting

### Common Issues

**Nodes not provisioning**
```bash
# Check Karpenter logs
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter

# Check pending pods
kubectl get pods --field-selector=status.phase=Pending
```

**API permission errors**
```bash
# Verify credentials
kubectl get secret ovh-credentials -n karpenter -o yaml
```

**Region mismatch**
- Ensure your NodeClass region matches your MKS cluster region
- Or leave it empty for auto-detection

See [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) for more solutions.

## Community

- **Slack**: [#karpenter](https://kubernetes.slack.com/archives/C02SFFZSA2K) on [Kubernetes Slack](https://slack.k8s.io/)
- **GitHub Issues**: [Report bugs or request features](https://github.com/ovh/karpenter-provider-ovhcloud/issues)
- **Contributing**: See [CONTRIBUTING.md](CONTRIBUTING.md)

## Related Projects

- [Karpenter Core](https://github.com/kubernetes-sigs/karpenter) - The Karpenter project
- [OVHcloud MKS Documentation](https://docs.ovh.com/gb/en/kubernetes/)

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

---

<p align="center">
  Made with ❤️ for the Kubernetes community
</p>
