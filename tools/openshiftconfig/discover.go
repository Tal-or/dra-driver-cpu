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

package openshiftconfig

import (
	"context"
	"fmt"
	"maps"
	"strings"

	mcov1 "github.com/openshift/api/machineconfiguration/v1"
	performancev2 "github.com/openshift/cluster-node-tuning-operator/pkg/apis/performanceprofile/v2"
	"k8s.io/utils/cpuset"
)

// findMatchingProfile returns the PerformanceProfile whose nodeSelector
// matches the MCP's nodeSelector.
func findMatchingProfile(ctx context.Context, clients Clients, ng *nodeGroup) (*performancev2.PerformanceProfile, error) {
	list := &performancev2.PerformanceProfileList{}
	if err := clients.API.List(ctx, list); err != nil {
		return nil, fmt.Errorf("listing PerformanceProfiles: %w", err)
	}
	for i := range list.Items {
		if profileMatchesNodeGroup(&list.Items[i], ng) {
			return &list.Items[i], nil
		}
	}
	return nil, fmt.Errorf("no PerformanceProfile matches node group %q", ng.mcp.Name)
}

// reservedFromProfile extracts the reserved CPU set from a PerformanceProfile.
func reservedFromProfile(pp *performancev2.PerformanceProfile) (*cpuset.CPUSet, error) {
	if pp.Spec.CPU == nil || pp.Spec.CPU.Reserved == nil {
		return nil, fmt.Errorf("PerformanceProfile %q has no reserved CPUs defined", pp.Name)
	}
	raw := strings.TrimSpace(string(*pp.Spec.CPU.Reserved))
	if raw == "" {
		return nil, fmt.Errorf("PerformanceProfile %q has empty reserved CPUs", pp.Name)
	}
	parsed, err := cpuset.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("PerformanceProfile %q reserved CPUs %q: %w", pp.Name, raw, err)
	}
	if parsed.IsEmpty() {
		return nil, fmt.Errorf("PerformanceProfile %q has empty reserved CPU set", pp.Name)
	}
	return &parsed, nil
}

// profileMatchesNodeGroup checks whether a PerformanceProfile targets exactly
// the same set of nodes as the given MCP by requiring their node-level label
// selectors to be identical.
func profileMatchesNodeGroup(pp *performancev2.PerformanceProfile, ng *nodeGroup) bool {
	if len(pp.Spec.NodeSelector) == 0 {
		return false
	}
	mcpNodeLabels := mcpNodeSelectorLabels(ng.mcp)
	if len(mcpNodeLabels) == 0 {
		return false
	}
	return maps.Equal(mcpNodeLabels, pp.Spec.NodeSelector)
}

func mcpNodeSelectorLabels(mcp *mcov1.MachineConfigPool) map[string]string {
	if mcp.Spec.NodeSelector == nil {
		return nil
	}
	return mcp.Spec.NodeSelector.MatchLabels
}
