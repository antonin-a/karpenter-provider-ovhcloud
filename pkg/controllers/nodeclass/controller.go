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

package nodeclass

import (
	"context"
	"fmt"

	"github.com/awslabs/operatorpkg/status"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/ovh/karpenter-provider-ovhcloud/pkg/apis/v1alpha1"
	utilscontroller "sigs.k8s.io/karpenter/pkg/utils/controller"
)

// Controller reconciles OVHNodeClass resources
type Controller struct {
	kubeClient client.Client
}

// NewController creates a new OVHNodeClass controller
func NewController(kubeClient client.Client) *Controller {
	return &Controller{
		kubeClient: kubeClient,
	}
}

func (c *Controller) Name() string {
	return "ovhnodeclass.readiness"
}

func (c *Controller) Reconcile(ctx context.Context, nodeClass *v1alpha1.OVHNodeClass) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("OVHNodeClass", nodeClass.Name)
	stored := nodeClass.DeepCopy()

	// Validate the OVHNodeClass and set Ready condition
	if err := c.validateNodeClass(nodeClass); err != nil {
		nodeClass.StatusConditions().SetFalse(status.ConditionReady, "ValidationFailed", err.Error())
		logger.Error(err, "OVHNodeClass validation failed")
	} else {
		nodeClass.StatusConditions().SetTrue(status.ConditionReady)
		logger.Info("OVHNodeClass is ready")
	}

	// Only patch if status changed
	if !equality.Semantic.DeepEqual(stored.Status, nodeClass.Status) {
		if err := c.kubeClient.Status().Patch(ctx, nodeClass, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
			if errors.IsConflict(err) {
				return reconcile.Result{Requeue: true}, nil
			}
			if errors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// validateNodeClass validates the OVHNodeClass configuration
func (c *Controller) validateNodeClass(nodeClass *v1alpha1.OVHNodeClass) error {
	if nodeClass.Spec.ServiceName == "" {
		return fmt.Errorf("serviceName is required")
	}
	if nodeClass.Spec.KubeID == "" {
		return fmt.Errorf("kubeId is required")
	}
	if nodeClass.Spec.Region == "" {
		return fmt.Errorf("region is required")
	}
	return nil
}

func (c *Controller) Register(ctx context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		Named(c.Name()).
		For(&v1alpha1.OVHNodeClass{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: utilscontroller.LinearScaleReconciles(utilscontroller.CPUCount(ctx), 10, 100),
		}).
		Complete(reconcile.AsReconciler(m.GetClient(), c))
}
