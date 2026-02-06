# Karpenter OVHcloud Demo

Step-by-step demonstration guide for Karpenter on OVHcloud Managed Kubernetes Service (MKS).

**Total duration:** 30-45 minutes

## Prerequisites

### Required tools
- `kubectl` configured with access to your MKS cluster
- `helm` v3.x
- `envsubst` (typically included in `gettext`)

### OVHcloud credentials
Create API credentials on [OVH API Console](https://eu.api.ovh.com/createToken/):

```bash
# Required environment variables
export OVH_APPLICATION_KEY="your-application-key"
export OVH_APPLICATION_SECRET="your-application-secret"
export OVH_CONSUMER_KEY="your-consumer-key"
export OVH_SERVICE_NAME="your-project-id"  # 32 hex characters
export OVH_KUBE_ID="your-cluster-id"
export OVH_REGION="EU-WEST-PAR"  # or GRA7, SBG5, etc.
```

### Required API permissions
```
GET    /cloud/project/*/kube/*
POST   /cloud/project/*/kube/*/nodepool
PUT    /cloud/project/*/kube/*/nodepool/*
DELETE /cloud/project/*/kube/*/nodepool/*
GET    /cloud/project/*/kube/*/nodepool
GET    /cloud/project/*/kube/*/nodepool/*/nodes
GET    /cloud/project/*/kube/*/flavors
GET    /cloud/project/*/capabilities/kube/*
```

## Demo structure

| File | Section | Description |
|------|---------|-------------|
| `00-verify-prereqs.sh` | Setup | Prerequisites verification |
| `01-namespace-secret.yaml` | Installation | Namespace and credentials |
| `02-ovhnodeclass.yaml` | Installation | OVHcloud configuration |
| `03-nodepool-basic.yaml` | Autoscaling | Basic NodePool |
| `04-inflate.yaml` | Autoscaling | Test deployment |
| `05-nodepool-multi-flavor.yaml` | Flavors | Multi-flavor NodePool |
| `06-small-workload.yaml` | Flavors | Small workload |
| `07-large-workload.yaml` | Flavors | Large workload |
| `08-nodepool-multizone.yaml` | Multi-zone | HA NodePool |
| `09-zone-spread.yaml` | Multi-zone | Zone-spread deployment |
| `10-ovhnodeclass-monthly.yaml` | Advanced | Monthly billing |
| `11-nodepool-monthly.yaml` | Advanced | Monthly billing NodePool |
| `12-ovhnodeclass-antiaffinity.yaml` | Advanced | Anti-affinity |
| `99-cleanup.sh` | Cleanup | Full cleanup |

## Demo walkthrough

### 1. Prerequisites verification (pre-demo)

```bash
chmod +x 00-verify-prereqs.sh
./00-verify-prereqs.sh
```

### 2. Installation

```bash
# Install Karpenter core CRDs (NodePool and NodeClaim)
# Note: OVHNodeClass CRD is installed automatically by Helm
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/main/pkg/apis/crds/karpenter.sh_nodepools.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/main/pkg/apis/crds/karpenter.sh_nodeclaims.yaml

# Create namespace and secret
envsubst < 01-namespace-secret.yaml | kubectl apply -f -

# Install Karpenter via Helm
helm install karpenter-ovhcloud ../charts/karpenter-ovhcloud \
  --namespace karpenter \
  --set ovh.serviceName="${OVH_SERVICE_NAME}" \
  --set ovh.kubeId="${OVH_KUBE_ID}" \
  --set ovh.region="${OVH_REGION}"

# Verify installation
kubectl get pods -n karpenter
```

### 3. Basic autoscaling

**Terminal 1 - Watch nodes:**
```bash
watch kubectl get nodes
```

**Terminal 2 - Watch nodeclaims:**
```bash
watch kubectl get nodeclaims
```

**Terminal 3 - Actions:**
```bash
# Apply configuration
envsubst < 02-ovhnodeclass.yaml | kubectl apply -f -
kubectl apply -f 03-nodepool-basic.yaml
kubectl apply -f 04-inflate.yaml

# Trigger scale-up
kubectl scale deployment/inflate --replicas=3

# Observe:
# 1. Pods in Pending state
# 2. NodeClaim created
# 3. OVH pool created (karpenter-b3-8-eu-west-par-a)
# 4. Node joins the cluster (~2-3 min)
# 5. Pods Running

# Scale down
kubectl scale deployment/inflate --replicas=0
# Observe consolidation after 1 minute
```

### 4. Flavor selection

```bash
# NodePool with multiple flavors
kubectl apply -f 05-nodepool-multi-flavor.yaml

# Test 1: Small workload -> selects b3-8
kubectl apply -f 06-small-workload.yaml
kubectl get nodeclaims -o wide

# Test 2: Large workload -> selects b3-32
kubectl apply -f 07-large-workload.yaml
kubectl get nodeclaims -o wide
```

### 5. Multi-zone

```bash
kubectl apply -f 08-nodepool-multizone.yaml
kubectl apply -f 09-zone-spread.yaml

# Observe the distribution
kubectl get nodes -L topology.kubernetes.io/zone
```

### 6. Consolidation

```bash
# Create multiple nodes
kubectl scale deployment/inflate --replicas=10
# Wait for all nodes to be Ready

# Reduce the load
kubectl scale deployment/inflate --replicas=2

# Observe consolidation
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-ovhcloud | grep -i consolidat
```

### 7. Advanced features

```bash
# Monthly billing (~50% savings, gen2 instances only: b2, c2, d2, r2)
envsubst < 10-ovhnodeclass-monthly.yaml | kubectl apply -f -
kubectl apply -f 11-nodepool-monthly.yaml

# Anti-affinity (spread across hypervisors, max 5 nodes/pool)
envsubst < 12-ovhnodeclass-antiaffinity.yaml | kubectl apply -f -

# Drift detection
kubectl describe nodeclaim <name>
```

### 8. Monitoring and troubleshooting

```bash
# Karpenter logs
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-ovhcloud -f

# Resource status
kubectl describe nodepool demo-pool
kubectl describe ovhnodeclass default
kubectl get nodeclaims -o wide

# Events
kubectl get events -n karpenter --sort-by='.lastTimestamp'
```

### 9. Cleanup

```bash
chmod +x 99-cleanup.sh
./99-cleanup.sh
```

## Key takeaways

1. **Shared Node Pools strategy**: Naming convention `karpenter-{flavor}-{zone}` to respect the 100 pools/cluster limit

2. **On-demand only**: OVHcloud MKS does not support spot instances

3. **Pool operations**:
   - Scale up: Increment `desiredNodes` or create new pool
   - Scale down: Decrement or delete pool (if only 1 node)

4. **Zone format**: `{region}-a/b/c` (e.g., `eu-west-par-a`)

5. **3-AZ regions**: EU-WEST-PAR and EU-SOUTH-MIL only

## Available OVHcloud flavors

> Use the OVH API to get the current list: `GET /cloud/project/{serviceName}/capabilities/kube/flavors?region={region}`

**Cost optimization:**
- **Gen2** (b2, c2, d2, r2): Monthly billing via `monthlyBilled: true` in OVHNodeClass
- **Gen3** (b3, c3, r3): Savings Plans via OVHcloud Console

| Category | Examples | Description |
|----------|----------|-------------|
| b2-* | b2-7, b2-15, b2-30, b2-60, b2-120 | General Purpose (gen2, monthly billing) |
| b3-* | b3-8, b3-16, b3-32, b3-64, b3-128, b3-256, b3-512 | General Purpose (gen3, Savings Plans) |
| c2-* | c2-7, c2-15, c2-30, c2-60, c2-120 | Compute Optimized (gen2, monthly billing) |
| c3-* | c3-4, c3-8, c3-16, c3-32, c3-64, c3-128, c3-256 | Compute Optimized (gen3, Savings Plans) |
| r2-* | r2-15, r2-30, r2-60, r2-120, r2-240 | Memory Optimized (gen2, monthly billing) |
| r3-* | r3-16, r3-32, r3-64, r3-128, r3-256, r3-512, r3-1024 | Memory Optimized (gen3, Savings Plans) |
| d2-* | d2-4, d2-8 | Discovery (gen2, monthly billing) |
| i1-* | i1-45, i1-90, i1-180 | IOPS Optimized |
| t1-*/t2-* | t1-45, t1-90, t2-45, t2-90, t2-180 | GPU Tesla V100 |
| a10-*/a100-* | a10-45, a10-90, a100-180, a100-360 | GPU NVIDIA A10/A100 |
| l4-*/l40s-* | l4-90, l4-180, l40s-90, l40s-180 | GPU NVIDIA L4/L40S |
| h100-* | h100-380, h100-760, h100-1520 | GPU NVIDIA H100 |
| rtx5000-* | rtx5000-28, rtx5000-56, rtx5000-84 | GPU NVIDIA RTX 5000 |

## Troubleshooting

| Issue | Diagnosis | Solution |
|-------|-----------|----------|
| NodePool Not Ready | `kubectl describe ovhnodeclass` | Check credentials |
| No node created | Karpenter logs | Check flavor requirements |
| Creation timeout | Logs + OVH Console | OVH quotas |
| Node doesn't join | `kubectl get nodeclaims` | Check region/zone |

## Useful links

- [Karpenter Documentation](https://karpenter.sh/docs/)
- [OVHcloud API](https://eu.api.ovh.com/console/)
- [OVHcloud MKS](https://www.ovhcloud.com/en/public-cloud/kubernetes/)
