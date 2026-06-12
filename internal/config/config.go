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

type Match struct {
	// ComputeClass matches the value of the pod's "cloud.google.com/compute-class" nodeSelector.
	ComputeClass string `json:"computeClass,omitempty"`

	// PodLabel matches if the pod contains all the specified labels with their corresponding values.
	PodLabel map[string]string `json:"podLabel,omitempty"`

	// PodAnnotation matches if the pod contains all the specified annotations with their corresponding values.
	PodAnnotation map[string]string `json:"podAnnotation,omitempty"`
}

type Override struct {
	// Name is a descriptive name for the override rule.
	Name string `json:"name"`

	// Match defines the criteria for the pod to qualify for this override.
	// All specified fields inside the Match block must be satisfied (AND logic).
	Match Match `json:"match"`

	// BaselineNodeType overrides the global baseline node type.
	BaselineNodeType string `json:"baselineNodeType,omitempty"`

	// NodeTypes overrides or extends the performance profiles of specific node types.
	NodeTypes map[string]NodeProfile `json:"nodeTypes,omitempty"`

	// Bounds overrides the global sizing limits.
	Bounds *Bounds `json:"bounds,omitempty"`
}

type ComputeClassConfig struct {
	BaselineNodeType string                 `json:"baselineNodeType,omitempty"`
	NodeTypes        map[string]NodeProfile `json:"nodeTypes,omitempty"`
	Bounds           *Bounds                `json:"bounds,omitempty"`
}

type LabelConfig struct {
	BaselineNodeType string                 `json:"baselineNodeType,omitempty"`
	NodeTypes        map[string]NodeProfile `json:"nodeTypes,omitempty"`
	Bounds           *Bounds                `json:"bounds,omitempty"`
}

type AnnotationConfig struct {
	BaselineNodeType string                 `json:"baselineNodeType,omitempty"`
	NodeTypes        map[string]NodeProfile `json:"nodeTypes,omitempty"`
	Bounds           *Bounds                `json:"bounds,omitempty"`
}

type Config struct {
	// BaselineNodeType is the value of the node-type label (see the
	// controller's --node-type-label flag, default cloud.google.com/machine-family)
	// that the workload's requests are calibrated against. Must also
	// appear as a key in NodeTypes.
	BaselineNodeType string                                 `json:"baselineNodeType"`
	NodeTypes        map[string]NodeProfile                 `json:"nodeTypes"`
	Bounds           Bounds                                 `json:"bounds"`
	ComputeClasses   map[string]ComputeClassConfig          `json:"computeClasses,omitempty"`
	PodLabels        map[string]map[string]LabelConfig      `json:"podLabels,omitempty"`
	PodAnnotations   map[string]map[string]AnnotationConfig `json:"podAnnotations,omitempty"`
	Overrides        []Override                             `json:"overrides,omitempty"`
}

func validateSubConfig(ctxName, baseType string, nodeTypes map[string]NodeProfile, b *Bounds, globalNodeTypes map[string]NodeProfile) error {
	if baseType != "" {
		inSubNodeTypes := false
		if nodeTypes != nil {
			_, inSubNodeTypes = nodeTypes[baseType]
		}
		_, inGlobalNodeTypes := globalNodeTypes[baseType]
		if !inSubNodeTypes && !inGlobalNodeTypes {
			return fmt.Errorf("%s.baselineNodeType %q must appear in its own nodeTypes or global nodeTypes", ctxName, baseType)
		}
	}
	for name, p := range nodeTypes {
		if p.CPUPerf <= 0 {
			return fmt.Errorf("%s.nodeTypes[%q].cpuPerf must be > 0", ctxName, name)
		}
		if p.MemPerf <= 0 {
			return fmt.Errorf("%s.nodeTypes[%q].memPerf must be > 0", ctxName, name)
		}
	}
	if b != nil {
		if b.CPU.Min.Cmp(b.CPU.Max) > 0 {
			return fmt.Errorf("%s.bounds.cpu.min > %s.bounds.cpu.max", ctxName, ctxName)
		}
		if b.Memory.Min.Cmp(b.Memory.Max) > 0 {
			return fmt.Errorf("%s.bounds.memory.min > %s.bounds.memory.max", ctxName, ctxName)
		}
	}
	return nil
}

func (c *Config) Validate() error {
	if c.BaselineNodeType == "" {
		return fmt.Errorf("baselineNodeType is required")
	}
	if _, ok := c.NodeTypes[c.BaselineNodeType]; !ok {
		return fmt.Errorf("baselineNodeType %q must appear in nodeTypes", c.BaselineNodeType)
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

	// Validate direct ComputeClasses mapping
	for ccName, ccCfg := range c.ComputeClasses {
		if ccName == "" {
			return fmt.Errorf("computeClasses has empty key")
		}
		ctxName := fmt.Sprintf("computeClasses[%q]", ccName)
		if err := validateSubConfig(ctxName, ccCfg.BaselineNodeType, ccCfg.NodeTypes, ccCfg.Bounds, c.NodeTypes); err != nil {
			return err
		}
	}

	// Validate direct PodLabels mapping
	for lKey, labelVals := range c.PodLabels {
		if lKey == "" {
			return fmt.Errorf("podLabels has empty key")
		}
		for lVal, lCfg := range labelVals {
			if lVal == "" {
				return fmt.Errorf("podLabels[%q] has empty key", lKey)
			}
			ctxName := fmt.Sprintf("podLabels[%q][%q]", lKey, lVal)
			if err := validateSubConfig(ctxName, lCfg.BaselineNodeType, lCfg.NodeTypes, lCfg.Bounds, c.NodeTypes); err != nil {
				return err
			}
		}
	}

	// Validate direct PodAnnotations mapping
	for aKey, annoVals := range c.PodAnnotations {
		if aKey == "" {
			return fmt.Errorf("podAnnotations has empty key")
		}
		for aVal, aCfg := range annoVals {
			if aVal == "" {
				return fmt.Errorf("podAnnotations[%q] has empty key", aKey)
			}
			ctxName := fmt.Sprintf("podAnnotations[%q][%q]", aKey, aVal)
			if err := validateSubConfig(ctxName, aCfg.BaselineNodeType, aCfg.NodeTypes, aCfg.Bounds, c.NodeTypes); err != nil {
				return err
			}
		}
	}

	// Validate overrides
	seenNames := make(map[string]bool)
	for i, o := range c.Overrides {
		if o.Name == "" {
			return fmt.Errorf("overrides[%d].name cannot be empty", i)
		}
		if seenNames[o.Name] {
			return fmt.Errorf("duplicate override name %q", o.Name)
		}
		seenNames[o.Name] = true

		// Check match has at least one criteria
		if o.Match.ComputeClass == "" && len(o.Match.PodLabel) == 0 && len(o.Match.PodAnnotation) == 0 {
			return fmt.Errorf("overrides[%q].match must specify at least one criteria (computeClass, podLabel, or podAnnotation)", o.Name)
		}

		ctxName := fmt.Sprintf("overrides[%q]", o.Name)
		if err := validateSubConfig(ctxName, o.BaselineNodeType, o.NodeTypes, o.Bounds, c.NodeTypes); err != nil {
			return err
		}
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
