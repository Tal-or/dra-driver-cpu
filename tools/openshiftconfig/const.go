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

import performancev2 "github.com/openshift/cluster-node-tuning-operator/pkg/apis/performanceprofile/v2"

const draResourceManagementValue = "true"

// DRAResourceManagementAnnotation tells the Node Tuning Operator to disable
// kubelet CPU/topology/memory managers that conflict with DRA.
const DRAResourceManagementAnnotation = performancev2.PerformanceProfileDRAResourceManagementAnnotation
