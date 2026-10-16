// Copyright 2026-present Orbit Contributors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package discovery

import (
	"errors"
	"fmt"
	"sync"
)

// ErrNoInstances reports a pick request before the discovery snapshot has endpoints.
var ErrNoInstances = errors.New("service discovery has no available instances")

// WeightedPicker selects from a refreshed endpoint snapshot using smooth weighted round robin.
//
// The picker performs no network calls, owns no discovery client, and is safe for concurrent
// callers. Refresh it with a new validated snapshot when the application refreshes discovery.
type WeightedPicker struct {
	mu        sync.Mutex
	service   string
	instances []Instance
	current   []int64
	total     int64
}

// NewWeightedPicker validates and snapshots one discovery result for local endpoint selection.
// An empty initial result is allowed; Pick then returns ErrNoInstances until a later Update.
func NewWeightedPicker(service string, instances []Instance) (*WeightedPicker, error) {
	if validateIdentifier(service) != nil {
		return nil, fmt.Errorf("invalid service identifier")
	}
	picker := &WeightedPicker{service: service}
	if err := picker.Update(instances); err != nil {
		return nil, err
	}
	return picker, nil
}

// Update atomically replaces the endpoint snapshot and resets the weighted schedule.
// Invalid, duplicate, or oversized snapshots leave the previous snapshot unchanged.
func (p *WeightedPicker) Update(instances []Instance) error {
	if p == nil {
		return fmt.Errorf("service-discovery picker is not initialized")
	}
	if len(instances) > MaxInstancesPerResponse {
		return fmt.Errorf("service-discovery result exceeds endpoint limit")
	}
	validated := make([]Instance, 0, len(instances))
	seen := make(map[string]struct{}, len(instances))
	var total int64
	for _, instance := range instances {
		if err := validateInstance(instance, p.service); err != nil {
			return fmt.Errorf("service-discovery result contains an invalid endpoint")
		}
		if _, duplicate := seen[instance.InstanceID]; duplicate {
			return fmt.Errorf("service-discovery result contains duplicate endpoints")
		}
		seen[instance.InstanceID] = struct{}{}
		instance.Host, _ = normalizeHost(instance.Host)
		validated = append(validated, cloneInstance(instance))
		total += int64(instance.Weight)
	}
	p.mu.Lock()
	p.instances = validated
	p.current = make([]int64, len(validated))
	p.total = total
	p.mu.Unlock()
	return nil
}

// Pick returns a detached endpoint selected in proportion to its configured weight.
func (p *WeightedPicker) Pick() (Instance, error) {
	if p == nil {
		return Instance{}, fmt.Errorf("service-discovery picker is not initialized")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.instances) == 0 {
		return Instance{}, ErrNoInstances
	}
	selected := -1
	for index, instance := range p.instances {
		p.current[index] += int64(instance.Weight)
		if selected < 0 || p.current[index] > p.current[selected] {
			selected = index
		}
	}
	p.current[selected] -= p.total
	return cloneInstance(p.instances[selected]), nil
}
