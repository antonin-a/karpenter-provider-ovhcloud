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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretReference contains the reference to a Kubernetes Secret
type SecretReference struct {
	// Name is the name of the Secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Namespace is the namespace of the Secret
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}

// OVHNodeClassSpec defines the desired state of OVHNodeClass
type OVHNodeClassSpec struct {
	// ServiceName is the OVHcloud Public Cloud project ID (required)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-f0-9]{32}$`
	ServiceName string `json:"serviceName"`

	// KubeID is the OVHcloud MKS cluster ID (required)
	// +kubebuilder:validation:Required
	KubeID string `json:"kubeId"`

	// Region is the OVHcloud region (e.g., GRA7, SBG5, EU-WEST-PAR)
	// +kubebuilder:validation:Required
	Region string `json:"region"`

	// CredentialsSecretRef points to the Secret containing OVH API credentials
	// Expected keys: applicationKey, applicationSecret, consumerKey
	// +kubebuilder:validation:Required
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef"`

	// MonthlyBilled enables monthly billing for nodes (cheaper for long-running nodes)
	// +kubebuilder:default:=false
	// +optional
	MonthlyBilled bool `json:"monthlyBilled,omitempty"`

	// AntiAffinity enables anti-affinity for the node pool (spreads nodes across hypervisors)
	// Note: Maximum 5 nodes per pool when anti-affinity is enabled
	// +kubebuilder:default:=false
	// +optional
	AntiAffinity bool `json:"antiAffinity,omitempty"`

	// Tags are key-value pairs applied to the node pools
	// +optional
	Tags map[string]string `json:"tags,omitempty"`
}

// OVHNodeClass is the Schema for the OVHNodeClass API
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=ovhnodeclasses,scope=Cluster,categories=karpenter,shortName={ovhnc,ovhncs}
// +kubebuilder:subresource:status
type OVHNodeClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OVHNodeClassSpec `json:"spec,omitempty"`
	// +kubebuilder:default:={conditions: {{type: "Ready", status: "True", reason:"Ready", lastTransitionTime: "2024-01-01T01:01:01Z", message: ""}}}
	Status OVHNodeClassStatus `json:"status,omitempty"`
}

// OVHNodeClassList contains a list of OVHNodeClass
// +kubebuilder:object:root=true
type OVHNodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OVHNodeClass `json:"items"`
}
