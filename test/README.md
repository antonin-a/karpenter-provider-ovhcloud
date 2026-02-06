# Karpenter OVHcloud Provider Tests

This directory contains tests and configuration for the Karpenter OVHcloud provider.

## Structure

```
test/
├── e2e/              # End-to-end tests against real MKS cluster
├── config.env        # Environment variables (gitignored)
├── kubeconfig        # MKS cluster kubeconfig (gitignored)
├── ovh.conf          # OVH API credentials (gitignored)
├── ssh_key           # SSH key for test VM (gitignored)
└── README.md
```

## Local Configuration

Create the following files in this directory (they are gitignored):

### ssh_key
SSH private key for accessing the test VM.

### ovh.conf
OVH API credentials in INI format:
```ini
[ovh-eu]
application_key=YOUR_APPLICATION_KEY
application_secret=YOUR_APPLICATION_SECRET
consumer_key=YOUR_CONSUMER_KEY
```

### kubeconfig
Kubeconfig file for the MKS cluster.

### config.env
Environment variables for testing:
```bash
export VM_HOST=x.x.x.x
export OVH_SERVICE_NAME=your-project-id
export OVH_KUBE_ID=your-cluster-id
export OVH_REGION=EU-WEST-PAR
```

## Usage

```bash
# Source the environment
source test/config.env

# Use the kubeconfig
export KUBECONFIG=$(pwd)/test/kubeconfig

# SSH to the test VM
ssh -i test/ssh_key ubuntu@$VM_HOST

# Run E2E tests
go test ./test/e2e/... -v
```

## Test Categories

- **Provisioning**: Test node creation based on pod scheduling
- **Consolidation**: Test node consolidation and optimization
- **Drift**: Test drift detection and node replacement
- **Disruption**: Test graceful node termination
