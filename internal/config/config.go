/*
Copyright 2026 Google LLC

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

package config

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

const ConfigKey = "config.yaml"

type NodeProfile struct {
	CPUPerf float64 `json:"cpuPerf"`
	MemPerf float64 `json:"memPerf"`
}

type Bound struct {
	Min resource.Quantity `json:"min"`
	Max resource.Quantity `json:"max"`
}

type Bounds struct {
	CPU    Bound `json:"cpu"`
	Memory Bound `json:"memory"`
}

type Config struct {
	BaselineInstanceType string                 `json:"baselineInstanceType"`
	NodeTypes            map[string]NodeProfile `json:"nodeTypes"`
	Bounds               Bounds                 `json:"bounds"`
}

func (c *Config) Validate() error {
	if c.BaselineInstanceType == "" {
		return fmt.Errorf("baselineInstanceType is required")
	}
	if _, ok := c.NodeTypes[c.BaselineInstanceType]; !ok {
		return fmt.Errorf("baselineInstanceType %q must appear in nodeTypes", c.BaselineInstanceType)
	}
	for name, p := range c.NodeTypes {
		if p.CPUPerf <= 0 {
			return fmt.Errorf("nodeTypes[%q].cpuPerf must be > 0", name)
		}
		if p.MemPerf <= 0 {
			return fmt.Errorf("nodeTypes[%q].memPerf must be > 0", name)
		}
	}
	if c.Bounds.CPU.Min.Cmp(c.Bounds.CPU.Max) > 0 {
		return fmt.Errorf("bounds.cpu.min > bounds.cpu.max")
	}
	if c.Bounds.Memory.Min.Cmp(c.Bounds.Memory.Max) > 0 {
		return fmt.Errorf("bounds.memory.min > bounds.memory.max")
	}
	return nil
}

func Parse(raw []byte) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

type Store struct {
	mu  sync.RWMutex
	cfg *Config
}

func NewStore() *Store { return &Store{} }

func (s *Store) Get() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Set replaces the in-memory config. Used by Refresher and by tests that need
// to seed config without an actual ConfigMap.
func (s *Store) Set(c *Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = c
}

type Refresher struct {
	Reader    client.Reader
	Store     *Store
	Namespace string
	Name      string
	Interval  time.Duration
}

func (r *Refresher) Start(ctx context.Context) error {
	if err := r.refreshOnce(ctx); err != nil {
		return fmt.Errorf("initial config load: %w", err)
	}
	go r.loop(ctx)
	return nil
}

func (r *Refresher) loop(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("config-refresher")
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.refreshOnce(ctx); err != nil {
				logger.Error(err, "config refresh failed; keeping previous config")
			}
		}
	}
}

func (r *Refresher) refreshOnce(ctx context.Context) error {
	var cm corev1.ConfigMap
	if err := r.Reader.Get(ctx, types.NamespacedName{Namespace: r.Namespace, Name: r.Name}, &cm); err != nil {
		return fmt.Errorf("get configmap %s/%s: %w", r.Namespace, r.Name, err)
	}
	raw, ok := cm.Data[ConfigKey]
	if !ok {
		return fmt.Errorf("configmap %s/%s missing key %q", r.Namespace, r.Name, ConfigKey)
	}
	cfg, err := Parse([]byte(raw))
	if err != nil {
		return err
	}
	r.Store.Set(cfg)
	return nil
}
