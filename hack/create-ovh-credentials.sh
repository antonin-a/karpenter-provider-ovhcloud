#!/bin/bash
#
# create-ovh-credentials.sh
#
# Helper script to create OVH API credentials with minimal permissions for Karpenter.
# The credentials are scoped to a specific MKS cluster only.
#
# Usage:
#   ./create-ovh-credentials.sh
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${GREEN}"
echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║     Karpenter OVHcloud - Restricted Credentials Helper        ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo -e "${NC}"

# Get configuration
echo -e "${YELLOW}First, let's gather information about your cluster:${NC}"
echo ""

if [ -z "$OVH_SERVICE_NAME" ]; then
    echo -n "Enter OVHcloud Project ID (serviceName): "
    read OVH_SERVICE_NAME
fi

if [ -z "$OVH_KUBE_ID" ]; then
    echo -n "Enter MKS Cluster ID (kubeId): "
    read OVH_KUBE_ID
fi

# Determine API endpoint
OVH_ENDPOINT="${OVH_ENDPOINT:-ovh-eu}"
case "$OVH_ENDPOINT" in
    ovh-eu)
        API_BASE="https://api.ovh.com"
        ;;
    ovh-ca)
        API_BASE="https://ca.api.ovh.com"
        ;;
    ovh-us)
        API_BASE="https://us.api.ovh.com"
        ;;
    *)
        echo -e "${RED}Error: Unknown endpoint '$OVH_ENDPOINT'. Use ovh-eu, ovh-ca, or ovh-us.${NC}"
        exit 1
        ;;
esac

# Build the pre-filled URL
PREFILLED_URL="${API_BASE}/createToken/?GET=/cloud/project/${OVH_SERVICE_NAME}/kube/${OVH_KUBE_ID}&GET=/cloud/project/${OVH_SERVICE_NAME}/kube/${OVH_KUBE_ID}/nodepool&GET=/cloud/project/${OVH_SERVICE_NAME}/kube/${OVH_KUBE_ID}/nodepool/*&POST=/cloud/project/${OVH_SERVICE_NAME}/kube/${OVH_KUBE_ID}/nodepool&PUT=/cloud/project/${OVH_SERVICE_NAME}/kube/${OVH_KUBE_ID}/nodepool/*&DELETE=/cloud/project/${OVH_SERVICE_NAME}/kube/${OVH_KUBE_ID}/nodepool/*&GET=/cloud/project/${OVH_SERVICE_NAME}/kube/${OVH_KUBE_ID}/flavors&GET=/cloud/project/${OVH_SERVICE_NAME}/capabilities/kube/*"

echo ""
echo -e "${YELLOW}Configuration:${NC}"
echo "  Endpoint:      $OVH_ENDPOINT"
echo "  Project ID:    $OVH_SERVICE_NAME"
echo "  Cluster ID:    $OVH_KUBE_ID"
echo ""

echo "═══════════════════════════════════════════════════════════════"
echo ""
echo -e "${GREEN}Step 1: Create API Keys${NC}"
echo ""
echo -e "Open this URL to create credentials with pre-filled permissions:"
echo ""
echo -e "${BLUE}${PREFILLED_URL}${NC}"
echo ""
echo -e "${YELLOW}The URL pre-fills the API permissions. You still need to fill in:${NC}"
echo ""
echo "  Application name:        karpenter-mks-${OVH_KUBE_ID}"
echo "  Application description: Karpenter autoscaler for MKS cluster ${OVH_KUBE_ID}"
echo -e "  Validity:                ${RED}Change from '1 day' to 'Unlimited'${NC} (important!)"
echo ""
echo "Click 'Create' and save the three credentials displayed."
echo ""
echo "═══════════════════════════════════════════════════════════════"
echo ""
echo -e "${GREEN}Step 2: Create Kubernetes Secret${NC}"
echo ""
echo "Once you have your credentials, run:"
echo ""
echo -e "${BLUE}kubectl create namespace karpenter${NC}"
echo ""
echo -e "${BLUE}kubectl create secret generic ovh-credentials -n karpenter \\
  --from-literal=applicationKey=YOUR_APPLICATION_KEY \\
  --from-literal=applicationSecret=YOUR_APPLICATION_SECRET \\
  --from-literal=consumerKey=YOUR_CONSUMER_KEY${NC}"
echo ""
echo "═══════════════════════════════════════════════════════════════"
echo ""
echo -e "${GREEN}Permissions granted (cluster-scoped only):${NC}"
echo "  - GET/POST/PUT/DELETE on node pools"
echo "  - GET cluster info and flavors"
echo "  - GET MKS capabilities"
echo ""
echo -e "${YELLOW}This key CANNOT:${NC}"
echo "  - Access other clusters in the project"
echo "  - Manage VMs, storage, or network resources"
echo "  - Access billing or account information"
echo ""

# Open URL or copy to clipboard
echo ""
echo -n "Would you like to (o)pen the URL in browser, (c)opy to clipboard, or (n)either? [o/c/N] "
read -r ACTION
case "$ACTION" in
    [Oo])
        if command -v xdg-open &> /dev/null; then
            xdg-open "$PREFILLED_URL"
            echo -e "${GREEN}Opening in browser...${NC}"
        elif command -v open &> /dev/null; then
            open "$PREFILLED_URL"
            echo -e "${GREEN}Opening in browser...${NC}"
        else
            echo -e "${YELLOW}Could not detect browser opener. Please copy the URL manually.${NC}"
        fi
        ;;
    [Cc])
        if command -v pbcopy &> /dev/null; then
            echo -n "$PREFILLED_URL" | pbcopy
            echo -e "${GREEN}URL copied to clipboard!${NC}"
        elif command -v xclip &> /dev/null; then
            echo -n "$PREFILLED_URL" | xclip -selection clipboard
            echo -e "${GREEN}URL copied to clipboard!${NC}"
        elif command -v xsel &> /dev/null; then
            echo -n "$PREFILLED_URL" | xsel --clipboard
            echo -e "${GREEN}URL copied to clipboard!${NC}"
        else
            echo -e "${YELLOW}Could not detect clipboard tool. Please copy the URL manually.${NC}"
        fi
        ;;
    *)
        echo "OK, no action taken."
        ;;
esac
