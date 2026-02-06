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

package client

import (
	corev1 "k8s.io/api/core/v1"
)

// NodePool represents an OVH MKS Node Pool
type NodePool struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	FlavorName       string            `json:"flavor"`
	DesiredNodes     int               `json:"desiredNodes"`
	CurrentNodes     int               `json:"currentNodes"`
	MinNodes         int               `json:"minNodes"`
	MaxNodes         int               `json:"maxNodes"`
	Autoscale        bool              `json:"autoscale"`
	MonthlyBilled    bool              `json:"monthlyBilled"`
	AntiAffinity     bool              `json:"antiAffinity"`
	Status           string            `json:"status"`
	AvailabilityZone string            `json:"availabilityZone,omitempty"`
	Template         *NodePoolTemplate `json:"template,omitempty"`
	CreatedAt        string            `json:"createdAt"`
	UpdatedAt        string            `json:"updatedAt"`
}

// NodePoolTemplate defines the template for nodes in a pool
type NodePoolTemplate struct {
	Metadata NodePoolTemplateMetadata `json:"metadata"` // Required by OVHcloud MKS API
	Spec     NodePoolTemplateSpec     `json:"spec"`     // Required by OVHcloud MKS API
}

// NodePoolTemplateMetadata defines metadata for nodes
type NodePoolTemplateMetadata struct {
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations"` // Required by OVHcloud MKS API, cannot be omitempty
	Finalizers  []string          `json:"finalizers"`  // Required by OVHcloud MKS API, cannot be omitempty
}

// NodePoolTemplateSpec defines spec for nodes
type NodePoolTemplateSpec struct {
	Taints        []corev1.Taint `json:"taints"`        // Required by OVHcloud MKS API, cannot be omitempty
	Unschedulable bool           `json:"unschedulable"` // Required by OVHcloud MKS API, cannot be omitempty
}

// CreateNodePoolRequest is the request body for creating a node pool
type CreateNodePoolRequest struct {
	Name              string            `json:"name"`
	FlavorName        string            `json:"flavorName"`
	DesiredNodes      int               `json:"desiredNodes"`
	MinNodes          int               `json:"minNodes,omitempty"`
	MaxNodes          int               `json:"maxNodes,omitempty"`
	Autoscale         bool              `json:"autoscale"`
	MonthlyBilled     bool              `json:"monthlyBilled,omitempty"`
	AntiAffinity      bool              `json:"antiAffinity,omitempty"`
	AvailabilityZones []string          `json:"availabilityZones,omitempty"`
	Template          *NodePoolTemplate `json:"template,omitempty"`
}

// UpdateNodePoolRequest is the request body for updating a node pool
type UpdateNodePoolRequest struct {
	DesiredNodes int `json:"desiredNodes"`
}

// Flavor represents an OVH instance flavor
type Flavor struct {
	Name      string `json:"name"`
	Category  string `json:"category"`
	VCPUs     int    `json:"vcpus"`
	RAM       int    `json:"ram"`  // in MiB
	Disk      int    `json:"disk"` // in GiB
	GPUs      int    `json:"gpus,omitempty"`
	Available bool   `json:"available"`
	State     string `json:"state"`
}

// Node represents an OVH MKS Node
type Node struct {
	ID         string `json:"id"`
	PoolID     string `json:"nodePoolId"`
	InstanceID string `json:"instanceId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Flavor     string `json:"flavor"`
	Version    string `json:"version"`
	CreatedAt  string `json:"createdAt"`
	IsUpToDate bool   `json:"isUpToDate"`
}

// Credentials holds OVH API credentials
type Credentials struct {
	Endpoint          string
	ApplicationKey    string
	ApplicationSecret string
	ConsumerKey       string
}

// KubeFlavorCapability represents a flavor from the OVH capabilities API
// This is different from Flavor which comes from the cluster-specific endpoint
type KubeFlavorCapability struct {
	Name     string `json:"name"`
	Category string `json:"category"` // b, c, r, d, i, t, a, g, h, l
	VCPUs    int    `json:"vCPUs"`    // Note: capital C and Ps in JSON
	RAM      int    `json:"ram"`      // in GiB (not MiB like Flavor)
	GPUs     int    `json:"gpus"`
	State    string `json:"state"` // "available" or other
}

// KubeCluster represents an OVH MKS cluster
type KubeCluster struct {
	ID                          string                `json:"id"`
	Name                        string                `json:"name"`
	Region                      string                `json:"region"`  // e.g., "EU-WEST-PAR" (uppercase)
	Version                     string                `json:"version"` // Kubernetes version
	Status                      string                `json:"status"`  // READY, INSTALLING, etc.
	ControlPlaneIsUpToDate      bool                  `json:"controlPlaneIsUpToDate"`
	IsUpToDate                  bool                  `json:"isUpToDate"`
	NextUpgradeVersions         []string              `json:"nextUpgradeVersions,omitempty"`
	NodesURL                    string                `json:"nodesUrl,omitempty"`
	PrivateNetworkID            string                `json:"privateNetworkId,omitempty"`
	PrivateNetworkConfiguration *PrivateNetworkConfig `json:"privateNetworkConfiguration,omitempty"`
	CreatedAt                   string                `json:"createdAt"`
	UpdatedAt                   string                `json:"updatedAt"`
}

// PrivateNetworkConfig represents the private network configuration of an MKS cluster
type PrivateNetworkConfig struct {
	DefaultVrackGateway            string `json:"defaultVrackGateway,omitempty"`
	PrivateNetworkRoutingAsDefault bool   `json:"privateNetworkRoutingAsDefault"`
}
