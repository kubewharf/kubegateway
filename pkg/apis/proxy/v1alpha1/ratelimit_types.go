package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Cluster

// RateLimitCondition is the Schema for the upstreamclusters API
type RateLimitCondition struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   RateLimitSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status RateLimitStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// RateLimitConditionList contains a list of RateLimitCondition
type RateLimitConditionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []RateLimitCondition `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// RateLimitSpec defines the expect rate limit state of UpstreamCluster
type RateLimitSpec struct {
	// All rate limit requests must specify a upstreamCluster.
	UpstreamCluster string `json:"upstreamCluster,omitempty" protobuf:"bytes,1,opt,name=upstreamCluster"`

	// Rate limit client instance identity
	Instance string `json:"instance,omitempty" protobuf:"bytes,2,opt,name=instance"`

	// Client flow control settings, e.g. qps and burst
	LimitItemConfigurations []RateLimitItemConfiguration `json:"limitItemConfigurations,omitempty" protobuf:"bytes,3,opt,name=limitItemConfigurations"`
}

// RateLimitStatus defines the rate limit state of UpstreamCluster
type RateLimitStatus struct {
	// Client flow control settings, e.g. qps and burst
	LimitItemStatuses []RateLimitItemStatus `json:"limitItemStatuses,omitempty" protobuf:"bytes,1,opt,name=limitItemStatuses"`
}

type RateLimitItemConfiguration struct {
	// Schema name
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// Expect limit config detail
	LimitItemDetail `json:",inline" protobuf:"bytes,2,opt,name=expect"`

	Strategy LimitStrategy `json:"strategy,omitempty" protobuf:"varint,3,opt,name=strategy"`

	// TODO Degradation
}

type RateLimitItemStatus struct {
	// Schema name
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	// Current limit status detail
	LimitItemDetail `json:",inline" protobuf:"bytes,2,opt,name=current"`

	// percent of ( actual requests / limit threshold )
	RequestLevel int32 `json:"requestLevel,omitempty" protobuf:"bytes,3,opt,name=requestLevel"`
}

// Represents token bucket rate limit approach.
type LimitItemDetail struct {
	// MaxRequestsInflight represents a maximum concurrent number of requests
	// in flight at a given time.
	// +optianal
	MaxRequestsInflight *MaxRequestsInflightFlowControlSchema `json:"maxRequestsInflight,omitempty" protobuf:"bytes,1,opt,name=maxRequestsInflight"`
	// TokenBucket represents a token bucket approach. The rate limiter allows bursts
	// of up to 'burst' to exceed the QPS, while still maintaining a smoothed qps
	// rate of 'qps'.
	// +optianal
	TokenBucket *TokenBucketFlowControlSchema `json:"tokenBucket,omitempty" protobuf:"bytes,2,opt,name=tokenBucket"`
}

type RateLimitServerInfo struct {
	Server        string         `json:"server,omitempty" protobuf:"bytes,1,opt,name=server"`
	ID            string         `json:"id,omitempty" protobuf:"bytes,2,opt,name=id"`
	Version       string         `json:"version,omitempty" protobuf:"bytes,3,opt,name=version"`
	ShardCount    int32          `json:"shardCount,omitempty" protobuf:"bytes,4,opt,name=shardCount"`
	ManagedShards []int32        `json:"managedShards,omitempty" protobuf:"bytes,5,opt,name=managedShards"`
	Endpoints     []EndpointInfo `json:"endpoints,omitempty" protobuf:"bytes,6,rep,name=endpoints"`
}

type EndpointInfo struct {
	Leader     string      `json:"Leader,omitempty" protobuf:"bytes,1,opt,name=Leader"`
	ShardID    int32       `json:"shardID" protobuf:"varint,2,opt,name=shardID"`
	LastChange metav1.Time `json:"lastHeartbeat,omitempty" protobuf:"bytes,3,opt,name=lastHeartbeat"`
}

// ReteLimitRequest defines the request body to do limit
type ReteLimitRequest struct {
	// Rate limit client instance identity
	Instance string `json:"instance,omitempty" protobuf:"bytes,1,opt,name=instance"`
	// Limit tokens requested
	Requests []LimitRequest `json:"requests,omitempty" protobuf:"bytes,2,opt,name=requests"`
}

type LimitRequest struct {
	FlowControl string `json:"flowControl,omitempty" protobuf:"bytes,1,opt,name=flowControl"`
	// Limit tokens required of this flowcontrol
	Tokens int32 `json:"tokens,omitempty" protobuf:"bytes,2,opt,name=tokens"`
}

// ReteLimitResult defines the response body to do limit
type ReteLimitResult struct {
	Results []LimitAcceptResult `json:"results,omitempty" protobuf:"bytes,1,opt,name=results"`
}

type LimitAcceptResult struct {
	FlowControl string `json:"flowControl,omitempty" protobuf:"bytes,1,opt,name=flowControl"`
	Accept      bool   `json:"accept" protobuf:"bytes,2,opt,name=accept"`
	Limit       int32  `json:"limit" protobuf:"bytes,3,opt,name=limit"`
	Error       string `json:"error,omitempty" protobuf:"bytes,4,opt,name=error"`
}
