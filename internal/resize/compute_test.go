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

package resize

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestApplyCPURatio(t *testing.T) {
	cases := []struct {
		name           string
		original       string
		baseline, node float64
		want           string
	}{
		{"same perf, no change", "1000m", 1.0, 1.0, "1000m"},
		{"more powerful node reduces", "1000m", 1.0, 1.25, "800m"},
		{"less powerful node increases", "800m", 1.0, 0.8, "1000m"},
		{"fractional cores", "250m", 1.0, 1.25, "200m"},
		{"zero node perf returns original", "500m", 1.0, 0.0, "500m"},
		{"large value", "16", 1.0, 2.0, "8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := resource.MustParse(tc.original)
			got := ApplyCPURatio(orig, tc.baseline, tc.node)
			want := resource.MustParse(tc.want)
			if got.Cmp(want) != 0 {
				t.Errorf("got %s, want %s", got.String(), want.String())
			}
		})
	}
}

func TestApplyMemoryRatio(t *testing.T) {
	cases := []struct {
		name           string
		original       string
		baseline, node float64
		want           string
	}{
		{"same perf, no change", "1Gi", 1.0, 1.0, "1Gi"},
		{"more powerful node reduces", "1Gi", 1.0, 2.0, "512Mi"},
		{"less powerful node increases", "512Mi", 1.0, 0.5, "1Gi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := resource.MustParse(tc.original)
			got := ApplyMemoryRatio(orig, tc.baseline, tc.node)
			want := resource.MustParse(tc.want)
			if got.Cmp(want) != 0 {
				t.Errorf("got %s, want %s", got.String(), want.String())
			}
		})
	}
}

func TestClamp(t *testing.T) {
	min := resource.MustParse("50m")
	max := resource.MustParse("16")
	cases := []struct {
		name        string
		q           string
		want        string
		hitMin, max bool
	}{
		{"in range", "1000m", "1000m", false, false},
		{"below min", "10m", "50m", true, false},
		{"at min", "50m", "50m", false, false},
		{"above max", "32", "16", false, true},
		{"at max", "16", "16", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := resource.MustParse(tc.q)
			got, hitMin, hitMax := Clamp(q, min, max)
			want := resource.MustParse(tc.want)
			if got.Cmp(want) != 0 {
				t.Errorf("value: got %s, want %s", got.String(), want.String())
			}
			if hitMin != tc.hitMin {
				t.Errorf("hitMin: got %v, want %v", hitMin, tc.hitMin)
			}
			if hitMax != tc.max {
				t.Errorf("hitMax: got %v, want %v", hitMax, tc.max)
			}
		})
	}
}
