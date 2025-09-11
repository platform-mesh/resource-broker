/*
Copyright 2025.
SPDX-License-Identifier: Apache-2.0

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

// The main package running the resource-broker.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	ctrl "sigs.k8s.io/controller-runtime"

	mctrl "sigs.k8s.io/multicluster-runtime"
	"sigs.k8s.io/multicluster-runtime/providers/file"

	"github.com/platform-mesh/resource-broker/pkg/manager"
	"github.com/platform-mesh/resource-broker/pkg/wrapprovider"
)

var (
	fSourceKubeconfig = flag.String(
		"source-kubeconfig",
		"",
		"Path(s) to the kubeconfig file for the source clusters. If not set, in-cluster config will be used.",
	)
	fTargetKubeconfig = flag.String(
		"target-kubeconfig",
		"",
		"Path(s) to the kubeconfig file for the target clusters. If not set, in-cluster config will be used.",
	)
	fGroup   = flag.String("group", "", "Group to watch")
	fVersion = flag.String("version", "", "Version to watch")
	fKind    = flag.String("kind", "", "Kind to watch")
)

func main() {
	flag.Parse()
	ctx := mctrl.SetupSignalHandler()
	if err := doMain(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "problem running main: %v\n", err)
		os.Exit(1)
	}
}

func doMain(ctx context.Context) error {
	local, err := ctrl.GetConfig()
	if err != nil {
		return err
	}

	source, err := file.New(file.Options{
		KubeconfigFiles: strings.Split(*fSourceKubeconfig, ","),
	})
	if err != nil {
		return err
	}

	target, err := file.New(file.Options{
		KubeconfigFiles: strings.Split(*fTargetKubeconfig, ","),
	})
	if err != nil {
		return err
	}

	return manager.Start(
		ctx,
		local,
		wrapprovider.Wrap(source, source.Run),
		wrapprovider.Wrap(target, target.Run),
		schema.GroupVersionKind{
			Group:   *fGroup,
			Version: *fVersion,
			Kind:    *fKind,
		},
	)
}
