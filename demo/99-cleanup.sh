#!/bin/bash
# Karpenter OVHcloud Demo - Cleanup Script
# Usage: ./99-cleanup.sh
#
# This script removes all demo resources in the correct order.

set -e

echo "=============================================="
echo "  Karpenter OVHcloud Demo - Cleanup"
echo "=============================================="
echo ""

# Step 1: Delete test deployments
echo "Step 1/6: Deleting test deployments..."
kubectl delete deployment inflate small-app large-app ha-app --ignore-not-found --timeout=60s 2>/dev/null || true
echo "  -> Deployments deleted"

# Step 2: Delete NodePools (triggers node cleanup)
echo ""
echo "Step 2/6: Deleting NodePools..."
kubectl delete nodepool demo-pool multi-flavor multi-zone --ignore-not-found --timeout=60s 2>/dev/null || true
echo "  -> NodePools deleted"

# Step 3: Wait for nodes to be removed
echo ""
echo "Step 3/6: Waiting for Karpenter nodes to be removed..."
TIMEOUT=300
ELAPSED=0
while kubectl get nodes -l demo=karpenter-ovhcloud 2>/dev/null | grep -q .; do
    if [ $ELAPSED -ge $TIMEOUT ]; then
        echo "  -> WARNING: Timeout waiting for nodes to be removed"
        break
    fi
    echo "  -> Waiting for nodes... (${ELAPSED}s)"
    sleep 10
    ELAPSED=$((ELAPSED + 10))
done
echo "  -> Karpenter nodes removed"

# Step 4: Delete OVHNodeClasses
echo ""
echo "Step 4/6: Deleting OVHNodeClasses..."
kubectl delete ovhnodeclass default monthly-billing high-availability --ignore-not-found --timeout=60s 2>/dev/null || true
echo "  -> OVHNodeClasses deleted"

# Step 5: Uninstall Helm release
echo ""
echo "Step 5/6: Uninstalling Helm release..."
helm uninstall karpenter-ovhcloud -n karpenter 2>/dev/null || true
echo "  -> Helm release uninstalled"

# Step 6: Delete namespace
echo ""
echo "Step 6/6: Deleting namespace..."
kubectl delete namespace karpenter --timeout=120s 2>/dev/null || true
echo "  -> Namespace deleted"

# Optional: Delete CRDs
echo ""
read -p "Delete Karpenter CRDs? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "Deleting CRDs..."
    kubectl delete crd nodepools.karpenter.sh nodeclaims.karpenter.sh ovhnodeclasses.karpenter.ovhcloud.sh 2>/dev/null || true
    echo "  -> CRDs deleted"
fi

echo ""
echo "=============================================="
echo "  Cleanup completed!"
echo "=============================================="
echo ""
echo "Verify with: kubectl get nodes"
