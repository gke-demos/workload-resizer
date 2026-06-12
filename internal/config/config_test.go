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
	"testing"
)

func TestParseAndValidate(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "valid simple config",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
  c3: { cpuPerf: 1.3, memPerf: 1.0 }
bounds:
  cpu:
    min: "50m"
    max: "16"
  memory:
    min: "64Mi"
    max: "32Gi"
`,
			wantErr: false,
		},
		{
			name: "missing baseline node type",
			yaml: `
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
`,
			wantErr: true,
		},
		{
			name: "baseline node type not in nodeTypes",
			yaml: `
baselineNodeType: c3
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
`,
			wantErr: true,
		},
		{
			name: "invalid global bounds",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "16", max: "50m" }
  memory: { min: "64Mi", max: "32Gi" }
`,
			wantErr: true,
		},
		{
			name: "valid config with overrides",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
  c3: { cpuPerf: 1.3, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
overrides:
  - name: "performance-class"
    match:
      computeClass: "Performance"
    baselineNodeType: c3
    nodeTypes:
      c3: { cpuPerf: 1.0, memPerf: 1.0 }
    bounds:
      cpu: { min: "100m", max: "32" }
  - name: "critical-pods"
    match:
      podLabel:
        priority: "critical"
    bounds:
      cpu: { min: "200m", max: "16" }
`,
			wantErr: false,
		},
		{
			name: "override with empty name",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
overrides:
  - name: ""
    match:
      computeClass: "Performance"
`,
			wantErr: true,
		},
		{
			name: "override with duplicate name",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
overrides:
  - name: "dup-name"
    match:
      computeClass: "Performance"
  - name: "dup-name"
    match:
      podLabel:
        app: "web"
`,
			wantErr: true,
		},
		{
			name: "override with empty match criteria",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
overrides:
  - name: "empty-match"
    match: {}
`,
			wantErr: true,
		},
		{
			name: "override with baseline node type missing from override and global nodeTypes",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
overrides:
  - name: "invalid-baseline"
    match:
      computeClass: "Performance"
    baselineNodeType: custom-node-type
`,
			wantErr: true,
		},
		{
			name: "override with baseline node type defined in its own nodeTypes",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
overrides:
  - name: "valid-baseline-in-override"
    match:
      computeClass: "Performance"
    baselineNodeType: custom-node-type
    nodeTypes:
      custom-node-type: { cpuPerf: 1.5, memPerf: 1.2 }
`,
			wantErr: false,
		},
		{
			name: "override with invalid perf values",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
overrides:
  - name: "invalid-perf"
    match:
      computeClass: "Performance"
    nodeTypes:
      n2d: { cpuPerf: -1.0, memPerf: 1.0 }
`,
			wantErr: true,
		},
		{
			name: "override with invalid bounds",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
overrides:
  - name: "invalid-bounds"
    match:
      computeClass: "Performance"
    bounds:
      cpu: { min: "16", max: "50m" }
`,
			wantErr: true,
		},
		{
			name: "valid config with computeClasses map",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
  c3: { cpuPerf: 1.3, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
computeClasses:
  Performance:
    baselineNodeType: c3
    bounds:
      cpu: { min: "100m", max: "32" }
  custom-class:
    nodeTypes:
      n2d: { cpuPerf: 1.1, memPerf: 1.1 }
`,
			wantErr: false,
		},
		{
			name: "invalid computeClasses with bad baseline",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
computeClasses:
  Performance:
    baselineNodeType: invalid-node-type
`,
			wantErr: true,
		},
		{
			name: "valid config with podLabels double map",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
podLabels:
  env:
    production:
      bounds:
        cpu: { min: "500m", max: "32" }
    sandbox:
      bounds:
        cpu: { min: "10m", max: "100m" }
`,
			wantErr: false,
		},
		{
			name: "invalid podLabels with negative performance",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
podLabels:
  env:
    sandbox:
      nodeTypes:
        n2d: { cpuPerf: -1.0, memPerf: 1.0 }
`,
			wantErr: true,
		},
		{
			name: "valid config with podAnnotations double map",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
podAnnotations:
  workload-resizer.io/tier:
    critical:
      bounds:
        cpu: { min: "1000m", max: "64" }
`,
			wantErr: false,
		},
		{
			name: "invalid podAnnotations with bad bounds",
			yaml: `
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
podAnnotations:
  workload-resizer.io/tier:
    critical:
      bounds:
        cpu: { min: "64", max: "1000m" }
`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if (err != nil) != tc.wantErr {
				t.Errorf("Parse() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
