/*
Copyright 2022 ByteDance and its affiliates.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MatchAll = "*"
)

// UpstreamClusterSpec defines the desired state of UpstreamCluster
type UpstreamClusterSpec struct {
	// Servers contains a group of upstream api servers
	Servers []UpstreamClusterServer `json:"servers,omitempty" protobuf:"bytes,1,rep,name=servers"`

	// Client connection config for upstream api servers
	ClientConfig ClientConfig `json:"clientConfig,omitempty" protobuf:"bytes,2,opt,name=clientConfig"`

	// Secures serving config for upstream cluster
	SecureServing SecureServing `json:"secureServing,omitempty" protobuf:"bytes,3,opt,name=secureServing"`

	// Client flow control settings, e.g. qps and burst
	FlowControl FlowControl `json:"flowControl,omitempty" protobuf:"bytes,4,opt,name=flowControl"`

	// DispatchPolicies describes how to dispatch requests to upsteams.
	// Only one dispatch policy will be matched
	// The router will follow the order of DispatchPolicies, that means
	// the previous policy has higher priority
	DispatchPolicies []DispatchPolicy `json:"dispatchPolicies,omitempty" protobuf:"bytes,5,rep,name=dispatchPolicies"`

	// Logging config for upstream cluster
	//
	// There are three places to control the log switch
	// 1. flag --enable-proxy-access-log: If it is false, all proxy access log
	//    will be disabled.
	// 2. log mode in upstream, it allows you control cluster level log switch. If it
	//    is off, all access logs of requests to this cluster will be disabled.
	// 3. log mode in dispatchPolicy, it allows you control policy level log switch.
	//    If it is off, all access logs of requests matching this policy will be disabled.
	Logging LoggingConfig `json:"logging,omitempty" protobuf:"bytes,6,opt,name=logging"`
}

type LogMode string

const (
	// LogOn enable access log
	LogOn LogMode = "on"
	// LogOff disable access log
	LogOff LogMode = "off"
)

type LoggingConfig struct {
	// upstream cluster level log mode
	// - if set to off, all access logs of requests to this cluster will be disabled.
	// - if set to on, access logs of requests to this cluster will be enabled. But it
	//   can be override by dispatchPolicy.LogMode
	// - if unset, the logging is controlled by dispatchPolicy.LogMode
	Mode LogMode `json:"mode,omitempty" protobuf:"bytes,1,opt,name=mode,casttype=LogMode"`
}

type SecureServing struct {
	// KeyData contains PEM-encoded data from a client key file for TLS.
	// The serialized form of data is a base64 encoded string
	KeyData []byte `json:"keyData,omitempty" protobuf:"bytes,1,opt,name=keyData"`
	// CertData contains PEM-encoded data from a client cert file for TLS.
	// The serialized form of data is a base64 encoded string
	CertData []byte `json:"certData,omitempty" protobuf:"bytes,2,opt,name=certData"`
	// ClientCAData contains PEM-encoded data from a ca file for TLS.
	// The serialized form of data is a base64 encoded string
	ClientCAData []byte `json:"clientCAData,omitempty" protobuf:"bytes,3,opt,name=clientCAData"`
}

type ClientConfig struct {
	// Server should be accessed without verifying the TLS certificate. For testing only.
	Insecure bool `json:"insecure,omitempty" protobuf:"varint,1,opt,name=insecure"`
	// BearerToken is the bearer token for authentication. It can be an service account token.
	BearerToken []byte `json:"bearerToken,omitempty" protobuf:"bytes,2,opt,name=bearerToken"`
	// KeyData contains PEM-encoded data from a client key file for TLS.
	// The serialized form of data is a base64 encoded string
	KeyData []byte `json:"keyData,omitempty" protobuf:"bytes,3,opt,name=keyData"`
	// CertData contains PEM-encoded data from a client cert file for TLS.
	// The serialized form of data is a base64 encoded string
	CertData []byte `json:"certData,omitempty" protobuf:"bytes,4,opt,name=certData"`
	// CAData contains PEM-encoded data from a ca file for TLS.
	// The serialized form of data is a base64 encoded string
	CAData []byte `json:"caData,omitempty" protobuf:"bytes,5,opt,name=caData"`
	// QPS indicates the maximum QPS to the master from this client.
	// Zero means no limit, it is different from qps defined in flowcontrol.RateLimiter
	// +optional
	QPS int32 `json:"qps,omitempty" protobuf:"varint,6,opt,name=qps"`
	// Maximum burst for throttle.
	// Zero means no limit
	// This value must be bigger than QPS if QPS is not 0
	// +optional
	Burst int32 `json:"burst,omitempty" protobuf:"varint,7,opt,name=burst"`
	// QPSDivisor divites QPS to a new float32 qps value because we can not use float32
	// type in API Struct according to API convention.
	// It allows you to set a more precise qps, like 0.01 (qps:1, qpsDivisor:100)
	// +optional
	QPSDivisor int32 `json:"qpsDivisor,omitempty" protobuf:"varint,8,opt,name=qpsDivisor"`
}

type FlowControl struct {
	Schemas []FlowControlSchema `json:"flowControlSchemas,omitempty" protobuf:"bytes,1,rep,name=flowControlSchemas"`
}

type FlowControlSchema struct {
	// Schema name
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	// Schema config
	FlowControlSchemaConfiguration `json:",inline" protobuf:"bytes,2,opt,name=flowControlSchemaConfiguration"`
}

// Represents the configuration of flow control schema
// Only one of its members may be specified
type FlowControlSchemaConfiguration struct {
	// Exempt represents no limits on a flow.
	// +optianal
	Exempt *ExemptFlowControlSchema `json:"exempt,omitempty" protobuf:"bytes,1,opt,name=exempt"`
	// MaxRequestsInflight represents a maximum concurrent number of requests
	// in flight at a given time.
	// +optianal
	MaxRequestsInflight *MaxRequestsInflightFlowControlSchema `json:"maxRequestsInflight,omitempty" protobuf:"bytes,2,opt,name=maxRequestsInflight"`
	// TokenBucket represents a token bucket approach. The rate limiter allows bursts
	// of up to 'burst' to exceed the QPS, while still maintaining a smoothed qps
	// rate of 'qps'.
	// +optianal
	TokenBucket *TokenBucketFlowControlSchema `json:"tokenBucket,omitempty" protobuf:"bytes,3,opt,name=tokenBucket"`
}

// Represents flow control schema type
type FlowControlSchemaType string

const (
	Unknown             FlowControlSchemaType = "Unknown"
	Exempt              FlowControlSchemaType = "Exempt"
	MaxRequestsInflight FlowControlSchemaType = "MaxRequestsInflight"
	TokenBucket         FlowControlSchemaType = "TokenBucket"
)

// Represents no limit flow control.
type ExemptFlowControlSchema struct {
}

// Represents a maximum concurrent number of requests in flight at a given time.
type MaxRequestsInflightFlowControlSchema struct {
	// maximum concurrent number of requests
	Max int32 `json:"max,omitempty" protobuf:"varint,1,opt,name=max"`
}

// Represents token bucket rate limit approach.
type TokenBucketFlowControlSchema struct {
	// QPS indicates the maximum QPS to the master from this client.
	// It can not be zero
	QPS int32 `json:"qps,omitempty" protobuf:"varint,1,opt,name=qps"`
	// Maximum burst for throttle.
	// This value must be bigger than QPS if QPS is not 0
	// +optional
	Burst int32 `json:"burst,omitempty" protobuf:"varint,2,opt,name=burst"`
}

type SecretReferecence struct {
	// `namespace` is the namespace of the secret.
	// Required
	Namespace string `json:"namespace" protobuf:"bytes,1,opt,name=namespace"`
	// `name` is the name of the secret.
	// Required
	Name string `json:"name" protobuf:"bytes,2,opt,name=name"`
}

type UpstreamClusterServer struct {
	// Endpoint is the backend api server address, like https://apiserver.com:6443
	Endpoint string `json:"endpoint,omitempty" protobuf:"bytes,1,opt,name=endpoint"`
	// Disabled marks the server as permanently unavailable.
	// +optional
	Disabled *bool `json:"disabled,omitempty" protobuf:"varint,2,opt,name=disabled"`
}

type DispatchPolicy struct {
	// Specifies a load balancing method for a server group
	Strategy Strategy `json:"strategy,omitempty" protobuf:"bytes,1,opt,name=strategy,casttype=Strategy"`

	// UpsteamSubset indacates to the list of upstream endpoints. An empty set
	// means use all upstreams
	// +optional
	UpsteamSubset []string `json:"upsteamSubset,omitempty" protobuf:"bytes,2,rep,name=upsteamSubset"`

	// Rules holds all the DispatchPolicyRules for this policy
	// Gateway matches rules according to the list order, the previous rules
	// take higher precedence
	// +optional
	Rules []DispatchPolicyRule `json:"rules,omitempty" protobuf:"bytes,3,rep,name=rules"`

	// FlowControlSchemaName indicates to which flow control schema in spec.FlowControl will
	// take effect on this prolicy
	// If not set, there is no limit
	// +optional
	FlowControlSchemaName string `json:"flowControlSchemaName,omitempty" protobuf:"bytes,4,opt,name=flowControlSchemaName"`

	// dispatch policy level access log mode
	// - if set to off, all access logs of requests matching this policy will be disabled.
	// - if set to on, access logs will be enabled when spec.Logging.Mode is "on" or ""
	// - if set to unset, the logging is controlled by spec.Logging.Mode
	// +optional
	LogMode LogMode `json:"logMode,omitempty" protobuf:"bytes,5,opt,name=logMode,casttype=LogMode"`
}

type Strategy string

const (
	RoundRobin Strategy = "RoundRobin"
)

// DispatchPolicyRule holds information that describes a policy rule
type DispatchPolicyRule struct {
	// Verbs is a list of Verbs that apply to ALL the ResourceKinds and AttributeRestrictions contained in this rule.
	// - "*" represents all Verbs.
	// - An empty set mains that nothing is allowed.
	// - use '-' prefix to invert verbs matching, e.g. "-get" means match all verbs except "get"
	Verbs []string `json:"verbs,omitempty" protobuf:"bytes,1,rep,name=verbs"`

	// APIGroups is the name of the APIGroup that contains the resources.  If multiple API groups are specified, any action requested against one of
	// the enumerated resources in any API group will be allowed.
	// - "*" represents all APIGroups.
	// - An empty set mains that nothing is allowed.
	// - use '-' prefix to invert apiGroups matching, e.g. "-apps" means match all apiGroups except "apps"
	APIGroups []string `json:"apiGroups,omitempty" protobuf:"bytes,2,rep,name=apiGroups"`

	// Resources is a list of resources this rule applies to.
	// - "*" represents all Resources.
	// - An empty set mains that nothing is allowed.
	// - use "{resource}/{subresource}" to match one resource's subresource
	// - use "*/{subresource}" to match all resources' subresource, but "{resource}/*" is not allowed.
	// - use '-' prefix to invert resources matching, e.g. "-deployments" means match all resources except "deployments".
	Resources []string `json:"resources,omitempty" protobuf:"bytes,3,rep,name=resources"`

	// ResourceNames is an optional white list of names that the rule applies to.
	// - "*" represents all ResourceNames.
	// - An empty set means that everything is allowed.
	// - use '-' prefix to invert resourceNames matching, e.g. "-nginx" means match all resourceNames except "nginx"
	// +optional
	ResourceNames []string `json:"resourceNames,omitempty" protobuf:"bytes,4,rep,name=resourceNames"`

	// Users is a list of users this rule applies to.
	// - "*" represents all Users.
	// - if ServiceAccounts is empty, an empty set means that everything is allowed, otherwise it means nothing is allowed.
	// use '-' prefix to invert users matching, e.g. "-admin" means match all users except "admin"
	// +optional
	Users []string `json:"users,omitempty" protobuf:"bytes,5,rep,name=users"`

	// ServiceAccounts is a list of serviceAccont users this rule applies to. It can be covered if Users matches all.
	// - ServiceAccounts can not use invert matching
	// - if Users is empty, an empty set mains that nothing is allowed, otherwise it means nothing is allowed.
	// - serviceAccount name and namespace must be set
	// +optional
	ServiceAccounts []ServiceAccountRef `json:"serviceAccounts,omitempty" protobuf:"bytes,6,rep,name=serviceAccounts"`

	// UserGroups is a list of users groups this rule applies to. UserGroupAll represents all user groups.
	// - "*" represents all UserGroups.
	// - An empty set means that everything is allowed.
	// - use '-' prefix to invert userGroups matching, e.g. "-system:controllers" means match all userGroups except "system:controllers"
	// +optional
	UserGroups []string `json:"userGroups,omitempty" protobuf:"bytes,7,rep,name=userGroups"`

	// NonResourceURLs is a set of partial urls that a user should have access to.  *s are allowed, but only as the full, final step in the path
	// Since non-resource URLs are not namespaced, this field is only applicable for ClusterRoles referenced from a ClusterRoleBinding.
	// Rules can either apply to API resources (such as "pods"or "secrets") or non-resource URL paths (such as "/api"),  but not both.
	// - "*" represents all NonResourceURLs.
	// - An empty set mains that nothing is allowed.
	// - NonResourceURLs can not use invert matching
	NonResourceURLs []string `json:"nonResourceURLs,omitempty" protobuf:"bytes,8,rep,name=nonResourceURLs"`
}

type ServiceAccountRef struct {
	Name      string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,2,opt,name=namespace"`
}

// UpstreamClusterStatus defines the observed state of UpstreamCluster
type UpstreamClusterStatus struct {
}

// +genclient
// +genclient:nonNamespaced
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// UpstreamCluster is the Schema for the upstreamclusters API
type UpstreamCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   UpstreamClusterSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status UpstreamClusterStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// UpstreamClusterList contains a list of UpstreamCluster
type UpstreamClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []UpstreamCluster `json:"items" protobuf:"bytes,2,rep,name=items"`
}
