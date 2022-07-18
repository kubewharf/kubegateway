// Copyright 2022 ByteDance and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package options contains flags and options for initializing an apiserver
package options

import (
	cliflag "k8s.io/component-base/cli/flag"
	_ "k8s.io/kubernetes/pkg/features" // add the kubernetes feature gates

	controleplaneoptions "github.com/kubewharf/kubegateway/pkg/gateway/controlplane/options"
	"github.com/kubewharf/kubegateway/plugin/admission/install"
)

// ControlPlaneServerRunOptions runs a kubernetes api server.
type ControlPlaneServerRunOptions struct {
	*controleplaneoptions.ControlPlaneOptions

	MaxConnectionBytesPerSec    int64
	ShowHiddenMetricsForVersion string
}

// NewControlPlaneServerRunOptions creates a new ServerRunOptions object with default parameters
func NewControlPlaneServerRunOptions() *ControlPlaneServerRunOptions {
	s := ControlPlaneServerRunOptions{
		ControlPlaneOptions: controleplaneoptions.NewControlPlaneOptions(),
	}

	// TODO: use pluginable way
	for name, f := range install.AdmissionPlugins {
		s.Admission.Plugins.Register(name, f)
		s.Admission.EnablePlugins = append(s.Admission.EnablePlugins, name)
		s.Admission.RecommendedPluginOrder = append(s.Admission.RecommendedPluginOrder, name)
	}

	// TODO:
	// Overwrite the default for storage data format.
	s.Etcd.DefaultStorageMediaType = "application/vnd.kubernetes.protobuf"
	return &s
}

// Flags returns flags for a specific APIServer by section name
func (s *ControlPlaneServerRunOptions) Flags() (fss cliflag.NamedFlagSets) {
	// Add the generic flags.
	fss = s.ControlPlaneOptions.Flags()

	// TODO(RainbowMango): move it to genericoptions before next flag comes.
	mfs := fss.FlagSet("metrics")
	mfs.StringVar(&s.ShowHiddenMetricsForVersion, "show-hidden-metrics-for-version", s.ShowHiddenMetricsForVersion,
		"The previous version for which you want to show hidden metrics. "+
			"Only the previous minor version is meaningful, other values will not be allowed. "+
			"The format is <major>.<minor>, e.g.: '1.16'. "+
			"The purpose of this format is make sure you have the opportunity to notice if the next release hides additional metrics, "+
			"rather than being surprised when they are permanently removed in the release after that.")

	// Note: the weird ""+ in below lines seems to be the only way to get gofmt to
	// arrange these text blocks sensibly. Grrr.
	fs := fss.FlagSet("misc")
	fs.Int64Var(&s.MaxConnectionBytesPerSec, "max-connection-bytes-per-sec", s.MaxConnectionBytesPerSec, ""+
		"If non-zero, throttle each user connection to this number of bytes/sec. "+
		"Currently only applies to long-running requests.")
	return fss
}

func (s *ControlPlaneServerRunOptions) Complete() error {
	if err := s.ControlPlaneOptions.Complete(); err != nil {
		return err
	}
	return nil
}
