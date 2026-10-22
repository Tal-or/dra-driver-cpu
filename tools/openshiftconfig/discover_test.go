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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/cpuset"
)

func TestReservedFromKubeletConfig(t *testing.T) {
	kc := kubeletConfig("kc", "0,1", nil)
	got, found, err := reservedFromKubeletConfig(kc)
	require.NoError(t, err)
	require.True(t, found)
	want, err := cpuset.Parse("0,1")
	require.NoError(t, err)
	assert.True(t, want.Equals(*got))
}

func TestReservedFromKubeletConfig_JSONBlob(t *testing.T) {
	kc := kubeletConfig("kc-json", "", nil)
	kc.Spec.KubeletConfig.Raw = []byte(`{"reservedSystemCPUs":"4-7"}`)
	got, found, err := reservedFromKubeletConfig(kc)
	require.NoError(t, err)
	require.True(t, found)
	want, err := cpuset.Parse("4-7")
	require.NoError(t, err)
	assert.True(t, want.Equals(*got))
}

func TestReservedFromKubeletConfig_Absent(t *testing.T) {
	kc := kubeletConfig("kc", "", nil)
	_, found, err := reservedFromKubeletConfig(kc)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestKubeletConfigSelectsPool(t *testing.T) {
	mcp := defaultMCP()

	withSelector := kubeletConfig("with", "0-1", defaultPoolSelector)
	assert.True(t, kubeletConfigSelectsPool(withSelector, mcp))

	wrongSelector := kubeletConfig("wrong", "0-1", map[string]string{"pools.operator.machineconfiguration.openshift.io/master": ""})
	assert.False(t, kubeletConfigSelectsPool(wrongSelector, mcp))

	noSelector := kubeletConfig("none", "0-1", nil)
	assert.False(t, kubeletConfigSelectsPool(noSelector, mcp))
}

func TestProfileMatchesNodeGroup(t *testing.T) {
	pp := performanceProfile("pp", defaultNodeSelector, nil)

	workerNG := &nodeGroup{mcp: defaultMCP()}
	assert.True(t, profileMatchesNodeGroup(pp, workerNG))

	masterMCP := machineConfigPool("master",
		map[string]string{"pools.operator.machineconfiguration.openshift.io/master": ""},
		map[string]string{"node-role.kubernetes.io/master": ""},
	)
	assert.False(t, profileMatchesNodeGroup(pp, &nodeGroup{mcp: masterMCP}))

	supersetMCP := machineConfigPool("superset", nil,
		map[string]string{"node-role.kubernetes.io/worker": "", "extra": "label"},
	)
	assert.False(t, profileMatchesNodeGroup(pp, &nodeGroup{mcp: supersetMCP}))
}
