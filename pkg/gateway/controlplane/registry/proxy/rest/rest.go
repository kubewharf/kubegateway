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

package rest

import (
	"github.com/kubewharf/apiserver-runtime/pkg/registry"
	runtimeschema "github.com/kubewharf/apiserver-runtime/pkg/schema"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/master"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
)

func NewRESTStorageProvider(scheme *runtime.Scheme, factory *registry.RESTStorageOptionsFactory) (master.RESTStorageProvider, error) {
	group := proxyv1alpha1.SchemeGroupVersion.Group

	upstreamClusterOption, err := newUpstreamClusterOption(factory)
	if err != nil {
		return nil, err
	}
	rateLimitStatus, err := newRateLimitStatusOption(factory)
	if err != nil {
		return nil, err
	}

	return registry.NewRESTStorageProvider(scheme, group, upstreamClusterOption, rateLimitStatus), nil
}

func newUpstreamClusterOption(factory *registry.RESTStorageOptionsFactory) (registry.RESTStorageOptions, error) {
	gvkr := runtimeschema.GroupVersionKindResource{
		Group:    proxyv1alpha1.SchemeGroupVersion.Group,
		Version:  proxyv1alpha1.SchemeGroupVersion.Version,
		Kind:     "UpstreamCluster",
		Resource: "upstreamclusters",
	}
	factory.SetHubGroupVersion(gvkr, gvkr.GroupVersion())
	factory.SetRESTStrategy(gvkr, registry.ClusterScopeStorageStrategySingleton)

	options, err := factory.GetRESTStorageOptions(gvkr)
	if err != nil {
		return registry.RESTStorageOptions{}, err
	}
	options.SubStatus = true
	return options, nil
}

func newRateLimitStatusOption(factory *registry.RESTStorageOptionsFactory) (registry.RESTStorageOptions, error) {
	gvkr := runtimeschema.GroupVersionKindResource{
		Group:    proxyv1alpha1.SchemeGroupVersion.Group,
		Version:  proxyv1alpha1.SchemeGroupVersion.Version,
		Kind:     "RateLimitCondition",
		Resource: "ratelimitconditions",
	}
	factory.SetHubGroupVersion(gvkr, gvkr.GroupVersion())
	factory.SetRESTStrategy(gvkr, registry.NewDefaultRESTStrategy(false, false))

	options, err := factory.GetRESTStorageOptions(gvkr)
	if err != nil {
		return registry.RESTStorageOptions{}, err
	}
	return options, nil
}

func NewRESTStorageProviderOrDie(scheme *runtime.Scheme, factory *registry.RESTStorageOptionsFactory) master.RESTStorageProvider {
	ret, err := NewRESTStorageProvider(scheme, factory)
	if err != nil {
		panic(err)
	}
	return ret
}
