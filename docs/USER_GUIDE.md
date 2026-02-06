# Karpenter OVHcloud User Guide

## Introduction

Karpenter OVHcloud is an implementation of the Karpenter project for OVHcloud Managed Kubernetes Service (MKS). It enables automatic and intelligent node autoscaling for your Kubernetes cluster based on the actual needs of pending pods.

### Comparison with OVHcloud Cluster Autoscaler

| Criteria | Cluster Autoscaler | Karpenter OVHcloud |
|----------|-------------------|-------------------|
| **Granularity** | Entire Node Pool | Per pod/workload |
| **Flavor selection** | Fixed per pool | Dynamic based on needs |
| **Speed** | 2-5 minutes | 1-3 minutes |
| **Consolidation** | No | Yes (automatic packing) |
| **Configuration** | Per Node Pool | Centralized (NodePool CRD) |
| **Multi-zone** | Manual | Automatic |

---

## Prerequisites

- Active OVHcloud MKS cluster
- OVHcloud API credentials (Application Key, Secret, Consumer Key)
- `kubectl` and `helm` installed
- Administrator access to the cluster

### Creating OVHcloud credentials

1. Go to [https://api.ovh.com/createToken/](https://api.ovh.com/createToken/)
2. Configure the following permissions:
   ```
   GET    /cloud/project/*/kube/*
   POST   /cloud/project/*/kube/*/nodepool
   PUT    /cloud/project/*/kube/*/nodepool/*
   DELETE /cloud/project/*/kube/*/nodepool/*
   GET    /cloud/project/*/kube/*/nodepool
   GET    /cloud/project/*/kube/*/nodepool/*/nodes
   GET    /cloud/project/*/kube/*/flavors
   ```

See [SECURITY.md](SECURITY.md) for detailed instructions on creating restricted credentials.

---

## Installation

### 1. Install CRDs

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/main/pkg/apis/crds/karpenter.sh_nodepools.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/main/pkg/apis/crds/karpenter.sh_nodeclaims.yaml
kubectl apply -f charts/crds/karpenter.ovhcloud.sh_ovhnodeclasses.yaml
```

### 2. Create the credentials Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: ovh-credentials
  namespace: karpenter
type: Opaque
stringData:
  endpoint: "ovh-eu"
  applicationKey: "YOUR_APPLICATION_KEY"
  applicationSecret: "YOUR_APPLICATION_SECRET"
  consumerKey: "YOUR_CONSUMER_KEY"
```

### 3. Install via Helm

```bash
helm install karpenter ./charts \
  --namespace karpenter \
  --create-namespace \
  --set ovh.serviceName="YOUR_PROJECT_ID"
```

> **Note**: `kubeId` and `region` are auto-detected from the cluster. You only need to provide `serviceName` (your OVHcloud project ID).

---

## Configuration

### OVHNodeClass

The `OVHNodeClass` defines OVHcloud-specific configuration for nodes created by Karpenter.

```yaml
apiVersion: karpenter.ovhcloud.sh/v1alpha1
kind: OVHNodeClass
metadata:
  name: default
spec:
  # OVHcloud Public Cloud project ID (required)
  serviceName: "abc123def456..."

  # MKS cluster ID (optional - auto-detected from cluster)
  kubeId: "cluster-xyz..."

  # OVHcloud region (optional - auto-detected from cluster)
  # Options: GRA7, GRA9, GRA11, SBG5, RBX-A, UK1, DE1, WAW1, BHS5, EU-WEST-PAR, etc.
  region: "GRA7"

  # Reference to Secret containing credentials (optional if defined globally)
  credentialsSecretRef:
    name: ovh-credentials
    namespace: karpenter

  # Monthly billing (optional, default: false)
  # Only available on gen2 instances: b2, c2, d2, r2
  monthlyBilled: false

  # Anti-affinity between nodes in the same pool (optional, default: false)
  antiAffinity: false
```

### NodePool

The `NodePool` defines autoscaling rules and node constraints.

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: general-purpose
spec:
  template:
    metadata:
      labels:
        # Custom labels applied to nodes
        team: platform
        environment: production
    spec:
      # Reference to OVHNodeClass
      nodeClassRef:
        group: karpenter.ovhcloud.sh
        kind: OVHNodeClass
        name: default

      # Node requirements
      requirements:
        # Allowed instance types
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["b2-7", "b2-15", "b2-30", "b2-60", "b2-120"]

        # CPU architecture
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]

        # Capacity type (on-demand only on OVHcloud)
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["on-demand"]

        # Specific zones (optional)
        - key: topology.kubernetes.io/zone
          operator: In
          values: ["gra7-a", "gra7-b", "gra7-c"]

      # Taints applied to nodes (optional)
      taints:
        - key: dedicated
          value: gpu
          effect: NoSchedule

      # Node expiration time (optional)
      # After this time, the node will be replaced (for updates, etc.)
      expireAfter: 720h  # 30 days

  # NodePool global limits (IMPORTANT: always define limits!)
  limits:
    # Total CPU limit (recommended: start small)
    cpu: 100
    # Total memory limit
    memory: 200Gi

  # Disruption policy (consolidation, etc.)
  disruption:
    # Consolidation policy
    consolidationPolicy: WhenEmptyOrUnderutilized

    # Delay before consolidation after scale-down
    consolidateAfter: 30s

    # Disruption budget (how many nodes can be disrupted simultaneously)
    budgets:
      - nodes: "10%"
      - nodes: "0"
        schedule: "0 9 * * 1-5"  # No disruption during business hours
        duration: 8h
```

---

## Optimization Parameters

### Scaling Performance

| Parameter | Location | Description | Recommended Value |
|-----------|----------|-------------|-------------------|
| `consolidateAfter` | NodePool.spec.disruption | Delay before consolidation | `30s` to `5m` |
| `expireAfter` | NodePool.spec.template.spec | Max node lifetime | `720h` (30 days) |
| `limits.cpu` | NodePool.spec | Total CPU limit | Based on budget |
| `limits.memory` | NodePool.spec | Total memory limit | Based on budget |

### Instance Selection

| Parameter | Location | Description | Example |
|-----------|----------|-------------|---------|
| `instance-type` | requirements | Allowed flavors | `["b2-7", "b2-15"]` |
| `capacity-type` | requirements | Type (on-demand) | `["on-demand"]` |
| `topology.kubernetes.io/zone` | requirements | Allowed zones | `["gra7-a"]` |

### Available OVHcloud Flavors

#### General Purpose (Generation 2)
| Flavor | vCPUs | RAM | Storage | Recommended Use |
|--------|-------|-----|---------|-----------------|
| b2-7 | 2 | 7 GiB | 50 GiB | Light workloads, dev |
| b2-15 | 4 | 15 GiB | 100 GiB | Standard applications |
| b2-30 | 8 | 30 GiB | 200 GiB | Databases, CI/CD |
| b2-60 | 16 | 60 GiB | 400 GiB | Intensive workloads |
| b2-120 | 32 | 120 GiB | 400 GiB | Big data, ML |

#### General Purpose (Generation 3 - Recommended)
| Flavor | vCPUs | RAM | Storage | Recommended Use |
|--------|-------|-----|---------|-----------------|
| b3-8 | 2 | 8 GiB | 25 GiB | Dev, testing |
| b3-16 | 4 | 16 GiB | 50 GiB | Web applications |
| b3-32 | 8 | 32 GiB | 50 GiB | Microservices |
| b3-64 | 16 | 64 GiB | 50 GiB | Intensive applications |
| b3-128 | 32 | 128 GiB | 50 GiB | Heavy workloads |

#### Compute Optimized
| Flavor | vCPUs | RAM | Storage | Recommended Use |
|--------|-------|-----|---------|-----------------|
| c2-7 | 2 | 7 GiB | 50 GiB | Light compute |
| c2-15 | 4 | 15 GiB | 100 GiB | CI/CD, builds |
| c2-30 | 8 | 30 GiB | 200 GiB | Batch processing |
| c2-60 | 16 | 60 GiB | 400 GiB | Light HPC |
| c2-120 | 32 | 120 GiB | 400 GiB | HPC |
| c3-8 | 2 | 8 GiB | 25 GiB | Compute (Gen3) |
| c3-16 | 4 | 16 GiB | 50 GiB | Compute (Gen3) |
| c3-32 | 8 | 32 GiB | 50 GiB | Compute (Gen3) |
| c3-64 | 16 | 64 GiB | 50 GiB | Compute (Gen3) |
| c3-128 | 32 | 128 GiB | 50 GiB | Compute (Gen3) |

#### Memory Optimized
| Flavor | vCPUs | RAM | Storage | Recommended Use |
|--------|-------|-----|---------|-----------------|
| r2-15 | 2 | 15 GiB | 50 GiB | Cache, Redis |
| r2-30 | 2 | 30 GiB | 50 GiB | Elasticsearch |
| r2-60 | 4 | 60 GiB | 100 GiB | Databases |
| r2-120 | 8 | 120 GiB | 200 GiB | In-memory DBs |
| r2-240 | 16 | 240 GiB | 400 GiB | SAP, large DBs |
| r3-16 | 2 | 16 GiB | 25 GiB | Memory (Gen3) |
| r3-32 | 2 | 32 GiB | 25 GiB | Memory (Gen3) |
| r3-64 | 4 | 64 GiB | 50 GiB | Memory (Gen3) |
| r3-128 | 8 | 128 GiB | 50 GiB | Memory (Gen3) |
| r3-256 | 16 | 256 GiB | 50 GiB | Memory (Gen3) |

#### GPU Instances
| Flavor | vCPUs | RAM | GPU | Recommended Use |
|--------|-------|-----|-----|-----------------|
| t1-45 | 4 | 45 GiB | 1x V100 | ML inference |
| t1-90 | 8 | 90 GiB | 2x V100 | ML training |
| t1-180 | 16 | 180 GiB | 4x V100 | Deep learning |
| t2-45 | 4 | 45 GiB | 1x V100S | ML inference (Gen2) |
| t2-90 | 8 | 90 GiB | 2x V100S | ML training (Gen2) |
| t2-180 | 16 | 180 GiB | 4x V100S | Deep learning (Gen2) |

---

## Configuration Examples

### Cost-effective Configuration (dev/test)

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: dev-pool
spec:
  template:
    spec:
      nodeClassRef:
        group: karpenter.ovhcloud.sh
        kind: OVHNodeClass
        name: default
      requirements:
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["b2-7", "b2-15"]  # Small instances only
  limits:
    cpu: 50
    memory: 100Gi
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
    consolidateAfter: 10s  # Fast consolidation to save costs
```

### High Availability Production Configuration

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: production-pool
spec:
  template:
    metadata:
      labels:
        environment: production
    spec:
      nodeClassRef:
        group: karpenter.ovhcloud.sh
        kind: OVHNodeClass
        name: default
      requirements:
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["b2-30", "b2-60", "b2-120"]
        - key: topology.kubernetes.io/zone
          operator: In
          values: ["gra7-a", "gra7-b", "gra7-c"]  # Multi-AZ
      expireAfter: 168h  # 7 days - frequent renewal
  limits:
    cpu: 100
    memory: 200Gi
  disruption:
    consolidationPolicy: WhenEmpty  # Conservative consolidation
    consolidateAfter: 5m
    budgets:
      - nodes: "20%"  # Max 20% nodes disrupted
      - nodes: "0"
        schedule: "0 8 * * 1-5"  # No disruption 8am-6pm weekdays
        duration: 10h
```

### GPU/ML Configuration

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: gpu-pool
spec:
  template:
    spec:
      nodeClassRef:
        group: karpenter.ovhcloud.sh
        kind: OVHNodeClass
        name: default
      requirements:
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["t2-45", "t2-90", "t2-180"]  # GPU instances
      taints:
        - key: nvidia.com/gpu
          value: "true"
          effect: NoSchedule
  limits:
    cpu: 100
```

---

## Testing Autoscaling

### Test Deployment (inflate)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inflate
spec:
  replicas: 0
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

### Test Commands

```bash
# 1. Check initial state
kubectl get nodes
kubectl get nodepools
kubectl get nodeclaims

# 2. Trigger scale-up
kubectl scale deployment/inflate --replicas=10

# 3. Watch node creation
kubectl get nodeclaims -w
kubectl get pods -w

# 4. Check Karpenter logs
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter -f

# 5. Test scale-down
kubectl scale deployment/inflate --replicas=0

# 6. Watch consolidation (after consolidateAfter)
kubectl get nodes -w
```

---

## Troubleshooting

### NodePool stays "Not Ready"

```bash
# Check OVHNodeClass status
kubectl describe ovhnodeclass default

# The Ready condition should be True
# If False, verify credentials and configuration
```

### No nodes are created

```bash
# Check Karpenter logs
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter

# Check events
kubectl get events -n karpenter

# Common causes:
# - Invalid credentials
# - OVHcloud quota reached
# - Flavor not available in the region
```

### Error "no instance type has enough resources"

Verify that NodePool requirements allow flavors with enough resources for your pods.

```yaml
# Pod requests 4 CPUs but only b2-7 (2 CPUs) is allowed
requirements:
  - key: node.kubernetes.io/instance-type
    operator: In
    values: ["b2-7"]  # Insufficient

# Solution: allow larger flavors
requirements:
  - key: node.kubernetes.io/instance-type
    operator: In
    values: ["b2-7", "b2-15", "b2-30"]  # OK
```

### Error "availabilityZones is mandatory"

Ensure a zone is specified or the default zone is configured:

```yaml
requirements:
  - key: topology.kubernetes.io/zone
    operator: In
    values: ["gra7-a"]  # Explicit zone
```

---

## Internal Architecture

### Node Creation Flow

```
1. Pod Pending (insufficient resources)
        |
2. Karpenter detects the pod
        |
3. Calculate best flavor based on requirements
        |
4. Create NodeClaim
        |
5. Call OVHcloud API: create/scale Node Pool
        |
6. OVHcloud provisions the VM
        |
7. Node joins the cluster
        |
8. Pod scheduled on the new node
```

### OVHcloud Node Pool Naming Convention

Karpenter uses shared pools named: `karpenter-{flavor}-{zone}`

Example: `karpenter-b2-30-gra7-a`

This approach allows:
- Reusing existing pools
- Respecting the 100 pools per cluster limit
- Optimizing costs

---

## Best Practices

1. **Define limits**: Always configure `limits.cpu` and `limits.memory` to avoid uncontrolled costs

2. **Use consolidation**: Enable `consolidationPolicy: WhenEmptyOrUnderutilized` to optimize costs

3. **Multi-zone**: Specify multiple zones for high availability

4. **Disruption budgets**: Configure maintenance windows to avoid disruptions in production

5. **Monitoring**: Watch Karpenter metrics:
   - `karpenter_nodes_created_total`
   - `karpenter_nodes_terminated_total`
   - `karpenter_pods_state`

---

## Savings Plans

OVHcloud offers [Savings Plans](https://www.ovhcloud.com/en/public-cloud/savings-plan/) for B3, C3, and R3 instances (including MKS). These plans provide significant discounts (up to 50%) in exchange for a commitment period (1-36 months).

### Important Notes

- **Karpenter does not manage Savings Plans** - You must create and manage them manually via the OVHcloud Control Panel
- **No automatic optimization** - Karpenter does not consider Savings Plan coverage when selecting instance types
- **Recommendation**: If you have Savings Plans, configure your NodePool requirements to prefer covered instance types (b3, c3, r3)

### Recommended Configuration with Savings Plans

If you have a Savings Plan for B3 instances, configure your NodePool like this:

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: savings-plan-pool
spec:
  template:
    spec:
      nodeClassRef:
        group: karpenter.ovhcloud.sh
        kind: OVHNodeClass
        name: default
      requirements:
        # Prefer instances covered by your Savings Plan
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["b3-8", "b3-16", "b3-32", "b3-64", "b3-128"]
```

See [OVHcloud Savings Plan documentation](https://www.ovhcloud.com/en/public-cloud/savings-plan/) for more details on creating and managing Savings Plans.

---

## Useful Links

- [Official Karpenter Documentation](https://karpenter.sh/docs/)
- [OVHcloud Cloud API](https://api.ovh.com/console/#/cloud)
- [OVHcloud MKS Documentation](https://help.ovhcloud.com/csm/en-public-cloud-kubernetes)
- [Karpenter GitHub Repository](https://github.com/kubernetes-sigs/karpenter)
