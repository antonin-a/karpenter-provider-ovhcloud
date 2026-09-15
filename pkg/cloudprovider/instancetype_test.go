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
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/ovh/karpenter-provider-ovhcloud/pkg/apis/v1alpha1"
	ovhclient "github.com/ovh/karpenter-provider-ovhcloud/pkg/client"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reference values observed on a live MKS node (testKarpenter, EU-WEST-PAR,
// k8s 1.35.2, flavor b3-8: 2 vCPU / 8192 MiB / 50 GB):
//
//	capacity:    cpu=2      memory=7938364Ki (~7752Mi)  ephemeral=50620216Ki
//	allocatable: cpu=1840m  memory=6054204Ki (~5912Mi)  ephemeral=~28.4Gi
func TestAllocatableMatchesLiveB38Node(t *testing.T) {
	// RAM is in GB per the cloud.kube.Flavor schema; disk merged from Nova
	flavor := ovhclient.Flavor{Name: "b3-8", Category: "b", VCPUs: 2, RAM: 8, Disk: 50}
	it := buildInstanceType(context.Background(), flavor, "EU-WEST-PAR", nil)

	alloc := it.Allocatable()

	// CPU: 2000m - (150m + 5m*2) = 1840m, exact match with the live node
	if got := alloc.Cpu().MilliValue(); got != 1840 {
		t.Errorf("allocatable cpu = %dm, want 1840m", got)
	}

	// Memory: modeled allocatable must never exceed the live node's 6054204Ki
	// (Karpenter overestimating allocatable causes provisioning oscillation),
	// and should stay within ~10%% of it (too conservative wastes capacity).
	liveAllocatableKi := int64(6054204)
	got := alloc.Memory().Value() / 1024 // Ki
	if got > liveAllocatableKi {
		t.Errorf("allocatable memory = %dKi exceeds live node %dKi: scheduler will overpack", got, liveAllocatableKi)
	}
	if got < liveAllocatableKi*90/100 {
		t.Errorf("allocatable memory = %dKi is more than 10%% below live node %dKi: too conservative", got, liveAllocatableKi)
	}

	// Capacity memory must also not exceed what the node reports
	liveCapacityKi := int64(7938364)
	if got := it.Capacity.Memory().Value() / 1024; got > liveCapacityKi {
		t.Errorf("capacity memory = %dKi exceeds live node %dKi", got, liveCapacityKi)
	}
}

func TestOverheadScalesWithCores(t *testing.T) {
	oh := mksOverhead(32, 400) // b3-128
	if got := oh.KubeReserved.Cpu().MilliValue(); got != 150+5*32 {
		t.Errorf("kube-reserved cpu for 32 cores = %dm, want %dm", got, 150+5*32)
	}
	if oh.KubeReserved.StorageEphemeral().Value() <= 0 {
		t.Error("ephemeral storage overhead should be positive")
	}
}

func TestPoolNameForClaim(t *testing.T) {
	c := &CloudProvider{}
	short := c.poolNameForClaim(&v1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "general-x7k2p"}})
	if short != "karpenter-general-x7k2p" {
		t.Errorf("pool name = %q, want karpenter-general-x7k2p", short)
	}
	long := c.poolNameForClaim(&v1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "a-very-long-nodepool-name-that-overflows-the-limit-x7k2p"}})
	if len(long) > MaxPoolNameLength {
		t.Errorf("pool name %q exceeds MaxPoolNameLength=%d", long, MaxPoolNameLength)
	}
	// Determinism: same claim always maps to the same pool name
	long2 := c.poolNameForClaim(&v1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "a-very-long-nodepool-name-that-overflows-the-limit-x7k2p"}})
	if long != long2 {
		t.Errorf("pool name not deterministic: %q != %q", long, long2)
	}
}

func TestWellKnownLabels(t *testing.T) {
	labels := wellKnownLabelsFor("b3-16", "eu-west-par-a")
	for _, key := range []string{
		corev1.LabelInstanceTypeStable,
		corev1.LabelTopologyZone,
		corev1.LabelArchStable,
		corev1.LabelOSStable,
		v1.CapacityTypeLabelKey,
	} {
		if labels[key] == "" {
			t.Errorf("well-known label %s missing: NodePool requirements on it would mark every NodeClaim RequirementsDrifted", key)
		}
	}
}

func TestSelectFlavorPicksCheapest(t *testing.T) {
	// b3-16 sorts lexicographically before b3-8: Values[0] would pick the
	// oversized flavor and consolidation would replace it one cycle later
	mkIT := func(name string, price float64) *cloudprovider.InstanceType {
		return &cloudprovider.InstanceType{
			Name: name,
			Offerings: cloudprovider.Offerings{
				&cloudprovider.Offering{Price: price, Available: true},
			},
		}
	}
	c := &CloudProvider{instanceTypes: []*cloudprovider.InstanceType{
		mkIT("b3-16", 0.10), mkIT("b3-32", 0.20), mkIT("b3-8", 0.05),
	}}
	claim := &v1.NodeClaim{Spec: v1.NodeClaimSpec{Requirements: []v1.NodeSelectorRequirementWithMinValues{{
		NodeSelectorRequirement: corev1.NodeSelectorRequirement{
			Key: corev1.LabelInstanceTypeStable, Operator: corev1.NodeSelectorOpIn,
			Values: []string{"b3-16", "b3-32", "b3-8"},
		},
	}}}}
	got, err := c.selectFlavor(claim)
	if err != nil {
		t.Fatal(err)
	}
	if got != "b3-8" {
		t.Errorf("selectFlavor = %q, want b3-8 (cheapest)", got)
	}
}

func TestMonthlyBillingOnlyForGen2(t *testing.T) {
	// The MKS API rejects monthlyBilled on gen3+ flavors with 400
	// "monthly billing not supported yet" (verified live 2026-09-15):
	// gen3+ uses Savings Plans at the billing level instead
	for name, gen2 := range map[string]bool{
		"b2-7": true, "c2-15": true, "r2-30": true, "d2-8": true,
		"b3-8": false, "c3-16": false, "r3-512": false, "t1-45": false, "a100-180": false, "l40s-90": false,
	} {
		if got := isGen2Flavor(name); got != gen2 {
			t.Errorf("isGen2Flavor(%s) = %v, want %v", name, got, gen2)
		}
	}
	monthlyClass := &v1alpha1.OVHNodeClass{Spec: v1alpha1.OVHNodeClassSpec{MonthlyBilled: true}}
	if effectiveMonthlyBilled(monthlyClass, "b3-8") {
		t.Error("monthlyBilled must be ignored for gen3 flavors (API rejects it; drift against raw spec would loop)")
	}
	if !effectiveMonthlyBilled(monthlyClass, "b2-7") {
		t.Error("monthlyBilled must apply to gen2 flavors")
	}
}
