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

	performancev2 "github.com/openshift/cluster-node-tuning-operator/pkg/apis/performanceprofile/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/cpuset"
)

func TestReservedFromProfile(t *testing.T) {
	pp := performanceProfile("pp", "0,1", defaultNodeSelector, nil)
	got, err := reservedFromProfile(pp)
	require.NoError(t, err)
	want, err := cpuset.Parse("0,1")
	require.NoError(t, err)
	assert.True(t, want.Equals(*got))
}

func TestReservedFromProfile_Range(t *testing.T) {
	pp := performanceProfile("pp", "4-7", defaultNodeSelector, nil)
	got, err := reservedFromProfile(pp)
	require.NoError(t, err)
	want, err := cpuset.Parse("4-7")
	require.NoError(t, err)
	assert.True(t, want.Equals(*got))
}

func TestReservedFromProfile_NoCPU(t *testing.T) {
	pp := performanceProfile("pp", "0-1", defaultNodeSelector, nil)
	pp.Spec.CPU = nil
	_, err := reservedFromProfile(pp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no reserved CPUs defined")
}

func TestReservedFromProfile_NilReserved(t *testing.T) {
	pp := performanceProfile("pp", "0-1", defaultNodeSelector, nil)
	pp.Spec.CPU.Reserved = nil
	_, err := reservedFromProfile(pp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no reserved CPUs defined")
}

func TestReservedFromProfile_Empty(t *testing.T) {
	pp := performanceProfile("pp", "0-1", defaultNodeSelector, nil)
	empty := performancev2.CPUSet("")
	pp.Spec.CPU.Reserved = &empty
	_, err := reservedFromProfile(pp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty reserved CPUs")
}

func TestProfileMatchesNodeGroup(t *testing.T) {
	pp := performanceProfile("pp", "0-1", defaultNodeSelector, nil)

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
