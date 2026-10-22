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
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	configv1 "github.com/openshift/api/config/v1"
	mcov1 "github.com/openshift/api/machineconfiguration/v1"
	performancev2 "github.com/openshift/cluster-node-tuning-operator/pkg/apis/performanceprofile/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/cpuset"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSetup_NotOpenShift(t *testing.T) {
	clients := makeClients(t, false)
	_, err := Setup(context.Background(), testr.New(t), clients, defaultOpts())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not OpenShift")
}

func TestSetup_RequiresNodeGroup(t *testing.T) {
	clients := makeClients(t, true)
	opts := defaultOpts()
	opts.NodeGroup = ""
	_, err := Setup(context.Background(), testr.New(t), clients, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node group")
}

func TestSetup_DiscoversReservedFromPerformanceProfile(t *testing.T) {
	pp := performanceProfile("worker-pp", "0-3", defaultNodeSelector, nil)
	clients := makeClients(t, true, pp, defaultMCP())

	result, err := Setup(context.Background(), testr.New(t), clients, defaultOpts())
	require.NoError(t, err)
	assertReserved(t, "0-3", result.ReservedCPUs)
}

func TestSetup_SelectsProfileByNodeGroup(t *testing.T) {
	workerPP := performanceProfile("worker-pp", "0,2", defaultNodeSelector, nil)
	masterPP := performanceProfile("master-pp", "0-7",
		map[string]string{"node-role.kubernetes.io/master": ""}, nil)
	clients := makeClients(t, true, workerPP, masterPP, defaultMCP())

	result, err := Setup(context.Background(), testr.New(t), clients, defaultOpts())
	require.NoError(t, err)
	assertReserved(t, "0,2", result.ReservedCPUs)
}

func TestSetup_AnnotatesPerformanceProfile(t *testing.T) {
	shortenMCPTimeouts(t)

	pp := performanceProfile("worker-pp", "0-1", defaultNodeSelector, nil)
	clients := makeClients(t, true, pp, defaultMCP())

	// Simulate MCO reacting: Updating=True then Updated=True.
	go simulateMCPRollout(t, clients.API, defaultNodeGroup)

	opts := defaultOpts()
	opts.EnableDRAAnnotation = true
	_, err := Setup(context.Background(), testr.New(t), clients, opts)
	require.NoError(t, err)

	got := &performancev2.PerformanceProfile{}
	require.NoError(t, clients.API.Get(context.Background(), client.ObjectKey{Name: "worker-pp"}, got))
	assert.Equal(t, "true", got.Annotations[DRAResourceManagementAnnotation])
}

func TestSetup_SkipsAnnotationWhenAlreadySet(t *testing.T) {
	pp := performanceProfile("worker-pp", "0-1", defaultNodeSelector, map[string]string{DRAResourceManagementAnnotation: "true"})
	clients := makeClients(t, true, pp, defaultMCP())

	opts := defaultOpts()
	opts.EnableDRAAnnotation = true
	_, err := Setup(context.Background(), testr.New(t), clients, opts)
	require.NoError(t, err)

	got := &performancev2.PerformanceProfile{}
	require.NoError(t, clients.API.Get(context.Background(), client.ObjectKey{Name: "worker-pp"}, got))
	assert.Equal(t, "true", got.Annotations[DRAResourceManagementAnnotation])
}

func TestSetup_ErrorsWhenNoMatchingProfile(t *testing.T) {
	pp := performanceProfile("master-pp", "0-1", map[string]string{"node-role.kubernetes.io/master": ""}, nil)
	clients := makeClients(t, true, pp, defaultMCP())

	opts := defaultOpts()
	opts.EnableDRAAnnotation = true
	_, err := Setup(context.Background(), testr.New(t), clients, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PerformanceProfile matches")

	got := &performancev2.PerformanceProfile{}
	require.NoError(t, clients.API.Get(context.Background(), client.ObjectKey{Name: "master-pp"}, got))
	assert.Empty(t, got.Annotations[DRAResourceManagementAnnotation])
}

func TestSetup_DryRunDoesNotAnnotate(t *testing.T) {
	pp := performanceProfile("worker-pp", "0-1", defaultNodeSelector, nil)
	clients := makeClients(t, true, pp, defaultMCP())
	opts := defaultOpts()
	opts.EnableDRAAnnotation = true
	opts.DryRun = true

	result, err := Setup(context.Background(), testr.New(t), clients, opts)
	require.NoError(t, err)
	assertReserved(t, "0-1", result.ReservedCPUs)

	got := &performancev2.PerformanceProfile{}
	require.NoError(t, clients.API.Get(context.Background(), client.ObjectKey{Name: "worker-pp"}, got))
	assert.Empty(t, got.Annotations[DRAResourceManagementAnnotation])
}

func TestSetup_NoMatchingPerformanceProfile(t *testing.T) {
	clients := makeClients(t, true, defaultMCP())
	_, err := Setup(context.Background(), testr.New(t), clients, defaultOpts())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PerformanceProfile matches")
}

func TestRun_PrintsReservedCPUs(t *testing.T) {
	pp := performanceProfile("worker-pp", "0-1", defaultNodeSelector, nil)
	clients := makeClients(t, true, pp, defaultMCP())

	var stdout bytes.Buffer
	err := Run([]string{
		"--kubeconfig", "/dev/null",
		"--node-group", defaultNodeGroup,
	}, Options{
		Logger: logr.Discard(),
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
		NewClients: func(string) (Clients, error) {
			return clients, nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "0-1\n", stdout.String())
}

func TestRun_RejectsPositionalArgs(t *testing.T) {
	err := Run([]string{"extra"}, Options{Stderr: &bytes.Buffer{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "positional arguments")
}

// --- helpers ---

const defaultNodeGroup = "worker"

var (
	defaultNodeSelector = map[string]string{"node-role.kubernetes.io/worker": ""}
	defaultPoolSelector = map[string]string{"pools.operator.machineconfiguration.openshift.io/worker": ""}
)

func defaultOpts() SetupOptions {
	return SetupOptions{
		NodeGroup: defaultNodeGroup,
	}
}

func defaultMCP() *mcov1.MachineConfigPool {
	return machineConfigPool(defaultNodeGroup, defaultPoolSelector, defaultNodeSelector)
}

// simulateMCPRollout mimics the MCO reacting to an annotation change:
// after a brief delay it sets Updating=True, then switches to Updated=True.
func simulateMCPRollout(t *testing.T, c client.Client, mcpName string) {
	t.Helper()
	ctx := context.Background()
	key := client.ObjectKey{Name: mcpName}

	time.Sleep(50 * time.Millisecond)

	mcp := &mcov1.MachineConfigPool{}
	if err := c.Get(ctx, key, mcp); err != nil {
		t.Logf("simulateMCPRollout: get for Updating: %v", err)
		return
	}
	mcp.Status.Conditions = []mcov1.MachineConfigPoolCondition{
		{Type: mcov1.MachineConfigPoolUpdating, Status: corev1.ConditionTrue},
		{Type: mcov1.MachineConfigPoolUpdated, Status: corev1.ConditionFalse},
	}
	if err := c.Status().Update(ctx, mcp); err != nil {
		t.Logf("simulateMCPRollout: status update for Updating: %v", err)
		return
	}

	time.Sleep(50 * time.Millisecond)

	if err := c.Get(ctx, key, mcp); err != nil {
		t.Logf("simulateMCPRollout: get for Updated: %v", err)
		return
	}
	mcp.Status.Conditions = []mcov1.MachineConfigPoolCondition{
		{Type: mcov1.MachineConfigPoolUpdating, Status: corev1.ConditionFalse},
		{Type: mcov1.MachineConfigPoolUpdated, Status: corev1.ConditionTrue},
	}
	if err := c.Status().Update(ctx, mcp); err != nil {
		t.Logf("simulateMCPRollout: status update for Updated: %v", err)
		return
	}
}

// shortenMCPTimeouts overrides the package-level MCP wait durations so
// tests don't wait for real production timeouts.
func shortenMCPTimeouts(t *testing.T) {
	t.Helper()
	origUpdating, origUpdated, origPoll := mcpUpdatingTimeout, mcpUpdatedTimeout, mcpPollInterval
	mcpUpdatingTimeout = 200 * time.Millisecond
	mcpUpdatedTimeout = 200 * time.Millisecond
	mcpPollInterval = 50 * time.Millisecond
	t.Cleanup(func() {
		mcpUpdatingTimeout = origUpdating
		mcpUpdatedTimeout = origUpdated
		mcpPollInterval = origPoll
	})
}

func makeClients(t *testing.T, openShift bool, objects ...client.Object) Clients {
	t.Helper()
	scheme, err := newScheme()
	require.NoError(t, err)
	var api client.Client = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&mcov1.MachineConfigPool{}).
		Build()
	if !openShift {
		api = &notOpenShiftClient{Client: api}
	}
	return Clients{API: api}
}

type notOpenShiftClient struct {
	client.Client
}

func (c *notOpenShiftClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*configv1.ClusterVersionList); ok {
		return &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: configv1.GroupName, Kind: "ClusterVersion"}}
	}
	return c.Client.List(ctx, list, opts...)
}

func performanceProfile(name, reserved string, nodeSelector, annotations map[string]string) *performancev2.PerformanceProfile {
	reservedCPUs := performancev2.CPUSet(reserved)
	isolated := performancev2.CPUSet("4-7")
	return &performancev2.PerformanceProfile{
		TypeMeta: metav1.TypeMeta{
			APIVersion: performancev2.GroupVersion.String(),
			Kind:       "PerformanceProfile",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Spec: performancev2.PerformanceProfileSpec{
			CPU: &performancev2.CPU{
				Reserved: &reservedCPUs,
				Isolated: &isolated,
			},
			NodeSelector: nodeSelector,
		},
	}
}

func machineConfigPool(name string, labels, nodeSelector map[string]string) *mcov1.MachineConfigPool {
	return &mcov1.MachineConfigPool{
		TypeMeta: metav1.TypeMeta{
			APIVersion: mcov1.GroupVersion.String(),
			Kind:       "MachineConfigPool",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: mcov1.MachineConfigPoolSpec{
			NodeSelector: &metav1.LabelSelector{MatchLabels: nodeSelector},
		},
	}
}

func assertReserved(t *testing.T, wantRaw, gotRaw string) {
	t.Helper()
	want, err := cpuset.Parse(wantRaw)
	require.NoError(t, err)
	got, err := cpuset.Parse(gotRaw)
	require.NoError(t, err)
	assert.True(t, want.Equals(got), "reservedCPUs=%q want %q", gotRaw, want.String())
}
