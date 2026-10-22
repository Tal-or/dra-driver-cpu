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
	"fmt"
	"maps"
	"strings"

	mcov1 "github.com/openshift/api/machineconfiguration/v1"
	performancev2 "github.com/openshift/cluster-node-tuning-operator/pkg/apis/performanceprofile/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"k8s.io/utils/cpuset"
	"sigs.k8s.io/yaml"
)

func reservedFromKubeletConfig(kc *mcov1.KubeletConfig) (*cpuset.CPUSet, bool, error) {
	if kc.Spec.KubeletConfig == nil || len(kc.Spec.KubeletConfig.Raw) == 0 {
		return nil, false, nil
	}
	var kubeletCfg kubeletconfigv1beta1.KubeletConfiguration
	if err := yaml.Unmarshal(kc.Spec.KubeletConfig.Raw, &kubeletCfg); err != nil {
		return nil, false, fmt.Errorf("reading reservedSystemCPUs from KubeletConfig %q: %w", kc.Name, err)
	}
	raw := strings.TrimSpace(kubeletCfg.ReservedSystemCPUs)
	if raw == "" {
		return nil, false, nil
	}
	parsed, err := cpuset.Parse(raw)
	if err != nil {
		return nil, false, fmt.Errorf("KubeletConfig %q reservedSystemCPUs %q: %w", kc.Name, raw, err)
	}
	if parsed.IsEmpty() {
		return nil, false, nil
	}
	return &parsed, true, nil
}

// kubeletConfigSelectsPool checks whether a KubeletConfig targets
// the given MCP via its machineConfigPoolSelector.
func kubeletConfigSelectsPool(kc *mcov1.KubeletConfig, mcp *mcov1.MachineConfigPool) bool {
	if kc.Spec.MachineConfigPoolSelector == nil {
		return false
	}
	selector, err := metav1.LabelSelectorAsSelector(kc.Spec.MachineConfigPoolSelector)
	if err != nil {
		return false
	}
	return !selector.Empty() && selector.Matches(labels.Set(mcp.Labels))
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
