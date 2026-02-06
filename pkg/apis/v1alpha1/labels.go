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

package v1alpha1

import (
	"github.com/ovh/karpenter-provider-ovhcloud/pkg/apis"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	// Instance labels
	LabelInstanceCategory = apis.Group + "/instance-category" // b2, c2, r2, t2, etc.
	LabelInstanceCPU      = apis.Group + "/instance-cpu"
	LabelInstanceMemory   = apis.Group + "/instance-memory"
	LabelInstanceFamily   = apis.Group + "/instance-family"
	LabelInstanceSize     = apis.Group + "/instance-size"

	// Pool tracking
	LabelPoolID   = apis.Group + "/pool-id"
	LabelPoolName = apis.Group + "/pool-name"

	// Annotations for tracking NodeClaim to Node mapping
	AnnotationOVHPoolID   = apis.Group + "/pool-id"
	AnnotationOVHNodeID   = apis.Group + "/node-id"
	AnnotationOVHNodeName = apis.Group + "/node-name"
)

func init() {
	v1.RestrictedLabelDomains = v1.RestrictedLabelDomains.Insert(apis.Group)
	v1.WellKnownLabels = v1.WellKnownLabels.Insert(
		LabelInstanceCategory,
		LabelInstanceCPU,
		LabelInstanceMemory,
		LabelInstanceFamily,
		LabelInstanceSize,
	)
}
