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

package main

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/ovh/karpenter-provider-ovhcloud/pkg/client"
	ovhcloud "github.com/ovh/karpenter-provider-ovhcloud/pkg/cloudprovider"
	"github.com/ovh/karpenter-provider-ovhcloud/pkg/controllers/nodeclass"
	"sigs.k8s.io/karpenter/pkg/cloudprovider/overlay"
	"sigs.k8s.io/karpenter/pkg/controllers"
	"sigs.k8s.io/karpenter/pkg/controllers/state"
	"sigs.k8s.io/karpenter/pkg/operator"
)

// Build version - updated to invalidate Docker cache
const buildVersion = "0.1.0-20260206"

const (
	// ClusterNameAnnotation is the annotation on nodes that contains the MKS cluster ID
	ClusterNameAnnotation = "cluster.x-k8s.io/cluster-name"
)

func main() {
	ctx, op := operator.NewOperator()
	logger := log.FromContext(ctx)

	// Load OVH credentials from environment variables
	creds := &client.Credentials{
		Endpoint:          getEnvOrDefault("OVH_ENDPOINT", "ovh-eu"),
		ApplicationKey:    os.Getenv("OVH_APPLICATION_KEY"),
		ApplicationSecret: os.Getenv("OVH_APPLICATION_SECRET"),
		ConsumerKey:       os.Getenv("OVH_CONSUMER_KEY"),
	}

	serviceName := os.Getenv("OVH_SERVICE_NAME")
	kubeID := os.Getenv("OVH_KUBE_ID") // Optional - will be auto-detected if not set
	region := os.Getenv("OVH_REGION")  // Optional - will be auto-detected if not set

	if creds.ApplicationKey == "" || creds.ApplicationSecret == "" || creds.ConsumerKey == "" {
		logger.Error(nil, "OVH credentials not set. Please set OVH_APPLICATION_KEY, OVH_APPLICATION_SECRET, and OVH_CONSUMER_KEY")
		os.Exit(1)
	}

	if serviceName == "" {
		logger.Error(nil, "OVH_SERVICE_NAME not set. This is the OVHcloud project ID and must be provided")
		os.Exit(1)
	}

	// Auto-detect kubeID from node annotations if not explicitly set
	if kubeID == "" {
		detectedKubeID, err := detectKubeIDFromNodes(ctx, op.GetClient())
		if err != nil {
			logger.Error(err, "failed to auto-detect kubeID from cluster nodes. Please set OVH_KUBE_ID manually")
			os.Exit(1)
		}
		kubeID = detectedKubeID
		logger.Info("Auto-detected kubeID from cluster nodes", "kubeID", kubeID)
	} else {
		logger.Info("Using configured kubeID", "kubeID", kubeID)
	}

	// Create OVH API client (region can be empty, will be auto-detected)
	ovhClient, err := client.NewOVHClient(creds, serviceName, kubeID, region)
	if err != nil {
		logger.Error(err, "failed creating OVH client")
		os.Exit(1)
	}

	// Auto-detect region from MKS cluster if not explicitly set
	if region == "" {
		detectedRegion, err := ovhClient.AutoDetectRegion(ctx)
		if err != nil {
			logger.Error(err, "failed to auto-detect region from MKS cluster")
			os.Exit(1)
		}
		logger.Info("Auto-detected region from MKS cluster", "region", detectedRegion)
	} else {
		logger.Info("Using configured region", "region", region)
	}

	// Construct instance types from OVH flavors
	instanceTypes, err := ovhcloud.ConstructInstanceTypes(ctx, ovhClient)
	if err != nil {
		logger.Error(err, "failed constructing instance types")
		os.Exit(1)
	}

	logger.Info("Loaded instance types", "count", len(instanceTypes))

	// Create cloud provider
	overlayUndecoratedCloudProvider := ovhcloud.NewCloudProvider(ctx, op.GetClient(), ovhClient, instanceTypes)
	cloudProvider := overlay.Decorate(overlayUndecoratedCloudProvider, op.GetClient(), op.InstanceTypeStore)
	clusterState := state.NewCluster(op.Clock, op.GetClient(), cloudProvider)

	// Create OVHNodeClass controller
	ovhNodeClassController := nodeclass.NewController(op.GetClient())

	// Get base controllers and append OVHNodeClass controller
	baseControllers := controllers.NewControllers(
		ctx,
		op.Manager,
		op.Clock,
		op.GetClient(),
		op.EventRecorder,
		cloudProvider,
		overlayUndecoratedCloudProvider,
		clusterState,
		op.InstanceTypeStore,
	)

	op.
		WithControllers(ctx, append(baseControllers, ovhNodeClassController)...).
		Start(ctx)
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// detectKubeIDFromNodes attempts to detect the MKS cluster ID from node annotations
func detectKubeIDFromNodes(ctx context.Context, kubeClient ctrlclient.Client) (string, error) {
	var nodes corev1.NodeList
	if err := kubeClient.List(ctx, &nodes); err != nil {
		return "", err
	}

	if len(nodes.Items) == 0 {
		return "", fmt.Errorf("no nodes found in cluster")
	}

	// Look for the cluster ID annotation on any node
	for _, node := range nodes.Items {
		if clusterName, ok := node.Annotations[ClusterNameAnnotation]; ok && clusterName != "" {
			return clusterName, nil
		}
	}

	return "", fmt.Errorf("no node has the %s annotation", ClusterNameAnnotation)
}
