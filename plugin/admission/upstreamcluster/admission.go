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
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/admission"
	genericadmissioninitializer "k8s.io/apiserver/pkg/admission/initializer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1/validation"
	gatewayinformers "github.com/kubewharf/kubegateway/pkg/client/informers"
	gatewayclientset "github.com/kubewharf/kubegateway/pkg/client/kubernetes"
	proxylisters "github.com/kubewharf/kubegateway/pkg/client/listers/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/clusters/features"
	"github.com/kubewharf/kubegateway/pkg/gateway/controlplane/admission/initializer"
)

var _ admission.Interface = &upstreamclusterPlugin{}
var _ admission.MutationInterface = &upstreamclusterPlugin{}
var _ admission.ValidationInterface = &upstreamclusterPlugin{}
var _ genericadmissioninitializer.WantsExternalKubeInformerFactory = &upstreamclusterPlugin{}
var _ genericadmissioninitializer.WantsExternalKubeClientSet = &upstreamclusterPlugin{}
var _ initializer.WantsGatewayResourceClientSet = &upstreamclusterPlugin{}
var _ initializer.WantsGatewayResourceInformerFactory = &upstreamclusterPlugin{}

func NewUpstreamClusterPlugin() admission.Interface {
	return &upstreamclusterPlugin{
		Handler: admission.NewHandler(admission.Create, admission.Update),
	}
}

type upstreamclusterPlugin struct {
	*admission.Handler
	lister proxylisters.UpstreamClusterLister
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

	// we need to wait for our caches to warm
	if !p.WaitForReady() {
		return admission.NewForbidden(a, fmt.Errorf("not yet ready to validate request"))
	}

	// check conflict

	clusterName := strings.ToLower(cluster.Name)
	clusters, err := p.lister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list upstream clusters %v", err)
	}
	for _, upstreamCluster := range clusters {
		if strings.ToLower(upstreamCluster.Name) == clusterName {
			continue
		}
		for _, s := range append([]string{upstreamCluster.Name}, upstreamCluster.Spec.SecureServing.ServerNames...) {
			if strings.ToLower(clusterName) == strings.ToLower(s) {
				allErrs = append(allErrs, field.Invalid(field.NewPath("metadata").Child("name"), clusterName, fmt.Sprintf("conflict with cluster %s %v", upstreamCluster.Name, upstreamCluster.Spec.SecureServing.ServerNames)))
			}

			for _, serverName := range cluster.Spec.SecureServing.ServerNames {
				if strings.ToLower(serverName) == strings.ToLower(s) {
					allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("secureServing").Child("serverNames"), cluster.Spec.SecureServing.ServerNames, fmt.Sprintf("conflict with cluster %s %v", upstreamCluster.Name, upstreamCluster.Spec.SecureServing.ServerNames)))
				}
			}
		}
	}

	return allErrs.ToAggregate()
}

func (p *upstreamclusterPlugin) SetExternalKubeInformerFactory(informers.SharedInformerFactory) {}

func (p *upstreamclusterPlugin) SetExternalKubeClientSet(kubernetes.Interface) {}

func (p *upstreamclusterPlugin) SetGatewayResourceInformerFactory(factory gatewayinformers.SharedInformerFactory) {
	informer := factory.Proxy().V1alpha1().UpstreamClusters()
	p.lister = informer.Lister()
	p.SetReadyFunc(informer.Informer().HasSynced)
}

func (p *upstreamclusterPlugin) SetGatewayResourceClientSet(g gatewayclientset.Interface) {}

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
