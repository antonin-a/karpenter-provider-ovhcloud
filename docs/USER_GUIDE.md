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

- Active OVHcloud MKS cluster **with at least one existing node**
  > ⚠️ **Important**: Karpenter runs as a Deployment inside your cluster and needs at least one node to be scheduled on. Your cluster must have a minimum of one node before installing Karpenter. You cannot use Karpenter to provision the initial node(s).
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

### Understanding CRDs

Karpenter uses three Custom Resource Definitions (CRDs):

| CRD | Source | Description |
|-----|--------|-------------|
| `NodePool` | Karpenter upstream | Defines autoscaling rules and node constraints |
| `NodeClaim` | Karpenter upstream | Internal resource tracking individual node requests |
| `OVHNodeClass` | This provider | OVHcloud-specific configuration (credentials, region, billing) |

**Important notes:**
- CRDs must exist before the controller starts (otherwise it crashes)
- CRDs are persistent: they survive control plane upgrades in managed clusters
- You only need to install CRDs once per cluster

### 1. Install CRDs

The **OVHNodeClass CRD** is automatically installed by the Helm chart (from `charts/crds/`).

You only need to install the **Karpenter core CRDs** manually:

```bash
# Install Karpenter core CRDs (NodePool and NodeClaim)
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/main/pkg/apis/crds/karpenter.sh_nodepools.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/main/pkg/apis/crds/karpenter.sh_nodeclaims.yaml
```

> **Note**: The OVHNodeClass CRD (`charts/crds/karpenter.ovhcloud.sh_ovhnodeclasses.yaml`) is installed automatically by Helm during step 3.

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
| `instance-type` | requirements | Allowed flavors | `["b3-8", "b3-16", "b3-32"]` |
| `capacity-type` | requirements | Type (on-demand) | `["on-demand"]` |
| `topology.kubernetes.io/zone` | requirements | Allowed zones | `["eu-west-par-a"]` |

### Available OVHcloud Flavors

> **Note**: Flavor availability varies by region. Use the OVH API to get the current list of available flavors for your region (see [Discovering Available Flavors](#discovering-available-flavors) below).

#### Cost Optimization Options

| Generation | Instances | Cost Option | How to Enable |
|------------|-----------|-------------|---------------|
| Gen2 | b2, c2, d2, r2 | **Monthly Billing** (~50% savings) | Set `monthlyBilled: true` in OVHNodeClass |
| Gen3 | b3, c3, r3 | **Savings Plans** (up to 50% savings) | Purchase via [OVHcloud Console](https://www.ovhcloud.com/en/public-cloud/savings-plan/) |

> **Important**: Monthly billing and Savings Plans are mutually exclusive options for different instance generations.

#### General Purpose (b series)

**Generation 2** (monthly billing available):
| Flavor | vCPUs | RAM |
|--------|-------|-----|
| b2-7 | 2 | 7 GiB |
| b2-15 | 4 | 15 GiB |
| b2-30 | 8 | 30 GiB |
| b2-60 | 16 | 60 GiB |
| b2-120 | 32 | 120 GiB |

**Generation 3** (Savings Plans compatible):
| Flavor | vCPUs | RAM |
|--------|-------|-----|
| b3-8 | 2 | 8 GiB |
| b3-16 | 4 | 16 GiB |
| b3-32 | 8 | 32 GiB |
| b3-64 | 16 | 64 GiB |
| b3-128 | 32 | 128 GiB |
| b3-256 | 64 | 256 GiB |
| b3-512 | 128 | 512 GiB |
| b3-640 | 160 | 640 GiB |

#### Compute Optimized (c series)

**Generation 2** (monthly billing available):
| Flavor | vCPUs | RAM |
|--------|-------|-----|
| c2-7 | 2 | 7 GiB |
| c2-15 | 4 | 15 GiB |
| c2-30 | 8 | 30 GiB |
| c2-60 | 16 | 60 GiB |
| c2-120 | 32 | 120 GiB |

**Generation 3** (Savings Plans compatible):
| Flavor | vCPUs | RAM |
|--------|-------|-----|
| c3-4 | 2 | 4 GiB |
| c3-8 | 4 | 8 GiB |
| c3-16 | 8 | 16 GiB |
| c3-32 | 16 | 32 GiB |
| c3-64 | 32 | 64 GiB |
| c3-128 | 64 | 128 GiB |
| c3-256 | 128 | 256 GiB |
| c3-320 | 160 | 320 GiB |

#### Memory Optimized (r series)

**Generation 2** (monthly billing available):
| Flavor | vCPUs | RAM |
|--------|-------|-----|
| r2-15 | 2 | 15 GiB |
| r2-30 | 2 | 30 GiB |
| r2-60 | 4 | 60 GiB |
| r2-120 | 8 | 120 GiB |
| r2-240 | 16 | 240 GiB |

**Generation 3** (Savings Plans compatible):
| Flavor | vCPUs | RAM |
|--------|-------|-----|
| r3-16 | 2 | 16 GiB |
| r3-32 | 4 | 32 GiB |
| r3-64 | 8 | 64 GiB |
| r3-128 | 16 | 128 GiB |
| r3-256 | 32 | 256 GiB |
| r3-512 | 64 | 512 GiB |
| r3-1024 | 128 | 1024 GiB |

#### Discovery (d series) - Monthly billing available

Entry-level instances for testing and development:
| Flavor | vCPUs | RAM |
|--------|-------|-----|
| d2-4 | 2 | 4 GiB |
| d2-8 | 4 | 8 GiB |

#### IOPS Optimized (i series)

High I/O performance instances:
| Flavor | vCPUs | RAM |
|--------|-------|-----|
| i1-45 | 8 | 45 GiB |
| i1-90 | 16 | 90 GiB |
| i1-180 | 32 | 180 GiB |

#### GPU - Tesla V100 (t series)

| Flavor | vCPUs | RAM | GPUs |
|--------|-------|-----|------|
| t1-45 | 8 | 45 GiB | 1 |
| t1-90 | 18 | 90 GiB | 2 |
| t1-180 | 36 | 180 GiB | 4 |
| t1-le-45 | 8 | 45 GiB | 1 |
| t1-le-90 | 16 | 90 GiB | 2 |
| t1-le-180 | 32 | 180 GiB | 4 |
| t2-45 | 15 | 45 GiB | 1 |
| t2-90 | 30 | 90 GiB | 2 |
| t2-180 | 60 | 180 GiB | 4 |
| t2-le-45 | 14 | 45 GiB | 1 |
| t2-le-90 | 30 | 90 GiB | 2 |
| t2-le-180 | 60 | 180 GiB | 4 |

> Note: `-le` variants are "low energy" versions.

#### GPU - NVIDIA A10 (a series)

| Flavor | vCPUs | RAM | GPUs |
|--------|-------|-----|------|
| a10-45 | 30 | 45 GiB | 1 |
| a10-90 | 60 | 90 GiB | 2 |
| a10-180 | 120 | 180 GiB | 4 |

#### GPU - NVIDIA A100 (a series)

| Flavor | vCPUs | RAM | GPUs |
|--------|-------|-----|------|
| a100-180 | 15 | 180 GiB | 1 |
| a100-360 | 30 | 360 GiB | 2 |
| a100-720 | 60 | 720 GiB | 4 |

#### GPU - NVIDIA L4 (l series)

| Flavor | vCPUs | RAM | GPUs |
|--------|-------|-----|------|
| l4-90 | 22 | 90 GiB | 1 |
| l4-180 | 45 | 180 GiB | 2 |
| l4-360 | 90 | 360 GiB | 4 |

#### GPU - NVIDIA L40S (l series)

| Flavor | vCPUs | RAM | GPUs |
|--------|-------|-----|------|
| l40s-90 | 15 | 90 GiB | 1 |
| l40s-180 | 30 | 180 GiB | 2 |
| l40s-360 | 60 | 360 GiB | 4 |

#### GPU - NVIDIA H100 (h series)

| Flavor | vCPUs | RAM | GPUs |
|--------|-------|-----|------|
| h100-380 | 30 | 380 GiB | 1 |
| h100-760 | 60 | 760 GiB | 2 |
| h100-1520 | 120 | 1520 GiB | 4 |

#### GPU - NVIDIA RTX 5000 (g series)

| Flavor | vCPUs | RAM | GPUs |
|--------|-------|-----|------|
| rtx5000-28 | 4 | 28 GiB | 1 |
| rtx5000-56 | 8 | 56 GiB | 2 |
| rtx5000-84 | 16 | 84 GiB | 3 |

### Discovering Available Flavors

The flavor list above is not exhaustive and availability varies by region. To get the current list of available flavors for your project and region, use the OVH API.

#### Available MKS Regions

As of the API query, the following regions support MKS:

| Region | Location |
|--------|----------|
| EU-WEST-PAR | Paris, France (3 AZ) |
| EU-SOUTH-MIL | Milan, Italy (3 AZ) |
| GRA7, GRA9, GRA11 | Gravelines, France |
| SBG5 | Strasbourg, France |
| RBX-A | Roubaix, France |
| UK1 | London, UK |
| DE1 | Frankfurt, Germany |
| WAW1 | Warsaw, Poland |
| BHS5 | Beauharnois, Canada |
| SGP1 | Singapore |
| SYD1 | Sydney, Australia |
| AP-SOUTH-MUM-1 | Mumbai, India |

#### Query flavors via API

Use the [OVH API Console](https://eu.api.ovh.com/console/) to query available flavors:

```
GET /cloud/project/{serviceName}/capabilities/kube/flavors?region={region}
```

Or for a specific cluster:

```
GET /cloud/project/{serviceName}/kube/{kubeId}/flavors
```

#### Example API response

```json
[
  {
    "name": "b3-8",
    "category": "b",
    "state": "available",
    "vCPUs": 2,
    "gpus": 0,
    "ram": 8,
    "isLocalStorage": false
  },
  {
    "name": "t2-45",
    "category": "t",
    "state": "available",
    "vCPUs": 15,
    "gpus": 1,
    "ram": 45,
    "isLocalStorage": false
  }
]
```

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
          values: ["b3-8", "b3-16"]  # Small gen3 instances (Savings Plans compatible)
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
          values: ["b3-32", "b3-64", "b3-128"]  # Gen3 (Savings Plans compatible)
        - key: topology.kubernetes.io/zone
          operator: In
          values: ["eu-west-par-a", "eu-west-par-b", "eu-west-par-c"]  # Multi-AZ
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

### Memory-intensive Workloads

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: memory-pool
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
          values: ["r3-64", "r3-128", "r3-256", "r3-512"]  # Memory optimized gen3
  limits:
    cpu: 200
    memory: 2000Gi
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
          values: ["l4-90", "l4-180", "l40s-90", "l40s-180"]  # NVIDIA L4/L40S for inference
      taints:
        - key: nvidia.com/gpu
          value: "true"
          effect: NoSchedule
  limits:
    cpu: 100
```

### AI Training Configuration

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: ai-training-pool
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
          values: ["a100-180", "a100-360", "h100-380", "h100-760"]  # High-end GPUs for training
      taints:
        - key: nvidia.com/gpu
          value: "true"
          effect: NoSchedule
  limits:
    cpu: 200
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
# Pod requests 4 CPUs but only b3-8 (2 CPUs) is allowed
requirements:
  - key: node.kubernetes.io/instance-type
    operator: In
    values: ["b3-8"]  # Insufficient

# Solution: allow larger flavors
requirements:
  - key: node.kubernetes.io/instance-type
    operator: In
    values: ["b3-8", "b3-16", "b3-32"]  # OK
```

### Error "availabilityZones is mandatory"

Ensure a zone is specified or the default zone is configured:

```yaml
requirements:
  - key: topology.kubernetes.io/zone
    operator: In
    values: ["eu-west-par-a"]  # Explicit zone
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

Example: `karpenter-b3-32-eu-west-par-a`

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
