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

package upstreamclusteradmission

import (
	"context"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/admission"
	genericadmissioninitializer "k8s.io/apiserver/pkg/admission/initializer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1/validation"
	"github.com/kubewharf/kubegateway/pkg/clusters/features"
)

var _ admission.Interface = &upstreamclusterPlugin{}
var _ admission.MutationInterface = &upstreamclusterPlugin{}
var _ admission.ValidationInterface = &upstreamclusterPlugin{}
var _ genericadmissioninitializer.WantsExternalKubeInformerFactory = &upstreamclusterPlugin{}
var _ genericadmissioninitializer.WantsExternalKubeClientSet = &upstreamclusterPlugin{}

func NewUpstreamClusterPlugin() admission.Interface {
	return &upstreamclusterPlugin{
		Handler: admission.NewHandler(admission.Create, admission.Update),
	}
}

type upstreamclusterPlugin struct {
	*admission.Handler
}

func (p *upstreamclusterPlugin) ValidateInitialization() error {
	return nil
}

func (p *upstreamclusterPlugin) Admit(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error {
	if shouldIgnore(a) {
		return nil
	}
	cluster := a.GetObject().(*proxyv1alpha1.UpstreamCluster)

	// set default
	defaulter := o.GetObjectDefaulter()
	defaulter.Default(cluster)

	for i := range cluster.Spec.DispatchPolicies {
		for j := range cluster.Spec.DispatchPolicies[i].Rules {
			cluster.Spec.DispatchPolicies[i].Rules[j] = normalizeRules(cluster.Spec.DispatchPolicies[i].Rules[j])
		}
	}

	return nil
}

func (p *upstreamclusterPlugin) Validate(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error {
	if shouldIgnore(a) {
		return nil
	}
	cluster := a.GetObject().(*proxyv1alpha1.UpstreamCluster)
	allErrs := validation.ValidateUpstreamCluster(cluster)

	if cluster.Annotations != nil {
		featuregate := cluster.Annotations[features.FeatureGateAnnotationKey]
		if len(featuregate) > 0 {
			copy := features.DefaultMutableFeatureGate.DeepCopy()
			if err := copy.Set(featuregate); err != nil {
				allErrs = append(allErrs, field.Invalid(field.NewPath("metadata").Child("annotations").Key(features.FeatureGateAnnotationKey), featuregate, err.Error()))
			}
		}
	}

	return allErrs.ToAggregate()
}

func (p *upstreamclusterPlugin) SetExternalKubeInformerFactory(informers.SharedInformerFactory) {}

func (p *upstreamclusterPlugin) SetExternalKubeClientSet(kubernetes.Interface) {}

func shouldIgnore(a admission.Attributes) bool {
	if a.GetResource().GroupResource() != proxyv1alpha1.Resource("upstreamclusters") {
		return true
	}
	if a.GetSubresource() != "" {
		return true
	}
	obj := a.GetObject()
	if obj == nil {
		return true
	}
	_, ok := obj.(*proxyv1alpha1.UpstreamCluster)
	return !ok
}

func normalizeRules(in proxyv1alpha1.DispatchPolicyRule) proxyv1alpha1.DispatchPolicyRule {
	return proxyv1alpha1.DispatchPolicyRule{
		Verbs:           filterRules(in.Verbs),
		APIGroups:       filterRules(in.APIGroups),
		Resources:       filterRules(in.Resources),
		ResourceNames:   filterRules(in.ResourceNames),
		Users:           filterRules(in.Users),
		ServiceAccounts: in.ServiceAccounts,
		UserGroups:      filterRules(in.UserGroups),
		NonResourceURLs: filterRules(in.NonResourceURLs),
	}
}

func filterRules(rules []string) (filtered []string) {
	reversed := []string{}
	matchAll := false
	for _, r := range rules {
		if r == "*" {
			matchAll = true
			break
		}
		// legecy group will be ""
		if len(r) > 0 && r[0] == '-' {
			reversed = append(reversed, r)
		} else {
			filtered = append(filtered, r)
		}
	}

	if matchAll {
		return []string{"*"}
	}
	if len(filtered) > 0 {
		// if filtered is not empty drop reversed
		return
	}
	filtered = reversed
	return
}
