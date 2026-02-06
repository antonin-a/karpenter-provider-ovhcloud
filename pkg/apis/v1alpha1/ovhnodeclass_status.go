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
	"github.com/awslabs/operatorpkg/status"
)

// OVHNodeClassStatus contains the resolved state of the OVHNodeClass
type OVHNodeClassStatus struct {
	// Conditions contains signals for health and readiness
	Conditions []status.Condition `json:"conditions,omitempty"`
	// DiscoveredFlavors shows available flavors in the region
	// +optional
	DiscoveredFlavors []string `json:"discoveredFlavors,omitempty"`
}

func (in *OVHNodeClass) StatusConditions() status.ConditionSet {
	return status.NewReadyConditions().For(in)
}

func (in *OVHNodeClass) GetConditions() []status.Condition {
	return in.Status.Conditions
}

func (in *OVHNodeClass) SetConditions(conditions []status.Condition) {
	in.Status.Conditions = conditions
}
