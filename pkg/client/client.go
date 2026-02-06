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
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/ovh/go-ovh/ovh"
)

// RetryConfig defines retry behavior for API calls
type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
}

// DefaultRetryConfig provides sensible defaults for retry behavior
var DefaultRetryConfig = RetryConfig{
	MaxRetries:     3,
	InitialBackoff: 1 * time.Second,
	MaxBackoff:     30 * time.Second,
	BackoffFactor:  2.0,
}

// isRetryableError checks if an error is worth retrying
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()

	// Retry on rate limiting (429)
	if strings.Contains(errStr, "429") || strings.Contains(errStr, "Too Many Requests") {
		return true
	}
	// Retry on server errors (5xx)
	if strings.Contains(errStr, "500") || strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "503") || strings.Contains(errStr, "504") {
		return true
	}
	// Retry on temporary network issues
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "temporary failure") {
		return true
	}
	return false
}

// calculateBackoff computes the backoff duration for a given retry attempt
func calculateBackoff(attempt int, config RetryConfig) time.Duration {
	backoff := float64(config.InitialBackoff) * math.Pow(config.BackoffFactor, float64(attempt))
	if backoff > float64(config.MaxBackoff) {
		backoff = float64(config.MaxBackoff)
	}
	// Add jitter (±25%)
	jitter := backoff * 0.25 * (rand.Float64()*2 - 1)
	return time.Duration(backoff + jitter)
}

// retryableAPICall wraps an API call with retry logic
func retryableAPICall[T any](ctx context.Context, config RetryConfig, operation string, fn func() (T, error)) (T, error) {
	var result T
	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		result, lastErr = fn()
		if lastErr == nil {
			return result, nil
		}

		if !isRetryableError(lastErr) {
			return result, lastErr
		}

		if attempt < config.MaxRetries {
			backoff := calculateBackoff(attempt, config)
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(backoff):
				// Continue to next retry
			}
		}
	}

	return result, fmt.Errorf("%s failed after %d retries: %w", operation, config.MaxRetries+1, lastErr)
}

// retryableVoidCall wraps a void API call with retry logic
func retryableVoidCall(ctx context.Context, config RetryConfig, operation string, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if !isRetryableError(lastErr) {
			return lastErr
		}

		if attempt < config.MaxRetries {
			backoff := calculateBackoff(attempt, config)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				// Continue to next retry
			}
		}
	}

	return fmt.Errorf("%s failed after %d retries: %w", operation, config.MaxRetries+1, lastErr)
}

// OVHClient wraps the OVH API client for Kubernetes operations
type OVHClient struct {
	client      *ovh.Client
	serviceName string
	kubeID      string
	region      string
	retryConfig RetryConfig
}

// NewOVHClient creates a new OVH API client
func NewOVHClient(creds *Credentials, serviceName, kubeID, region string) (*OVHClient, error) {
	client, err := ovh.NewClient(
		creds.Endpoint,
		creds.ApplicationKey,
		creds.ApplicationSecret,
		creds.ConsumerKey,
	)
	if err != nil {
		return nil, fmt.Errorf("creating OVH client: %w", err)
	}

	return &OVHClient{
		client:      client,
		serviceName: serviceName,
		kubeID:      kubeID,
		region:      region,
		retryConfig: DefaultRetryConfig,
	}, nil
}

// WithRetryConfig sets custom retry configuration
func (c *OVHClient) WithRetryConfig(config RetryConfig) *OVHClient {
	c.retryConfig = config
	return c
}

// basePath returns the base API path for the cluster
func (c *OVHClient) basePath() string {
	return fmt.Sprintf("/cloud/project/%s/kube/%s", c.serviceName, c.kubeID)
}

// ListNodePools returns all node pools in the cluster
func (c *OVHClient) ListNodePools(ctx context.Context) ([]NodePool, error) {
	path := fmt.Sprintf("%s/nodepool", c.basePath())
	return retryableAPICall(ctx, c.retryConfig, "ListNodePools", func() ([]NodePool, error) {
		var pools []NodePool
		if err := c.client.GetWithContext(ctx, path, &pools); err != nil {
			return nil, fmt.Errorf("listing node pools: %w", err)
		}
		return pools, nil
	})
}

// GetNodePool returns a specific node pool by ID
func (c *OVHClient) GetNodePool(ctx context.Context, poolID string) (*NodePool, error) {
	path := fmt.Sprintf("%s/nodepool/%s", c.basePath(), poolID)
	return retryableAPICall(ctx, c.retryConfig, "GetNodePool", func() (*NodePool, error) {
		var pool NodePool
		if err := c.client.GetWithContext(ctx, path, &pool); err != nil {
			return nil, fmt.Errorf("getting node pool %s: %w", poolID, err)
		}
		return &pool, nil
	})
}

// CreateNodePool creates a new node pool
func (c *OVHClient) CreateNodePool(ctx context.Context, req *CreateNodePoolRequest) (*NodePool, error) {
	path := fmt.Sprintf("%s/nodepool", c.basePath())
	return retryableAPICall(ctx, c.retryConfig, "CreateNodePool", func() (*NodePool, error) {
		var pool NodePool
		if err := c.client.PostWithContext(ctx, path, req, &pool); err != nil {
			return nil, fmt.Errorf("creating node pool: %w", err)
		}
		return &pool, nil
	})
}

// UpdateNodePool updates a node pool (mainly for scaling)
func (c *OVHClient) UpdateNodePool(ctx context.Context, poolID string, req *UpdateNodePoolRequest) (*NodePool, error) {
	path := fmt.Sprintf("%s/nodepool/%s", c.basePath(), poolID)
	return retryableAPICall(ctx, c.retryConfig, "UpdateNodePool", func() (*NodePool, error) {
		var pool NodePool
		if err := c.client.PutWithContext(ctx, path, req, &pool); err != nil {
			return nil, fmt.Errorf("updating node pool %s: %w", poolID, err)
		}
		return &pool, nil
	})
}

// DeleteNodePool deletes a node pool
func (c *OVHClient) DeleteNodePool(ctx context.Context, poolID string) error {
	path := fmt.Sprintf("%s/nodepool/%s", c.basePath(), poolID)
	return retryableVoidCall(ctx, c.retryConfig, "DeleteNodePool", func() error {
		if err := c.client.DeleteWithContext(ctx, path, nil); err != nil {
			return fmt.Errorf("deleting node pool %s: %w", poolID, err)
		}
		return nil
	})
}

// ListPoolNodes returns all nodes in a specific pool
func (c *OVHClient) ListPoolNodes(ctx context.Context, poolID string) ([]Node, error) {
	path := fmt.Sprintf("%s/nodepool/%s/nodes", c.basePath(), poolID)
	return retryableAPICall(ctx, c.retryConfig, "ListPoolNodes", func() ([]Node, error) {
		var nodes []Node
		if err := c.client.GetWithContext(ctx, path, &nodes); err != nil {
			return nil, fmt.Errorf("listing nodes in pool %s: %w", poolID, err)
		}
		return nodes, nil
	})
}

// ListFlavors returns available flavors for the cluster
func (c *OVHClient) ListFlavors(ctx context.Context) ([]Flavor, error) {
	path := fmt.Sprintf("%s/flavors", c.basePath())
	return retryableAPICall(ctx, c.retryConfig, "ListFlavors", func() ([]Flavor, error) {
		var flavors []Flavor
		if err := c.client.GetWithContext(ctx, path, &flavors); err != nil {
			return nil, fmt.Errorf("listing flavors: %w", err)
		}
		return flavors, nil
	})
}

// GetRegion returns the configured region
func (c *OVHClient) GetRegion() string {
	return c.region
}

// GetServiceName returns the configured service name (project ID)
func (c *OVHClient) GetServiceName() string {
	return c.serviceName
}

// GetKubeID returns the configured cluster ID
func (c *OVHClient) GetKubeID() string {
	return c.kubeID
}

// capabilitiesBasePath returns the base path for capabilities endpoints
func (c *OVHClient) capabilitiesBasePath() string {
	return fmt.Sprintf("/cloud/project/%s/capabilities/kube", c.serviceName)
}

// ListKubeRegions returns all available MKS regions for the project
func (c *OVHClient) ListKubeRegions(ctx context.Context) ([]string, error) {
	path := fmt.Sprintf("%s/regions", c.capabilitiesBasePath())
	return retryableAPICall(ctx, c.retryConfig, "ListKubeRegions", func() ([]string, error) {
		var regions []string
		if err := c.client.GetWithContext(ctx, path, &regions); err != nil {
			return nil, fmt.Errorf("listing kube regions: %w", err)
		}
		return regions, nil
	})
}

// ListKubeFlavors returns available MKS flavors for a specific region from the capabilities API
func (c *OVHClient) ListKubeFlavors(ctx context.Context, region string) ([]KubeFlavorCapability, error) {
	path := fmt.Sprintf("%s/flavors?region=%s", c.capabilitiesBasePath(), region)
	return retryableAPICall(ctx, c.retryConfig, "ListKubeFlavors", func() ([]KubeFlavorCapability, error) {
		var flavors []KubeFlavorCapability
		if err := c.client.GetWithContext(ctx, path, &flavors); err != nil {
			return nil, fmt.Errorf("listing kube flavors for region %s: %w", region, err)
		}
		return flavors, nil
	})
}

// GetCluster returns the MKS cluster information including the region
func (c *OVHClient) GetCluster(ctx context.Context) (*KubeCluster, error) {
	path := c.basePath()
	return retryableAPICall(ctx, c.retryConfig, "GetCluster", func() (*KubeCluster, error) {
		var cluster KubeCluster
		if err := c.client.GetWithContext(ctx, path, &cluster); err != nil {
			return nil, fmt.Errorf("getting cluster info: %w", err)
		}
		return &cluster, nil
	})
}

// SetRegion updates the region (useful after auto-detection)
func (c *OVHClient) SetRegion(region string) {
	c.region = region
}

// AutoDetectRegion fetches the cluster info and sets the region automatically
// Returns the detected region or an error
func (c *OVHClient) AutoDetectRegion(ctx context.Context) (string, error) {
	cluster, err := c.GetCluster(ctx)
	if err != nil {
		return "", fmt.Errorf("auto-detecting region: %w", err)
	}
	c.region = cluster.Region
	return cluster.Region, nil
}

// DeleteNode deletes a specific node from the cluster
// This is different from scaling down - it removes the exact node specified
// API: DELETE /cloud/project/{serviceName}/kube/{kubeId}/node/{nodeId}
func (c *OVHClient) DeleteNode(ctx context.Context, nodeID string) error {
	path := fmt.Sprintf("%s/node/%s", c.basePath(), nodeID)
	return retryableVoidCall(ctx, c.retryConfig, "DeleteNode", func() error {
		if err := c.client.DeleteWithContext(ctx, path, nil); err != nil {
			return fmt.Errorf("deleting node %s: %w", nodeID, err)
		}
		return nil
	})
}
