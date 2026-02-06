#!/bin/bash
# Karpenter OVHcloud Demo - Prerequisites Verification
# Usage: ./00-verify-prereqs.sh

set -e

echo "=============================================="
echo "  Karpenter OVHcloud - Prerequisites Check"
echo "=============================================="
echo ""

# Check kubectl
echo -n "kubectl: "
if kubectl version --client --short 2>/dev/null; then
    echo "  -> OK"
else
    echo "  -> NOT INSTALLED"
    exit 1
fi

# Check helm
echo -n "helm: "
if helm version --short 2>/dev/null; then
    echo "  -> OK"
else
    echo "  -> NOT INSTALLED"
    exit 1
fi

# Check cluster access
echo ""
echo "=== Cluster Access ==="
if kubectl cluster-info 2>/dev/null | head -1; then
    echo "  -> Cluster accessible"
else
    echo "  -> CLUSTER NOT ACCESSIBLE"
    exit 1
fi

# Check existing nodes
echo ""
echo "=== Current Nodes ==="
kubectl get nodes

# Check if Karpenter already exists
echo ""
echo "=== Karpenter Status ==="
if kubectl get ns karpenter 2>/dev/null; then
    echo "  -> WARNING: karpenter namespace already exists"
else
    echo "  -> No existing installation (expected)"
fi

# Check environment variables
echo ""
echo "=== Environment Variables ==="
MISSING_VARS=""

if [ -z "$OVH_APPLICATION_KEY" ]; then
    MISSING_VARS="$MISSING_VARS OVH_APPLICATION_KEY"
fi
if [ -z "$OVH_APPLICATION_SECRET" ]; then
    MISSING_VARS="$MISSING_VARS OVH_APPLICATION_SECRET"
fi
if [ -z "$OVH_CONSUMER_KEY" ]; then
    MISSING_VARS="$MISSING_VARS OVH_CONSUMER_KEY"
fi
if [ -z "$OVH_SERVICE_NAME" ]; then
    MISSING_VARS="$MISSING_VARS OVH_SERVICE_NAME"
fi
if [ -z "$OVH_KUBE_ID" ]; then
    MISSING_VARS="$MISSING_VARS OVH_KUBE_ID"
fi
if [ -z "$OVH_REGION" ]; then
    MISSING_VARS="$MISSING_VARS OVH_REGION"
fi

if [ -n "$MISSING_VARS" ]; then
    echo "  -> Missing variables:$MISSING_VARS"
    echo ""
    echo "Please set these variables before running the demo:"
    echo "  export OVH_APPLICATION_KEY=\"your-app-key\""
    echo "  export OVH_APPLICATION_SECRET=\"your-app-secret\""
    echo "  export OVH_CONSUMER_KEY=\"your-consumer-key\""
    echo "  export OVH_SERVICE_NAME=\"your-project-id\""
    echo "  export OVH_KUBE_ID=\"your-cluster-id\""
    echo "  export OVH_REGION=\"EU-WEST-PAR\""
else
    echo "  OVH_APPLICATION_KEY: set"
    echo "  OVH_APPLICATION_SECRET: set"
    echo "  OVH_CONSUMER_KEY: set"
    echo "  OVH_SERVICE_NAME: $OVH_SERVICE_NAME"
    echo "  OVH_KUBE_ID: $OVH_KUBE_ID"
    echo "  OVH_REGION: $OVH_REGION"
fi

echo ""
echo "=============================================="
echo "  Prerequisites check completed"
echo "=============================================="
