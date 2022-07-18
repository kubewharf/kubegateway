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

package scheme

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	admissioninstall "k8s.io/kubernetes/pkg/apis/admission/install"
	admissionregistrationinstall "k8s.io/kubernetes/pkg/apis/admissionregistration/install"
	appsinstall "k8s.io/kubernetes/pkg/apis/apps/install"
	auditregistrationinstall "k8s.io/kubernetes/pkg/apis/auditregistration/install"
	authenticationinstall "k8s.io/kubernetes/pkg/apis/authentication/install"
	authorizationinstall "k8s.io/kubernetes/pkg/apis/authorization/install"
	autoscalinginstall "k8s.io/kubernetes/pkg/apis/autoscaling/install"
	batchinstall "k8s.io/kubernetes/pkg/apis/batch/install"
	certificatesinstall "k8s.io/kubernetes/pkg/apis/certificates/install"
	coordinationinstall "k8s.io/kubernetes/pkg/apis/coordination/install"
	coreinstall "k8s.io/kubernetes/pkg/apis/core/install"
	discoveryinstall "k8s.io/kubernetes/pkg/apis/discovery/install"
	eventsinstall "k8s.io/kubernetes/pkg/apis/events/install"
	extensionsinstall "k8s.io/kubernetes/pkg/apis/extensions/install"
	flowcontrolinstall "k8s.io/kubernetes/pkg/apis/flowcontrol/install"
	imagepolicyinstall "k8s.io/kubernetes/pkg/apis/imagepolicy/install"
	networkinginstall "k8s.io/kubernetes/pkg/apis/networking/install"
	nodeinstall "k8s.io/kubernetes/pkg/apis/node/install"
	policyinstall "k8s.io/kubernetes/pkg/apis/policy/install"
	rbacinstall "k8s.io/kubernetes/pkg/apis/rbac/install"
	schedulinginstall "k8s.io/kubernetes/pkg/apis/scheduling/install"
	settingsinstall "k8s.io/kubernetes/pkg/apis/settings/install"
	storageinstall "k8s.io/kubernetes/pkg/apis/storage/install"
)

var (
	// Scheme is the default instance of runtime.Scheme to which types in the Kubernetes API are already registered.
	Scheme = runtime.NewScheme()

	// Codecs provides access to encoding and decoding for the scheme
	Codecs = serializer.NewCodecFactory(Scheme)

	// ParameterCodec handles versioning of objects that are converted to query parameters.
	ParameterCodec = runtime.NewParameterCodec(Scheme)

	AddToScheme = localSchemeBuilder.AddToScheme
	Install     = AddToScheme

	localSchemeBuilder = runtime.SchemeBuilder{
		localInstall(admissioninstall.Install),
		localInstall(admissionregistrationinstall.Install),
		localInstall(appsinstall.Install),
		localInstall(auditregistrationinstall.Install),
		localInstall(authenticationinstall.Install),
		localInstall(authorizationinstall.Install),
		localInstall(autoscalinginstall.Install),
		localInstall(batchinstall.Install),
		localInstall(certificatesinstall.Install),
		localInstall(coordinationinstall.Install),
		localInstall(coreinstall.Install),
		localInstall(discoveryinstall.Install),
		localInstall(eventsinstall.Install),
		localInstall(extensionsinstall.Install),
		localInstall(flowcontrolinstall.Install),
		localInstall(imagepolicyinstall.Install),
		localInstall(networkinginstall.Install),
		localInstall(nodeinstall.Install),
		localInstall(policyinstall.Install),
		localInstall(rbacinstall.Install),
		localInstall(schedulinginstall.Install),
		localInstall(settingsinstall.Install),
		localInstall(storageinstall.Install),
	}
)

func localInstall(install func(*runtime.Scheme)) func(*runtime.Scheme) error {
	return func(s *runtime.Scheme) error {
		install(s)
		return nil
	}
}

func init() {
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})
	utilruntime.Must(AddToScheme(Scheme))
}
