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
	"math"

	"k8s.io/apimachinery/pkg/api/resource"
)

// ApplyCPURatio scales an original CPU quantity by baselinePerf/nodePerf.
// If nodePerf is non-positive the original is returned unchanged.
func ApplyCPURatio(original resource.Quantity, baselinePerf, nodePerf float64) resource.Quantity {
	if nodePerf <= 0 {
		return original.DeepCopy()
	}
	scaled := float64(original.MilliValue()) * baselinePerf / nodePerf
	return *resource.NewMilliQuantity(int64(math.Round(scaled)), original.Format)
}

// ApplyMemoryRatio scales an original memory quantity by baselinePerf/nodePerf.
func ApplyMemoryRatio(original resource.Quantity, baselinePerf, nodePerf float64) resource.Quantity {
	if nodePerf <= 0 {
		return original.DeepCopy()
	}
	scaled := float64(original.Value()) * baselinePerf / nodePerf
	return *resource.NewQuantity(int64(math.Round(scaled)), original.Format)
}

// Clamp constrains q to [min, max] and reports which (if any) bound was hit.
func Clamp(q, min, max resource.Quantity) (clamped resource.Quantity, hitMin, hitMax bool) {
	if q.Cmp(min) < 0 {
		return min.DeepCopy(), true, false
	}
	if q.Cmp(max) > 0 {
		return max.DeepCopy(), false, true
	}
	return q.DeepCopy(), false, false
}
