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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// PricingCatalog represents the OVH public cloud pricing catalog
type PricingCatalog struct {
	CatalogID int             `json:"catalogId"`
	Locale    PricingLocale   `json:"locale"`
	Plans     []PricingPlan   `json:"plans"`
	Addons    []PricingAddon  `json:"addons"`
}

// PricingLocale contains locale information
type PricingLocale struct {
	CurrencyCode string `json:"currencyCode"`
	Subsidiary   string `json:"subsidiary"`
}

// PricingPlan represents a plan in the catalog
type PricingPlan struct {
	PlanCode    string          `json:"planCode"`
	InvoiceName string          `json:"invoiceName"`
	Pricings    []PricingDetail `json:"pricings"`
}

// PricingAddon represents an addon (like instance flavors) in the catalog
type PricingAddon struct {
	PlanCode    string          `json:"planCode"`
	InvoiceName string          `json:"invoiceName"`
	Pricings    []PricingDetail `json:"pricings"`
}

// PricingDetail contains pricing information
type PricingDetail struct {
	Capacities     []string `json:"capacities"`
	Description    string   `json:"description"`
	Duration       string   `json:"duration"`
	Interval       int      `json:"interval"`
	MinimumRepeat  int      `json:"minimumRepeat"`
	MaximumRepeat  *int     `json:"maximumRepeat"`
	Price          int64    `json:"price"`          // Price in micro-units (e.g., 1000000 = 1.00 EUR)
	PriceInUcents  int64    `json:"priceInUcents"`  // Price in micro-cents
	Tax            int64    `json:"tax"`
}

// PricingClient handles OVH pricing API calls
type PricingClient struct {
	httpClient *http.Client
	baseURL    string
	subsidiary string

	// Cache
	mu           sync.RWMutex
	catalog      *PricingCatalog
	lastRefresh  time.Time
	cacheTTL     time.Duration
	flavorPrices map[string]float64 // flavor name -> hourly price in EUR
}

// NewPricingClient creates a new pricing client
func NewPricingClient(subsidiary string) *PricingClient {
	if subsidiary == "" {
		subsidiary = "FR"
	}
	return &PricingClient{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      "https://api.ovh.com/1.0/order/catalog/public/cloud",
		subsidiary:   subsidiary,
		cacheTTL:     6 * time.Hour, // Refresh prices every 6 hours
		flavorPrices: make(map[string]float64),
	}
}

// GetFlavorPrice returns the hourly price for a flavor in EUR
// Tries multiple lookup strategies: exact match, region-prefixed, catalog patterns
func (p *PricingClient) GetFlavorPrice(ctx context.Context, flavorName string, region string) (float64, error) {
	// Refresh cache if needed
	if err := p.refreshCacheIfNeeded(ctx); err != nil {
		// On error, return estimated price
		return p.estimatePrice(flavorName), nil
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	regionLower := strings.ToLower(region)

	// Strategy 1: Exact match by flavor name
	if price, ok := p.flavorPrices[flavorName]; ok {
		return price, nil
	}

	// Strategy 2: Region prefix (e.g., "gra7.b3-8")
	regionKey := fmt.Sprintf("%s.%s", regionLower, flavorName)
	if price, ok := p.flavorPrices[regionKey]; ok {
		return price, nil
	}

	// Strategy 3: Instance catalog format (e.g., "instance-b3-8.gra7.hour.consumption")
	catalogKey := fmt.Sprintf("instance-%s.%s.hour.consumption", flavorName, regionLower)
	if price, ok := p.flavorPrices[catalogKey]; ok {
		return price, nil
	}

	// Strategy 4: Try without region-specific suffix (some flavors have global pricing)
	// e.g., "instance-b3-8.hour.consumption"
	globalKey := fmt.Sprintf("instance-%s.hour.consumption", flavorName)
	if price, ok := p.flavorPrices[globalKey]; ok {
		return price, nil
	}

	// Fall back to estimation
	return p.estimatePrice(flavorName), nil
}

// refreshCacheIfNeeded refreshes the pricing cache if it's stale
func (p *PricingClient) refreshCacheIfNeeded(ctx context.Context) error {
	p.mu.RLock()
	needsRefresh := p.catalog == nil || time.Since(p.lastRefresh) > p.cacheTTL
	p.mu.RUnlock()

	if !needsRefresh {
		return nil
	}

	return p.refreshCache(ctx)
}

// refreshCache fetches fresh pricing data from OVH API
func (p *PricingClient) refreshCache(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if p.catalog != nil && time.Since(p.lastRefresh) <= p.cacheTTL {
		return nil
	}

	url := fmt.Sprintf("%s?ovhSubsidiary=%s", p.baseURL, p.subsidiary)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching pricing catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pricing API returned status %d", resp.StatusCode)
	}

	var catalog PricingCatalog
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return fmt.Errorf("decoding pricing catalog: %w", err)
	}

	p.catalog = &catalog
	p.lastRefresh = time.Now()
	p.extractFlavorPrices()

	return nil
}

// extractFlavorPrices parses the catalog and extracts hourly prices for flavors
func (p *PricingClient) extractFlavorPrices() {
	p.flavorPrices = make(map[string]float64)

	// Extract from addons (where instance flavors are typically listed)
	for _, addon := range p.catalog.Addons {
		// Look for instance-related plan codes
		// OVH format: "instance-{flavor}.{region}.hour.consumption" or similar patterns
		planCode := strings.ToLower(addon.PlanCode)

		if !strings.Contains(planCode, "instance") {
			continue
		}

		for _, pricing := range addon.Pricings {
			// Look for hourly consumption pricing
			isHourly := strings.Contains(pricing.Duration, "P1H") ||
			            strings.Contains(pricing.Description, "hour") ||
			            pricing.Interval == 1

			hasConsumption := false
			for _, cap := range pricing.Capacities {
				if cap == "consumption" {
					hasConsumption = true
					break
				}
			}

			if isHourly && hasConsumption && pricing.Price > 0 {
				// Extract flavor name from plan code
				// Examples: "instance-b3-8.gra7.hour.consumption" -> "b3-8"
				flavorName := extractFlavorFromPlanCode(planCode)
				if flavorName != "" {
					// Price is in micro-units, convert to EUR
					priceEUR := float64(pricing.Price) / 100000000.0
					p.flavorPrices[flavorName] = priceEUR

					// Also store with full plan code for region-specific lookup
					p.flavorPrices[planCode] = priceEUR
				}
			}
		}
	}
}

// extractFlavorFromPlanCode extracts the flavor name from an OVH plan code
func extractFlavorFromPlanCode(planCode string) string {
	// Remove common prefixes and suffixes
	planCode = strings.ToLower(planCode)
	planCode = strings.TrimPrefix(planCode, "instance-")

	// Split by "." and take the first part (flavor name)
	parts := strings.Split(planCode, ".")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// estimatePrice provides a fallback price estimation when API is unavailable
func (p *PricingClient) estimatePrice(flavorName string) float64 {
	// Parse flavor name to estimate resources
	// OVH naming: {category}{generation}-{size} e.g., b3-8, c2-15, r2-30

	// Default estimation based on typical OVH pricing
	// These are approximate EUR/hour values
	flavorPrices := map[string]float64{
		// B2 General Purpose
		"b2-7":   0.0283,
		"b2-15":  0.0567,
		"b2-30":  0.1134,
		"b2-60":  0.2268,
		"b2-120": 0.4536,
		// B3 General Purpose (newer gen)
		"b3-8":   0.0340,
		"b3-16":  0.0680,
		"b3-32":  0.1360,
		"b3-64":  0.2720,
		"b3-128": 0.5440,
		// C2 Compute Optimized
		"c2-7":   0.0340,
		"c2-15":  0.0680,
		"c2-30":  0.1360,
		"c2-60":  0.2720,
		"c2-120": 0.5440,
		// C3 Compute Optimized (newer gen)
		"c3-8":   0.0408,
		"c3-16":  0.0816,
		"c3-32":  0.1632,
		"c3-64":  0.3264,
		"c3-128": 0.6528,
		// R2 Memory Optimized
		"r2-15":  0.0567,
		"r2-30":  0.0850,
		"r2-60":  0.1700,
		"r2-120": 0.3400,
		"r2-240": 0.6800,
		// R3 Memory Optimized (newer gen)
		"r3-16":  0.0680,
		"r3-32":  0.1020,
		"r3-64":  0.2040,
		"r3-128": 0.4080,
		"r3-256": 0.8160,
		// GPU instances (T series)
		"t1-45":  0.90,
		"t1-90":  1.80,
		"t1-180": 3.60,
		"t2-45":  1.10,
		"t2-90":  2.20,
		"t2-180": 4.40,
	}

	if price, ok := flavorPrices[flavorName]; ok {
		return price
	}

	// Generic estimation: $0.02/vCPU + $0.005/GiB RAM (rough estimate)
	// For unknown flavors, assume 4 vCPU average
	return 0.10
}

// ForceRefresh forces a refresh of the pricing cache
func (p *PricingClient) ForceRefresh(ctx context.Context) error {
	p.mu.Lock()
	p.catalog = nil
	p.lastRefresh = time.Time{}
	p.mu.Unlock()

	return p.refreshCache(ctx)
}

// GetCachedPrices returns all cached flavor prices (for debugging)
func (p *PricingClient) GetCachedPrices() map[string]float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]float64, len(p.flavorPrices))
	for k, v := range p.flavorPrices {
		result[k] = v
	}
	return result
}
