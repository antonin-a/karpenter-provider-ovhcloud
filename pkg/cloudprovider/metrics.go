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
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	metricsSubsystem = "karpenter_ovhcloud"
)

var (
	// Node provisioning metrics
	nodeProvisioningTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: metricsSubsystem,
			Name:      "node_provisioning_total",
			Help:      "Total number of node provisioning attempts",
		},
		[]string{"flavor", "zone", "status"},
	)

	nodeProvisioningDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: metricsSubsystem,
			Name:      "node_provisioning_duration_seconds",
			Help:      "Time taken to provision a node (including MKS bootstrap)",
			Buckets:   prometheus.ExponentialBuckets(10, 2, 8), // 10s, 20s, 40s, ... 1280s
		},
		[]string{"flavor", "zone"},
	)

	// Node deletion metrics
	nodeDeletionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: metricsSubsystem,
			Name:      "node_deletion_total",
			Help:      "Total number of node deletion attempts",
		},
		[]string{"status"},
	)

	nodeDeletionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: metricsSubsystem,
			Name:      "node_deletion_duration_seconds",
			Help:      "Time taken to delete a node",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 8), // 1s, 2s, 4s, ... 128s
		},
		[]string{},
	)

	// Pool metrics
	poolOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: metricsSubsystem,
			Name:      "pool_operations_total",
			Help:      "Total number of pool operations",
		},
		[]string{"operation", "status"},
	)

	poolsActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: metricsSubsystem,
			Name:      "pools_active",
			Help:      "Number of active Karpenter-managed pools",
		},
	)

	// API metrics
	apiCallsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: metricsSubsystem,
			Name:      "api_calls_total",
			Help:      "Total number of OVH API calls",
		},
		[]string{"method", "endpoint", "status"},
	)

	apiCallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: metricsSubsystem,
			Name:      "api_call_duration_seconds",
			Help:      "Duration of OVH API calls",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 8), // 0.1s, 0.2s, ... 12.8s
		},
		[]string{"method", "endpoint"},
	)

	apiRetriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: metricsSubsystem,
			Name:      "api_retries_total",
			Help:      "Total number of API call retries",
		},
		[]string{"method", "endpoint"},
	)

	// Instance type metrics
	instanceTypesAvailable = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: metricsSubsystem,
			Name:      "instance_types_available",
			Help:      "Number of available instance types",
		},
	)

	// Pricing cache metrics
	pricingCacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Subsystem: metricsSubsystem,
			Name:      "pricing_cache_hits_total",
			Help:      "Total number of pricing cache hits",
		},
	)

	pricingCacheMisses = prometheus.NewCounter(
		prometheus.CounterOpts{
			Subsystem: metricsSubsystem,
			Name:      "pricing_cache_misses_total",
			Help:      "Total number of pricing cache misses",
		},
	)

	pricingCacheRefreshes = prometheus.NewCounter(
		prometheus.CounterOpts{
			Subsystem: metricsSubsystem,
			Name:      "pricing_cache_refreshes_total",
			Help:      "Total number of pricing cache refreshes",
		},
	)

	// Drift metrics
	driftDetectionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: metricsSubsystem,
			Name:      "drift_detection_total",
			Help:      "Total number of drift detections",
		},
		[]string{"reason"},
	)
)

func init() {
	// Register all metrics with controller-runtime's registry
	metrics.Registry.MustRegister(
		nodeProvisioningTotal,
		nodeProvisioningDuration,
		nodeDeletionTotal,
		nodeDeletionDuration,
		poolOperationsTotal,
		poolsActive,
		apiCallsTotal,
		apiCallDuration,
		apiRetriesTotal,
		instanceTypesAvailable,
		pricingCacheHits,
		pricingCacheMisses,
		pricingCacheRefreshes,
		driftDetectionTotal,
	)
}

// RecordNodeProvisioning records a node provisioning attempt
func RecordNodeProvisioning(flavor, zone, status string) {
	nodeProvisioningTotal.WithLabelValues(flavor, zone, status).Inc()
}

// RecordNodeProvisioningDuration records the duration of a node provisioning
func RecordNodeProvisioningDuration(flavor, zone string, durationSeconds float64) {
	nodeProvisioningDuration.WithLabelValues(flavor, zone).Observe(durationSeconds)
}

// RecordNodeDeletion records a node deletion attempt
func RecordNodeDeletion(status string) {
	nodeDeletionTotal.WithLabelValues(status).Inc()
}

// RecordNodeDeletionDuration records the duration of a node deletion
func RecordNodeDeletionDuration(durationSeconds float64) {
	nodeDeletionDuration.WithLabelValues().Observe(durationSeconds)
}

// RecordPoolOperation records a pool operation
func RecordPoolOperation(operation, status string) {
	poolOperationsTotal.WithLabelValues(operation, status).Inc()
}

// SetPoolsActive sets the number of active pools
func SetPoolsActive(count int) {
	poolsActive.Set(float64(count))
}

// RecordAPICall records an OVH API call
func RecordAPICall(method, endpoint, status string) {
	apiCallsTotal.WithLabelValues(method, endpoint, status).Inc()
}

// RecordAPICallDuration records the duration of an OVH API call
func RecordAPICallDuration(method, endpoint string, durationSeconds float64) {
	apiCallDuration.WithLabelValues(method, endpoint).Observe(durationSeconds)
}

// RecordAPIRetry records an API retry
func RecordAPIRetry(method, endpoint string) {
	apiRetriesTotal.WithLabelValues(method, endpoint).Inc()
}

// SetInstanceTypesAvailable sets the number of available instance types
func SetInstanceTypesAvailable(count int) {
	instanceTypesAvailable.Set(float64(count))
}

// RecordPricingCacheHit records a pricing cache hit
func RecordPricingCacheHit() {
	pricingCacheHits.Inc()
}

// RecordPricingCacheMiss records a pricing cache miss
func RecordPricingCacheMiss() {
	pricingCacheMisses.Inc()
}

// RecordPricingCacheRefresh records a pricing cache refresh
func RecordPricingCacheRefresh() {
	pricingCacheRefreshes.Inc()
}

// RecordDriftDetection records a drift detection
func RecordDriftDetection(reason string) {
	driftDetectionTotal.WithLabelValues(reason).Inc()
}
