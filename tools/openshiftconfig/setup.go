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
	"strings"
	"time"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	mcov1 "github.com/openshift/api/machineconfiguration/v1"
	performancev2 "github.com/openshift/cluster-node-tuning-operator/pkg/apis/performanceprofile/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// nodeGroup holds the MCP used to match PerformanceProfiles by nodeSelector.
type nodeGroup struct {
	mcp *mcov1.MachineConfigPool
}

// Clients is the cluster access needed to discover reserved CPUs and
// annotate PerformanceProfiles.
type Clients struct {
	API client.Client
}

// SetupOptions configures how reserved CPUs are discovered.
type SetupOptions struct {
	// NodeGroup is the MachineConfigPool name used to find the matching
	// PerformanceProfile by nodeSelector. Required.
	NodeGroup           string
	DryRun              bool
	EnableDRAAnnotation bool
}

// Result is the discovered reserved CPU set.
type Result struct {
	ReservedCPUs string
}

// Setup discovers reserved CPUs from the PerformanceProfile matching the
// given MachineConfigPool and optionally sets the DRA management annotation.
// The reserved CPU set is returned so callers can pass it to Helm.
func Setup(ctx context.Context, logger logr.Logger, clients Clients, opts SetupOptions) (Result, error) {
	if opts.NodeGroup == "" {
		return Result{}, fmt.Errorf("node group (MachineConfigPool name) is required")
	}
	if clients.API == nil {
		return Result{}, fmt.Errorf("API client is required")
	}

	isOCP, err := isOpenShift(ctx, clients.API)
	if err != nil {
		return Result{}, err
	}
	if !isOCP {
		return Result{}, fmt.Errorf("cluster is not OpenShift; aborting")
	}

	ng, err := resolveNodeGroup(ctx, clients, opts.NodeGroup)
	if err != nil {
		return Result{}, err
	}

	pp, err := findMatchingProfile(ctx, clients, ng)
	if err != nil {
		return Result{}, err
	}

	reserved, err := reservedFromProfile(pp)
	if err != nil {
		return Result{}, err
	}
	logger.Info("using reserved CPUs from PerformanceProfile", "name", pp.Name, "reservedCPUs", reserved.String())

	if opts.EnableDRAAnnotation {
		logger.Info("enabling DRA management annotation")
		if err := annotateProfile(ctx, logger, clients, pp, ng, opts.DryRun); err != nil {
			return Result{}, fmt.Errorf("annotating PerformanceProfile: %w", err)
		}
	}

	return Result{ReservedCPUs: reserved.String()}, nil
}

func isOpenShift(ctx context.Context, c client.Client) (bool, error) {
	if err := c.List(ctx, &configv1.ClusterVersionList{}); err != nil {
		if meta.IsNoMatchError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to list ClusterVersions: %w", err)
	}
	return true, nil
}

func resolveNodeGroup(ctx context.Context, clients Clients, name string) (*nodeGroup, error) {
	mcp := &mcov1.MachineConfigPool{}
	if err := clients.API.Get(ctx, client.ObjectKey{Name: name}, mcp); err != nil {
		return nil, fmt.Errorf("failed to get MachineConfigPool %q: %w", name, err)
	}
	return &nodeGroup{mcp: mcp}, nil
}

// annotateProfile sets the DRA management annotation on the given
// PerformanceProfile that was already matched to the node group.
func annotateProfile(ctx context.Context, logger logr.Logger, clients Clients, pp *performancev2.PerformanceProfile, ng *nodeGroup, dryRun bool) error {
	if annotationIsEnabled(pp) {
		logger.Info("PerformanceProfile already has DRA management annotation", "name", pp.Name)
		return nil
	}
	if dryRun {
		logger.Info("dry-run: would set DRA management annotation", "name", pp.Name, "annotation", DRAResourceManagementAnnotation)
		return nil
	}
	original := pp.DeepCopy()
	if pp.Annotations == nil {
		pp.Annotations = map[string]string{}
	}
	pp.Annotations[DRAResourceManagementAnnotation] = draResourceManagementValue
	if err := clients.API.Patch(ctx, pp, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("annotating PerformanceProfile %q: %w", pp.Name, err)
	}
	logger.Info("set DRA management annotation on PerformanceProfile", "name", pp.Name)

	if err := waitForMCPUpdate(ctx, logger, clients, ng); err != nil {
		return fmt.Errorf("waiting for MachineConfigPool %q to finish updating after annotation: %w", ng.mcp.Name, err)
	}
	return nil
}

// Waiter timeouts and polling interval. Package-level vars so tests can
// override them with short durations.
var (
	mcpUpdatingTimeout = 2 * time.Minute
	mcpUpdatedTimeout  = 30 * time.Minute
	mcpPollInterval    = 10 * time.Second
)

// waitForMCPUpdate waits for the MCP to go through Updating → Updated after
// the PerformanceProfile annotation triggers a MachineConfig change.
func waitForMCPUpdate(ctx context.Context, logger logr.Logger, clients Clients, ng *nodeGroup) error {
	mcpName := ng.mcp.Name

	// Phase 1: wait for MCP to start updating.
	logger.Info("waiting for MachineConfigPool to start updating", "name", mcpName)
	updatingCtx, cancel := context.WithTimeout(ctx, mcpUpdatingTimeout)
	defer cancel()
	err := wait.PollUntilContextCancel(updatingCtx, mcpPollInterval, true, func(ctx context.Context) (bool, error) {
		mcp := &mcov1.MachineConfigPool{}
		if err := clients.API.Get(ctx, client.ObjectKey{Name: mcpName}, mcp); err != nil {
			return false, fmt.Errorf("failed to get MachineConfigPool %q: %w", mcpName, err)
		}
		if mcpConditionStatus(mcp, mcov1.MachineConfigPoolDegraded) == corev1.ConditionTrue {
			return false, fmt.Errorf("MachineConfigPool %q is Degraded", mcpName)
		}
		if mcpConditionStatus(mcp, mcov1.MachineConfigPoolUpdating) == corev1.ConditionTrue {
			logger.Info("MachineConfigPool is Updating", "name", mcpName)
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("MachineConfigPool %q did not start updating after annotation: %w", mcpName, err)
	}

	// Phase 2: wait for MCP to finish updating.
	logger.Info("MachineConfigPool is Updating; waiting for rollout to complete", "name", mcpName)
	updatedCtx, cancel2 := context.WithTimeout(ctx, mcpUpdatedTimeout)
	defer cancel2()
	return wait.PollUntilContextCancel(updatedCtx, mcpPollInterval, true, func(ctx context.Context) (bool, error) {
		mcp := &mcov1.MachineConfigPool{}
		if err := clients.API.Get(ctx, client.ObjectKey{Name: mcpName}, mcp); err != nil {
			return false, fmt.Errorf("getting MachineConfigPool %q: %w", mcpName, err)
		}
		if mcpConditionStatus(mcp, mcov1.MachineConfigPoolDegraded) == corev1.ConditionTrue {
			return false, fmt.Errorf("MachineConfigPool %q is Degraded", mcpName)
		}
		if mcpConditionStatus(mcp, mcov1.MachineConfigPoolUpdated) == corev1.ConditionTrue &&
			mcpConditionStatus(mcp, mcov1.MachineConfigPoolUpdating) != corev1.ConditionTrue {
			logger.Info("MachineConfigPool rollout complete", "name", mcpName)
			return true, nil
		}
		return false, nil
	})
}

func mcpConditionStatus(mcp *mcov1.MachineConfigPool, condType mcov1.MachineConfigPoolConditionType) corev1.ConditionStatus {
	for i := range mcp.Status.Conditions {
		if mcp.Status.Conditions[i].Type == condType {
			return mcp.Status.Conditions[i].Status
		}
	}
	return corev1.ConditionUnknown
}

func annotationIsEnabled(pp *performancev2.PerformanceProfile) bool {
	if pp.Annotations == nil {
		return false
	}
	v, ok := pp.Annotations[DRAResourceManagementAnnotation]
	if !ok {
		return false
	}
	return strings.EqualFold(v, draResourceManagementValue) || v == "1"
}
