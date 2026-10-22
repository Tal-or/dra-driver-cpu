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
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Options configures the openshift-config command.
type Options struct {
	Logger     logr.Logger
	Stdout     io.Writer
	Stderr     io.Writer
	NewClients func(kubeconfig string) (Clients, error)
}

// Run discovers OpenShift reserved CPUs, annotates the matching
// PerformanceProfile for DRA management, and prints the reserved CPU set
// to stdout so callers can pass it to Helm.
func Run(args []string, opts Options) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	fs := flag.NewFlagSet("openshift-config", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [flags]\n\nDiscover reserved CPUs from an OpenShift KubeletConfig, annotate the matching PerformanceProfile for DRA management, and print the reserved CPU set.\n\nFlags:\n", fs.Name())
		fs.PrintDefaults()
	}

	kubeconfig := fs.String("kubeconfig", "", "Path to the kubeconfig file (required).")
	nodeGroup := fs.String("node-group", "", "MachineConfigPool name to scope discovery (required).")
	dryRun := fs.Bool("dry-run", false, "Discover reserved CPUs without writing the annotation.")
	enableDRAAnnotation := fs.Bool("enable-dra-annotation", false, "Enable DRA management annotation on the matching PerformanceProfile.")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("openshift-config does not accept positional arguments")
	}
	if *kubeconfig == "" {
		return fmt.Errorf("--kubeconfig is required")
	}

	newClients := opts.NewClients
	if newClients == nil {
		newClients = clientsFromKubeconfig
	}
	clients, err := newClients(*kubeconfig)
	if err != nil {
		return err
	}

	result, err := Setup(context.Background(), opts.Logger, clients, SetupOptions{
		NodeGroup:           *nodeGroup,
		DryRun:              *dryRun,
		EnableDRAAnnotation: *enableDRAAnnotation,
	})
	if err != nil {
		return err
	}

	opts.Logger.Info("discovered reserved CPUs",
		"reservedCPUs", result.ReservedCPUs,
		"dryRun", *dryRun,
	)
	_, err = fmt.Fprintln(stdout, result.ReservedCPUs)
	return err
}

func clientsFromKubeconfig(kubeconfigPath string) (Clients, error) {
	cfg, err := restConfig(kubeconfigPath)
	if err != nil {
		return Clients{}, err
	}
	scheme, err := newScheme()
	if err != nil {
		return Clients{}, fmt.Errorf("building scheme: %w", err)
	}
	api, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return Clients{}, fmt.Errorf("creating API client: %w", err)
	}
	return Clients{API: api}, nil
}

func restConfig(explicitPath string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if explicitPath != "" {
		rules.ExplicitPath = explicitPath
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("cannot load kubeconfig: %w", err)
	}
	return cfg, nil
}
