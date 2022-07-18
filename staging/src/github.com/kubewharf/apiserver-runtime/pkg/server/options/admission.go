// Copyright 2022 ByteDance and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package options

import (
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/admission"
	admissionmetrics "k8s.io/apiserver/pkg/admission/metrics"
	"k8s.io/apiserver/pkg/admission/plugin/namespace/lifecycle"
	genericoptions "k8s.io/apiserver/pkg/server/options"
)

//
func NewAdmissionOptions() *genericoptions.AdmissionOptions {
	options := &genericoptions.AdmissionOptions{
		Plugins:                admission.NewPlugins(),
		Decorators:             admission.Decorators{admission.DecoratorFunc(admissionmetrics.WithControllerMetrics)},
		RecommendedPluginOrder: []string{lifecycle.PluginName},
		DefaultOffPlugins:      sets.NewString(),
	}

	RegisterAllAdmissionPlugins(options.Plugins)
	return options
}

func RegisterAllAdmissionPlugins(plugins *admission.Plugins) {
	lifecycle.Register(plugins)
}

// enabledPluginNames makes use of RecommendedPluginOrder, DefaultOffPlugins,
// EnablePlugins, DisablePlugins fields
// to prepare a list of ordered plugin names that are enabled.
func enabledPluginNames(a *genericoptions.AdmissionOptions) []string {
	allOffPlugins := append(a.DefaultOffPlugins.List(), a.DisablePlugins...)
	disabledPlugins := sets.NewString(allOffPlugins...)
	enabledPlugins := sets.NewString(a.EnablePlugins...)
	disabledPlugins = disabledPlugins.Difference(enabledPlugins)

	orderedPlugins := []string{}
	for _, plugin := range a.RecommendedPluginOrder {
		if !disabledPlugins.Has(plugin) {
			orderedPlugins = append(orderedPlugins, plugin)
		}
	}

	return orderedPlugins
}
