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

// Package garbagecollection reconciles OVH MKS node pools against NodeClaims.
//
// Karpenter core already garbage-collects cloud instances that have no
// NodeClaim, through CloudProvider.List() and Delete(). That path only sees
// pools that have at least one node. This controller covers the remaining
// leak: karpenter-* pools stuck without a matching NodeClaim (interrupted
// Create, claim deleted while the pool was still INSTALLING, ...). MKS pools
// live behind an external REST API, so no Kubernetes owner reference can
// clean them up for free.
package garbagecollection

import (
	"context"
	"strings"
	"time"

	"github.com/awslabs/operatorpkg/reconciler"
	"github.com/awslabs/operatorpkg/singleton"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/ovh/karpenter-provider-ovhcloud/pkg/apis/v1alpha1"
	ovhclient "github.com/ovh/karpenter-provider-ovhcloud/pkg/client"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	// PoolNamePrefix must match the cloudprovider's pool naming
	PoolNamePrefix = "karpenter-"
	// gracePeriod protects freshly created pools from being collected while
	// their Create() call is still in flight (pool creation is ~2 min async)
	gracePeriod = 10 * time.Minute
	// interval between reconciliations
	interval = 2 * time.Minute
)

// Controller deletes orphan karpenter-* pools with no matching NodeClaim
type Controller struct {
	kubeClient client.Client
	ovhClient  *ovhclient.OVHClient
}

func NewController(kubeClient client.Client, ovhClient *ovhclient.OVHClient) *Controller {
	return &Controller{kubeClient: kubeClient, ovhClient: ovhClient}
}

func (c *Controller) Name() string {
	return "pool.garbagecollection"
}

func (c *Controller) Reconcile(ctx context.Context) (reconciler.Result, error) {
	logger := log.FromContext(ctx).WithName(c.Name())

	pools, err := c.ovhClient.ListNodePools(ctx)
	if err != nil {
		return reconciler.Result{RequeueAfter: interval}, err
	}

	nodeClaims := &v1.NodeClaimList{}
	if err := c.kubeClient.List(ctx, nodeClaims); err != nil {
		return reconciler.Result{RequeueAfter: interval}, err
	}

	// Index the pools referenced by live NodeClaims: by pool-id annotation,
	// and by the deterministic pool name for claims not yet annotated
	referenced := map[string]bool{}
	for i := range nodeClaims.Items {
		nc := &nodeClaims.Items[i]
		if id := nc.Annotations[v1alpha1.AnnotationOVHPoolID]; id != "" {
			referenced[id] = true
		}
		referenced[PoolNamePrefix+strings.ToLower(nc.Name)] = true
	}

	for i := range pools {
		pool := &pools[i]
		if !strings.HasPrefix(pool.Name, PoolNamePrefix) {
			continue
		}
		if referenced[pool.ID] || referenced[pool.Name] {
			continue
		}
		if pool.Status == "DELETING" {
			continue
		}
		if created, err := time.Parse(time.RFC3339, pool.CreatedAt); err == nil && time.Since(created) < gracePeriod {
			continue
		}
		logger.Info("Deleting orphan pool", "pool", pool.Name, "poolID", pool.ID, "status", pool.Status, "currentNodes", pool.CurrentNodes)
		if err := c.ovhClient.DeleteNodePool(ctx, pool.ID); err != nil {
			logger.Error(err, "failed deleting orphan pool", "pool", pool.Name)
		}
	}

	return reconciler.Result{RequeueAfter: interval}, nil
}

func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		Named(c.Name()).
		WatchesRawSource(singleton.Source()).
		Complete(singleton.AsReconciler(c))
}
