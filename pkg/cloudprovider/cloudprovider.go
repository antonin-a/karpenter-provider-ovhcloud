/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ovhcloud

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/awslabs/operatorpkg/status"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/ovh/karpenter-provider-ovhcloud/pkg/apis/v1alpha1"
	ovhclient "github.com/ovh/karpenter-provider-ovhcloud/pkg/client"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

// CloudProvider implements the Karpenter CloudProvider interface for OVHcloud MKS
type CloudProvider struct {
	kubeClient    client.Client
	ovhClient     *ovhclient.OVHClient
	pricingClient *ovhclient.PricingClient
	instanceTypes []*cloudprovider.InstanceType

	// Mutex for pool operations
	mu sync.RWMutex
	// Cache of pool names to pool IDs
	poolCache map[string]string
}

// NewCloudProvider creates a new OVHcloud CloudProvider
func NewCloudProvider(ctx context.Context, kubeClient client.Client, ovhClient *ovhclient.OVHClient, instanceTypes []*cloudprovider.InstanceType) *CloudProvider {
	return &CloudProvider{
		kubeClient:    kubeClient,
		ovhClient:     ovhClient,
		pricingClient: ovhclient.NewPricingClient("FR"), // Default to FR subsidiary
		instanceTypes: instanceTypes,
		poolCache:     make(map[string]string),
	}
}

// NewCloudProviderWithPricing creates a new OVHcloud CloudProvider with custom pricing client
func NewCloudProviderWithPricing(ctx context.Context, kubeClient client.Client, ovhClient *ovhclient.OVHClient, pricingClient *ovhclient.PricingClient, instanceTypes []*cloudprovider.InstanceType) *CloudProvider {
	return &CloudProvider{
		kubeClient:    kubeClient,
		ovhClient:     ovhClient,
		pricingClient: pricingClient,
		instanceTypes: instanceTypes,
		poolCache:     make(map[string]string),
	}
}

// Create launches a NodeClaim by creating or scaling up an OVH Node Pool
func (c *CloudProvider) Create(ctx context.Context, nodeClaim *v1.NodeClaim) (*v1.NodeClaim, error) {
	logger := log.FromContext(ctx)
	startTime := time.Now()

	// Resolve NodeClass
	nodeClass, err := c.resolveNodeClass(ctx, nodeClaim)
	if err != nil {
		if errors.IsNotFound(err) {
			RecordNodeProvisioning("unknown", "unknown", "error")
			return nil, cloudprovider.NewInsufficientCapacityError(fmt.Errorf("resolving node class: %w", err))
		}
		RecordNodeProvisioning("unknown", "unknown", "error")
		return nil, fmt.Errorf("resolving node class: %w", err)
	}

	// Check if NodeClass is ready
	if readyCondition := nodeClass.StatusConditions().Get(status.ConditionReady); readyCondition.IsFalse() {
		RecordNodeProvisioning("unknown", "unknown", "nodeclass_not_ready")
		return nil, cloudprovider.NewNodeClassNotReadyError(stderrors.New(readyCondition.Message))
	}

	// Determine flavor and zone from requirements
	flavor, err := c.selectFlavor(nodeClaim)
	if err != nil {
		RecordNodeProvisioning("unknown", "unknown", "no_flavor")
		return nil, fmt.Errorf("selecting flavor: %w", err)
	}

	zone := c.selectZone(nodeClaim)
	poolName := c.poolName(flavor, zone)

	logger.Info("Creating node", "flavor", flavor, "zone", zone, "poolName", poolName)

	// Get or create the pool with labels and taints from NodeClaim
	pool, err := c.getOrCreatePool(ctx, poolName, flavor, zone, nodeClass, nodeClaim)
	if err != nil {
		RecordNodeProvisioning(flavor, zone, "pool_error")
		return nil, fmt.Errorf("getting/creating pool: %w", err)
	}

	// Wait for a new node to appear
	node, err := c.waitForNewNode(ctx, pool.ID, pool.CurrentNodes)
	if err != nil {
		RecordNodeProvisioning(flavor, zone, "timeout")
		return nil, fmt.Errorf("waiting for new node: %w", err)
	}

	// Record successful provisioning metrics
	duration := time.Since(startTime).Seconds()
	RecordNodeProvisioning(flavor, zone, "success")
	RecordNodeProvisioningDuration(flavor, zone, duration)

	logger.Info("Node created", "nodeID", node.ID, "nodeName", node.Name, "poolID", pool.ID, "durationSeconds", duration)

	// Build the response NodeClaim
	instanceType, _ := c.getInstanceType(flavor)
	created := nodeClaim.DeepCopy()
	// Use OpenStack instance ID format to match what OVH MKS sets on nodes
	created.Status.ProviderID = fmt.Sprintf("%s%s", ProviderPrefix, node.InstanceID)
	created.Status.Capacity = c.getCapacityForFlavor(instanceType)
	created.Status.Allocatable = c.getAllocatableForFlavor(instanceType)

	// Add annotations for tracking
	if created.Annotations == nil {
		created.Annotations = make(map[string]string)
	}
	created.Annotations[v1alpha1.AnnotationOVHPoolID] = pool.ID
	created.Annotations[v1alpha1.AnnotationOVHNodeID] = node.ID
	created.Annotations[v1alpha1.AnnotationOVHNodeName] = node.Name

	// Add labels
	if created.Labels == nil {
		created.Labels = make(map[string]string)
	}
	created.Labels[corev1.LabelInstanceTypeStable] = flavor
	created.Labels[corev1.LabelTopologyZone] = zone
	created.Labels[v1.CapacityTypeLabelKey] = v1.CapacityTypeOnDemand

	return created, nil
}

// Delete removes a NodeClaim by scaling down or deleting the OVH Node Pool
func (c *CloudProvider) Delete(ctx context.Context, nodeClaim *v1.NodeClaim) error {
	logger := log.FromContext(ctx)
	startTime := time.Now()

	poolID := nodeClaim.Annotations[v1alpha1.AnnotationOVHPoolID]
	if poolID == "" {
		RecordNodeDeletion("no_pool_id")
		return cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("no pool ID annotation"))
	}

	// Get the current pool state
	pool, err := c.ovhClient.GetNodePool(ctx, poolID)
	if err != nil {
		// Pool might already be deleted
		RecordNodeDeletion("pool_not_found")
		return cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("pool not found: %w", err))
	}

	logger.Info("Deleting node", "poolID", poolID, "currentNodes", pool.CurrentNodes)

	if pool.DesiredNodes <= 1 {
		// Delete the entire pool
		if err := c.ovhClient.DeleteNodePool(ctx, poolID); err != nil {
			RecordNodeDeletion("delete_error")
			RecordPoolOperation("delete", "error")
			return fmt.Errorf("deleting pool: %w", err)
		}
		RecordPoolOperation("delete", "success")
		// Clear from cache
		c.mu.Lock()
		for name, id := range c.poolCache {
			if id == poolID {
				delete(c.poolCache, name)
				break
			}
		}
		c.mu.Unlock()
	} else {
		// Scale down by 1
		_, err := c.ovhClient.UpdateNodePool(ctx, poolID, &ovhclient.UpdateNodePoolRequest{
			DesiredNodes: pool.DesiredNodes - 1,
		})
		if err != nil {
			RecordNodeDeletion("scale_down_error")
			RecordPoolOperation("scale_down", "error")
			return fmt.Errorf("scaling down pool: %w", err)
		}
		RecordPoolOperation("scale_down", "success")
	}

	// Record successful deletion metrics
	duration := time.Since(startTime).Seconds()
	RecordNodeDeletion("success")
	RecordNodeDeletionDuration(duration)

	return cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("instance terminated"))
}

// Get retrieves a NodeClaim by provider ID
func (c *CloudProvider) Get(ctx context.Context, providerID string) (*v1.NodeClaim, error) {
	// Parse providerID: openstack:///{instanceId}
	instanceID := strings.TrimPrefix(providerID, ProviderPrefix)
	if instanceID == "" || instanceID == providerID {
		return nil, cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("invalid provider ID format: %s", providerID))
	}

	// Search all Karpenter pools for the node with this instanceId
	pools, err := c.ovhClient.ListNodePools(ctx)
	if err != nil {
		return nil, cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("listing pools: %w", err))
	}

	for _, pool := range pools {
		if !strings.HasPrefix(pool.Name, PoolNamePrefix) {
			continue
		}

		nodes, err := c.ovhClient.ListPoolNodes(ctx, pool.ID)
		if err != nil {
			continue
		}

		for _, node := range nodes {
			if node.InstanceID == instanceID {
				return c.nodeToNodeClaim(&node, pool.ID)
			}
		}
	}

	return nil, cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("node not found"))
}

// List retrieves all Karpenter-managed NodeClaims
func (c *CloudProvider) List(ctx context.Context) ([]*v1.NodeClaim, error) {
	pools, err := c.ovhClient.ListNodePools(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing pools: %w", err)
	}

	var nodeClaims []*v1.NodeClaim
	for _, pool := range pools {
		// Only list Karpenter-managed pools
		if !strings.HasPrefix(pool.Name, PoolNamePrefix) {
			continue
		}

		nodes, err := c.ovhClient.ListPoolNodes(ctx, pool.ID)
		if err != nil {
			continue
		}

		for _, node := range nodes {
			nc, err := c.nodeToNodeClaim(&node, pool.ID)
			if err != nil {
				continue
			}
			nodeClaims = append(nodeClaims, nc)
		}
	}

	return nodeClaims, nil
}

// GetInstanceTypes returns available instance types
func (c *CloudProvider) GetInstanceTypes(ctx context.Context, nodePool *v1.NodePool) ([]*cloudprovider.InstanceType, error) {
	SetInstanceTypesAvailable(len(c.instanceTypes))
	return c.instanceTypes, nil
}

// IsDrifted checks if a NodeClaim has drifted from its NodeClass
// This is called by Karpenter core to determine if a node should be replaced
// Note: Karpenter core also performs its own drift detection (RequirementsDrifted)
// by comparing NodeClaim requirements against the node's actual labels
func (c *CloudProvider) IsDrifted(ctx context.Context, nodeClaim *v1.NodeClaim) (cloudprovider.DriftReason, error) {
	logger := log.FromContext(ctx)

	nodeClass, err := c.resolveNodeClass(ctx, nodeClaim)
	if err != nil {
		// If we can't resolve the NodeClass, don't report drift
		// This avoids false positives during NodeClass creation/deletion
		logger.V(1).Info("Cannot resolve NodeClass for drift detection", "nodeClaim", nodeClaim.Name, "error", err)
		return "", nil
	}

	poolID := nodeClaim.Annotations[v1alpha1.AnnotationOVHPoolID]
	if poolID == "" {
		// No pool ID means this NodeClaim was created before pool tracking
		// Don't report drift to avoid unnecessary replacements
		logger.V(1).Info("No pool ID annotation for drift detection", "nodeClaim", nodeClaim.Name)
		return "", nil
	}

	pool, err := c.ovhClient.GetNodePool(ctx, poolID)
	if err != nil {
		// Pool might have been deleted or API error
		// Don't report drift on transient errors
		logger.V(1).Info("Cannot get pool for drift detection", "poolID", poolID, "error", err)
		return "", nil
	}

	// Check monthly billing drift
	// This is a billing configuration that can't be changed on existing pools
	if pool.MonthlyBilled != nodeClass.Spec.MonthlyBilled {
		logger.Info("Drift detected: MonthlyBillingChanged",
			"nodeClaim", nodeClaim.Name,
			"pool", pool.Name,
			"poolMonthlyBilled", pool.MonthlyBilled,
			"nodeClassMonthlyBilled", nodeClass.Spec.MonthlyBilled)
		RecordDriftDetection("MonthlyBillingChanged")
		return "MonthlyBillingChanged", nil
	}

	// Check anti-affinity drift
	// This is a placement configuration that can't be changed on existing pools
	if pool.AntiAffinity != nodeClass.Spec.AntiAffinity {
		logger.Info("Drift detected: AntiAffinityChanged",
			"nodeClaim", nodeClaim.Name,
			"pool", pool.Name,
			"poolAntiAffinity", pool.AntiAffinity,
			"nodeClassAntiAffinity", nodeClass.Spec.AntiAffinity)
		RecordDriftDetection("AntiAffinityChanged")
		return "AntiAffinityChanged", nil
	}

	// No OVHcloud-specific drift detected
	// Note: Karpenter core may still detect RequirementsDrifted if node labels
	// don't match the NodeClaim requirements. This is expected behavior when
	// MKS doesn't apply all the labels we request in the pool template.
	return "", nil
}

// Name returns the provider name
func (c *CloudProvider) Name() string {
	return "ovhcloud"
}

// GetSupportedNodeClasses returns the supported NodeClass types
func (c *CloudProvider) GetSupportedNodeClasses() []status.Object {
	return []status.Object{&v1alpha1.OVHNodeClass{}}
}

// RepairPolicies returns the repair policies for unhealthy nodes
func (c *CloudProvider) RepairPolicies() []cloudprovider.RepairPolicy {
	return []cloudprovider.RepairPolicy{
		{
			ConditionType:      corev1.NodeReady,
			ConditionStatus:    corev1.ConditionFalse,
			TolerationDuration: 10 * time.Minute,
		},
		{
			ConditionType:      corev1.NodeReady,
			ConditionStatus:    corev1.ConditionUnknown,
			TolerationDuration: 10 * time.Minute,
		},
	}
}

// Helper methods

func (c *CloudProvider) resolveNodeClass(ctx context.Context, nodeClaim *v1.NodeClaim) (*v1alpha1.OVHNodeClass, error) {
	nodeClass := &v1alpha1.OVHNodeClass{}
	if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaim.Spec.NodeClassRef.Name}, nodeClass); err != nil {
		return nil, err
	}
	return nodeClass, nil
}

func (c *CloudProvider) poolName(flavor, zone string) string {
	// Sanitize flavor name for pool naming
	safeFlavor := strings.ReplaceAll(flavor, ".", "-")
	if zone != "" {
		return fmt.Sprintf("%s%s-%s", PoolNamePrefix, safeFlavor, zone)
	}
	return fmt.Sprintf("%s%s", PoolNamePrefix, safeFlavor)
}

func (c *CloudProvider) selectFlavor(nodeClaim *v1.NodeClaim) (string, error) {
	// Find the instance type requirement
	for _, req := range nodeClaim.Spec.Requirements {
		if req.Key == corev1.LabelInstanceTypeStable && len(req.Values) > 0 {
			// Return the first matching instance type
			return req.Values[0], nil
		}
	}
	return "", fmt.Errorf("no instance type requirement found")
}

func (c *CloudProvider) selectZone(nodeClaim *v1.NodeClaim) string {
	for _, req := range nodeClaim.Spec.Requirements {
		if req.Key == corev1.LabelTopologyZone && len(req.Values) > 0 {
			return req.Values[0]
		}
	}
	// Default to zone "a" of the configured region
	// OVHcloud availability zones follow the pattern: {region}-a, {region}-b, {region}-c
	region := c.ovhClient.GetRegion()
	return strings.ToLower(region) + "-a"
}

func (c *CloudProvider) getOrCreatePool(ctx context.Context, poolName, flavor, zone string, nodeClass *v1alpha1.OVHNodeClass, nodeClaim *v1.NodeClaim) (*ovhclient.NodePool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check cache first
	if poolID, ok := c.poolCache[poolName]; ok {
		pool, err := c.ovhClient.GetNodePool(ctx, poolID)
		if err == nil {
			// Scale up the pool
			_, err = c.ovhClient.UpdateNodePool(ctx, poolID, &ovhclient.UpdateNodePoolRequest{
				DesiredNodes: pool.DesiredNodes + 1,
			})
			if err != nil {
				return nil, fmt.Errorf("scaling up pool: %w", err)
			}
			pool.DesiredNodes++
			return pool, nil
		}
		// Pool might have been deleted, remove from cache
		delete(c.poolCache, poolName)
	}

	// Check if pool exists in OVH
	pools, err := c.ovhClient.ListNodePools(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing pools: %w", err)
	}

	for _, pool := range pools {
		if pool.Name == poolName {
			// Update cache and scale up
			c.poolCache[poolName] = pool.ID
			_, err = c.ovhClient.UpdateNodePool(ctx, pool.ID, &ovhclient.UpdateNodePoolRequest{
				DesiredNodes: pool.DesiredNodes + 1,
			})
			if err != nil {
				return nil, fmt.Errorf("scaling up existing pool: %w", err)
			}
			pool.DesiredNodes++
			return &pool, nil
		}
	}

	// Create new pool
	req := &ovhclient.CreateNodePoolRequest{
		Name:          poolName,
		FlavorName:    flavor,
		DesiredNodes:  DefaultDesiredNodes,
		Autoscale:     false,
		MonthlyBilled: nodeClass.Spec.MonthlyBilled,
		AntiAffinity:  nodeClass.Spec.AntiAffinity,
	}

	if zone != "" {
		req.AvailabilityZones = []string{zone}
	}

	// Build labels for the node template
	// These labels are applied to nodes by MKS and are critical for Karpenter to match nodes to NodeClaims
	labels := make(map[string]string)

	// Standard Karpenter management labels
	labels["managed-by"] = "karpenter"

	// Standard Kubernetes labels that Karpenter uses for scheduling decisions
	// These MUST match what we set on the NodeClaim for drift detection to work correctly
	labels[corev1.LabelInstanceTypeStable] = flavor
	labels[corev1.LabelTopologyZone] = zone
	labels[v1.CapacityTypeLabelKey] = v1.CapacityTypeOnDemand
	labels[corev1.LabelArchStable] = v1.ArchitectureAmd64
	labels[corev1.LabelOSStable] = string(corev1.Linux)

	// Add Karpenter-specific labels
	labels["karpenter.sh/registered"] = "true"

	// Add the NodePool name from the NodeClaim if available
	if nodeClaim != nil && nodeClaim.Labels != nil {
		if nodePoolName, ok := nodeClaim.Labels[v1.NodePoolLabelKey]; ok {
			labels[v1.NodePoolLabelKey] = nodePoolName
		}
	}

	// Add user-defined tags from NodeClass
	for k, v := range nodeClass.Spec.Tags {
		labels[k] = v
	}

	// Build taints from NodeClaim spec
	var taints []corev1.Taint
	if nodeClaim != nil && nodeClaim.Spec.Taints != nil {
		taints = append(taints, nodeClaim.Spec.Taints...)
	}

	// Build annotations and finalizers for the node template
	// OVHcloud MKS API requires these fields to be set (even if empty)
	annotations := make(map[string]string)
	finalizers := []string{}

	req.Template = &ovhclient.NodePoolTemplate{
		Metadata: ovhclient.NodePoolTemplateMetadata{
			Labels:      labels,
			Annotations: annotations,
			Finalizers:  finalizers,
		},
		Spec: ovhclient.NodePoolTemplateSpec{
			Taints: taints,
		},
	}

	pool, err := c.ovhClient.CreateNodePool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("creating pool: %w", err)
	}

	c.poolCache[poolName] = pool.ID
	return pool, nil
}

func (c *CloudProvider) waitForNewNode(ctx context.Context, poolID string, previousCount int) (*ovhclient.Node, error) {
	timeout := time.After(10 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for new node")
		case <-ticker.C:
			nodes, err := c.ovhClient.ListPoolNodes(ctx, poolID)
			if err != nil {
				continue
			}

			// Look for a READY node with InstanceID populated
			// The InstanceID is the OpenStack instance ID needed for provider ID matching
			for _, node := range nodes {
				if node.Status == "READY" && node.InstanceID != "" {
					return &node, nil
				}
			}
		}
	}
}

func (c *CloudProvider) getInstanceType(name string) (*cloudprovider.InstanceType, error) {
	it, found := lo.Find(c.instanceTypes, func(it *cloudprovider.InstanceType) bool {
		return it.Name == name
	})
	if !found {
		return nil, fmt.Errorf("instance type not found: %s", name)
	}
	return it, nil
}

func (c *CloudProvider) getCapacityForFlavor(instanceType *cloudprovider.InstanceType) corev1.ResourceList {
	if instanceType == nil {
		return corev1.ResourceList{}
	}
	return instanceType.Capacity
}

func (c *CloudProvider) getAllocatableForFlavor(instanceType *cloudprovider.InstanceType) corev1.ResourceList {
	if instanceType == nil {
		return corev1.ResourceList{}
	}
	return instanceType.Allocatable()
}

func (c *CloudProvider) nodeToNodeClaim(node *ovhclient.Node, poolID string) (*v1.NodeClaim, error) {
	// Extract zone from pool name: karpenter-{flavor}-{zone}
	// e.g., karpenter-b3-8-eu-west-par-a -> eu-west-par-a
	zone := c.extractZoneFromPoolName(poolID)
	if zone == "" {
		// Fallback to default zone
		region := c.ovhClient.GetRegion()
		zone = strings.ToLower(region) + "-a"
	}

	return &v1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: node.Name,
			Annotations: map[string]string{
				v1alpha1.AnnotationOVHPoolID:   poolID,
				v1alpha1.AnnotationOVHNodeID:   node.ID,
				v1alpha1.AnnotationOVHNodeName: node.Name,
			},
			Labels: map[string]string{
				corev1.LabelInstanceTypeStable: node.Flavor,
				corev1.LabelTopologyZone:       zone,
				v1.CapacityTypeLabelKey:        v1.CapacityTypeOnDemand,
				corev1.LabelArchStable:         v1.ArchitectureAmd64,
				corev1.LabelOSStable:           string(corev1.Linux),
			},
		},
		Status: v1.NodeClaimStatus{
			NodeName:   node.Name,
			ProviderID: fmt.Sprintf("%s%s", ProviderPrefix, node.InstanceID),
		},
	}, nil
}

// extractZoneFromPoolName extracts the zone from a pool name or ID
// Pool names follow: karpenter-{flavor}-{zone}
func (c *CloudProvider) extractZoneFromPoolName(poolID string) string {
	// First, try to get the pool to get its actual zone
	ctx := context.Background()
	pool, err := c.ovhClient.GetNodePool(ctx, poolID)
	if err == nil && pool.AvailabilityZone != "" {
		return pool.AvailabilityZone
	}

	// Fallback: try to find pool in cache and extract zone from name
	c.mu.RLock()
	defer c.mu.RUnlock()
	for name, id := range c.poolCache {
		if id == poolID {
			// Extract zone from name: karpenter-{flavor}-{zone}
			// Zone format is region-letter (e.g., eu-west-par-a)
			parts := strings.Split(name, "-")
			if len(parts) >= 4 {
				// Reconstruct zone from last 4 parts (e.g., eu-west-par-a)
				return strings.Join(parts[len(parts)-4:], "-")
			}
		}
	}
	return ""
}

// ConstructInstanceTypes builds instance types from OVH flavors (uses estimated pricing)
func ConstructInstanceTypes(ctx context.Context, ovhClient *ovhclient.OVHClient) ([]*cloudprovider.InstanceType, error) {
	return ConstructInstanceTypesWithPricing(ctx, ovhClient, nil)
}

// ConstructInstanceTypesWithPricing builds instance types from OVH capabilities API with real pricing
// Falls back to cluster-specific endpoint if capabilities API is not available
func ConstructInstanceTypesWithPricing(ctx context.Context, ovhClient *ovhclient.OVHClient, pricingClient *ovhclient.PricingClient) ([]*cloudprovider.InstanceType, error) {
	logger := log.FromContext(ctx)
	region := ovhClient.GetRegion()

	// Try capabilities API first (more complete and region-aware)
	capFlavors, err := ovhClient.ListKubeFlavors(ctx, region)
	if err == nil && len(capFlavors) > 0 {
		logger.Info("Retrieved flavors from OVH Capabilities API", "region", region, "count", len(capFlavors))
		return buildInstanceTypesFromCapabilities(ctx, capFlavors, region, pricingClient)
	}

	// Fallback to cluster-specific endpoint
	logger.Info("Capabilities API unavailable, falling back to cluster flavors", "error", err)

	flavors, err := ovhClient.ListFlavors(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing flavors: %w", err)
	}

	logger.Info("Retrieved flavors from cluster API", "count", len(flavors))

	return buildInstanceTypesFromClusterFlavors(ctx, flavors, region, pricingClient)
}

// buildInstanceTypesFromCapabilities builds instance types from capabilities API response
func buildInstanceTypesFromCapabilities(ctx context.Context, capFlavors []ovhclient.KubeFlavorCapability, region string, pricingClient *ovhclient.PricingClient) ([]*cloudprovider.InstanceType, error) {
	var instanceTypes []*cloudprovider.InstanceType

	for _, capFlavor := range capFlavors {
		// Skip unavailable flavors or those without CPU info
		if capFlavor.State != "available" || capFlavor.VCPUs == 0 {
			continue
		}

		// Convert to internal Flavor type for compatibility
		// Note: Capabilities API returns RAM in GiB, not MiB
		flavor := ovhclient.Flavor{
			Name:      capFlavor.Name,
			Category:  capFlavor.Category,
			VCPUs:     capFlavor.VCPUs,
			RAM:       capFlavor.RAM, // Already in GiB from capabilities API
			GPUs:      capFlavor.GPUs,
			Available: true,
			State:     capFlavor.State,
		}

		it := buildInstanceType(ctx, flavor, region, pricingClient, true) // true = RAM already in GiB
		instanceTypes = append(instanceTypes, it)
	}

	return instanceTypes, nil
}

// buildInstanceTypesFromClusterFlavors builds instance types from cluster-specific flavors endpoint
func buildInstanceTypesFromClusterFlavors(ctx context.Context, flavors []ovhclient.Flavor, region string, pricingClient *ovhclient.PricingClient) ([]*cloudprovider.InstanceType, error) {
	var instanceTypes []*cloudprovider.InstanceType

	for _, flavor := range flavors {
		// Skip flavors without CPU info
		if flavor.VCPUs == 0 {
			continue
		}

		it := buildInstanceType(ctx, flavor, region, pricingClient, false) // false = RAM in MiB
		instanceTypes = append(instanceTypes, it)
	}

	return instanceTypes, nil
}

// buildInstanceType creates a single InstanceType from a Flavor
func buildInstanceType(ctx context.Context, flavor ovhclient.Flavor, region string, pricingClient *ovhclient.PricingClient, ramInGiB bool) *cloudprovider.InstanceType {
	// Build requirements including GPU if present
	requirements := scheduling.NewRequirements(
		scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, flavor.Name),
		scheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, v1.ArchitectureAmd64),
		scheduling.NewRequirement(corev1.LabelOSStable, corev1.NodeSelectorOpIn, string(corev1.Linux)),
		scheduling.NewRequirement(v1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, v1.CapacityTypeOnDemand),
		scheduling.NewRequirement(v1alpha1.LabelInstanceCategory, corev1.NodeSelectorOpIn, flavor.Category),
	)

	// Build capacity - handle RAM unit conversion
	var memoryStr string
	if ramInGiB {
		memoryStr = fmt.Sprintf("%dGi", flavor.RAM)
	} else {
		// Cluster API returns RAM in MiB, convert to GiB for Kubernetes
		memoryStr = fmt.Sprintf("%dMi", flavor.RAM)
	}

	capacity := corev1.ResourceList{
		corev1.ResourceCPU:              resource.MustParse(fmt.Sprintf("%d", flavor.VCPUs)),
		corev1.ResourceMemory:           resource.MustParse(memoryStr),
		corev1.ResourcePods:             resource.MustParse("110"),
		corev1.ResourceEphemeralStorage: resource.MustParse(fmt.Sprintf("%dGi", flavor.Disk)),
	}

	// Add GPU resources if present
	if flavor.GPUs > 0 {
		capacity[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse(fmt.Sprintf("%d", flavor.GPUs))
	}

	return &cloudprovider.InstanceType{
		Name:         flavor.Name,
		Requirements: requirements,
		Capacity:     capacity,
		Offerings:    buildOfferingsWithPricing(ctx, flavor, region, pricingClient),
		Overhead: &cloudprovider.InstanceTypeOverhead{
			KubeReserved: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
	}
}

// estimatePrice estimates the hourly price for a flavor
// This is a rough estimate; real pricing should come from OVH API
func estimatePrice(flavor ovhclient.Flavor) float64 {
	// Rough estimate: $0.02 per vCPU + $0.005 per GiB RAM
	cpuPrice := float64(flavor.VCPUs) * 0.02
	ramPrice := float64(flavor.RAM) / 1024 * 0.005

	// Add GPU pricing if present
	if flavor.GPUs > 0 {
		// Rough estimate: $0.50 per GPU hour
		cpuPrice += float64(flavor.GPUs) * 0.50
	}

	return cpuPrice + ramPrice
}

// buildOfferingsWithPricing creates offerings for a flavor with real or estimated pricing
func buildOfferingsWithPricing(ctx context.Context, flavor ovhclient.Flavor, region string, pricingClient *ovhclient.PricingClient) cloudprovider.Offerings {
	// OVHcloud availability zones follow the pattern: {region}-a, {region}-b, {region}-c
	regionLower := strings.ToLower(region)
	zones := []string{
		regionLower + "-a",
		regionLower + "-b",
		regionLower + "-c",
	}

	var offerings cloudprovider.Offerings
	for _, zone := range zones {
		// Get price from pricing client or use estimation
		var price float64
		if pricingClient != nil {
			var err error
			price, err = pricingClient.GetFlavorPrice(ctx, flavor.Name, region)
			if err != nil {
				price = estimatePrice(flavor)
			}
		} else {
			price = estimatePrice(flavor)
		}

		offerings = append(offerings, &cloudprovider.Offering{
			Requirements: scheduling.NewRequirements(
				scheduling.NewRequirement(v1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, v1.CapacityTypeOnDemand),
				scheduling.NewRequirement(corev1.LabelTopologyZone, corev1.NodeSelectorOpIn, zone),
			),
			Price:     price,
			Available: true,
		})
	}

	return offerings
}
