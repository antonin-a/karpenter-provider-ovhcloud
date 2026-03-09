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

// Package nodelabels provides a controller that ensures Karpenter-managed OVH
// nodes carry the correct karpenter.sh/* labels.
//
// Background
// ----------
// OVH MKS silently drops arbitrary labels that are set in a node-pool's
// template metadata when it bootstraps the node.  Standard kubelet labels
// (kubernetes.io/arch, node.kubernetes.io/instance-type, …) are applied
// because kubelet itself writes them, but labels added by Karpenter
// (karpenter.sh/nodepool, karpenter.sh/capacity-type, …) never land on the
// node object.
//
// Karpenter core's overlay mechanism is supposed to fix this by patching the
// node after it registers, but there is a race window: the disruption
// controller can evaluate RequirementsDrifted before the overlay patch
// completes and immediately delete the node (observed: deletion 1-2 s after
// the node joined).
//
// This controller closes that race by running a dedicated reconcile loop on
// every Node object.  Whenever a Karpenter-managed node is missing labels
// that its matching NodeClaim carries, it patches them in.  The operation is
// fully idempotent; it is a no-op on non-Karpenter nodes and on nodes that
// already have all the right labels.
package nodelabels

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	utilscontroller "sigs.k8s.io/karpenter/pkg/utils/controller"
)

const (
	// ovhProviderPrefix is the providerID prefix OVH MKS sets on nodes.
	ovhProviderPrefix = "openstack:///"
)

// labelsToSync lists the label key prefixes that must be synced from a
// NodeClaim onto its node.  These are labels Karpenter relies on for
// scheduling and drift detection but that OVH MKS does not propagate.
var labelsToSync = []string{
	"karpenter.sh/",
	// topology.kubernetes.io/zone: OVH MKS sets this to the OpenStack
	// zone name ("nova") rather than the OVH availability-zone format
	// ("gra9-a") that Karpenter uses in NodePool requirements.
	"topology.kubernetes.io/zone",
}

// Controller ensures that every OVH node managed by Karpenter has the labels
// from its NodeClaim applied, compensating for OVH MKS not propagating pool
// template metadata labels.
type Controller struct {
	kubeClient client.Client
}

// NewController creates a new node-labels controller.
func NewController(kubeClient client.Client) *Controller {
	return &Controller{kubeClient: kubeClient}
}

func (c *Controller) Name() string {
	return "ovh.nodelabels"
}

// Reconcile is called for every Node event.  It is a fast no-op for the
// common case (node already has correct labels or is not managed by
// Karpenter).
func (c *Controller) Reconcile(ctx context.Context, node *corev1.Node) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("node", node.Name)

	// Only handle OVH/OpenStack nodes.
	if !strings.HasPrefix(node.Spec.ProviderID, ovhProviderPrefix) {
		return reconcile.Result{}, nil
	}

	// Find the NodeClaim whose providerID matches this node.
	claim, err := c.findNodeClaim(ctx, node.Spec.ProviderID)
	if err != nil {
		return reconcile.Result{}, err
	}
	if claim == nil {
		// No NodeClaim yet — the node may have registered before the
		// provider stamped the providerID.  The NodeClaim watch below
		// will re-enqueue this node once the claim appears.
		return reconcile.Result{}, nil
	}

	// Compute the set of labels that need to be added/corrected on the node.
	missing := missingLabels(node.Labels, claim.Labels)
	if len(missing) == 0 {
		return reconcile.Result{}, nil
	}

	// Patch only the labels that need to change; leave everything else alone.
	base := node.DeepCopy()
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	for k, v := range missing {
		node.Labels[k] = v
	}

	logger.Info("Patching missing Karpenter labels onto node",
		"count", len(missing), "labels", missing)

	if err := c.kubeClient.Patch(ctx, node, client.MergeFrom(base)); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// Register wires the controller into the manager.
// It watches both Node and NodeClaim objects so that:
//   - A Node event fires when the node first registers.
//   - A NodeClaim event fires when the provider sets the providerID; this
//     re-enqueues the node in case it registered before the NodeClaim was
//     updated.
func (c *Controller) Register(ctx context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		Named(c.Name()).
		For(&corev1.Node{}).
		// Also watch NodeClaims: when a NodeClaim gets its providerID set,
		// find the matching Node and enqueue it.
		Watches(
			&karpenterv1.NodeClaim{},
			handler.EnqueueRequestsFromMapFunc(c.nodeClaimToNode),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: utilscontroller.LinearScaleReconciles(
				utilscontroller.CPUCount(ctx), 10, 100),
		}).
		Complete(reconcile.AsReconciler(m.GetClient(), c))
}

// ── helpers ───────────────────────────────────────────────────────────────────

// findNodeClaim returns the NodeClaim whose Status.ProviderID matches the
// given providerID, or nil if none exists yet.
func (c *Controller) findNodeClaim(ctx context.Context, providerID string) (*karpenterv1.NodeClaim, error) {
	if providerID == "" {
		return nil, nil
	}
	claimList := &karpenterv1.NodeClaimList{}
	if err := c.kubeClient.List(ctx, claimList); err != nil {
		return nil, err
	}
	for i := range claimList.Items {
		if claimList.Items[i].Status.ProviderID == providerID {
			return &claimList.Items[i], nil
		}
	}
	return nil, nil
}

// nodeClaimToNode maps a NodeClaim event to the corresponding Node so it can
// be re-enqueued.
func (c *Controller) nodeClaimToNode(ctx context.Context, obj client.Object) []reconcile.Request {
	claim, ok := obj.(*karpenterv1.NodeClaim)
	if !ok || claim.Status.ProviderID == "" {
		return nil
	}
	// Find the node whose spec.providerID matches.
	nodeList := &corev1.NodeList{}
	if err := c.kubeClient.List(ctx, nodeList); err != nil {
		return nil
	}
	for _, node := range nodeList.Items {
		if node.Spec.ProviderID == claim.Status.ProviderID {
			return []reconcile.Request{{
				NamespacedName: client.ObjectKeyFromObject(&node),
			}}
		}
	}
	return nil
}

// missingLabels returns the subset of claimLabels that should be synced to the
// node but are either absent or hold the wrong value.
// Only label keys covered by labelsToSync are considered.
func missingLabels(nodeLabels, claimLabels map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range claimLabels {
		if !shouldSync(k) {
			continue
		}
		if existing, ok := nodeLabels[k]; !ok || existing != v {
			result[k] = v
		}
	}
	return result
}

// shouldSync reports whether a label key falls within the set of keys this
// controller is responsible for syncing.
func shouldSync(key string) bool {
	for _, prefix := range labelsToSync {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
