package util

import "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"

func FlowControlConfigToMap(flowControlConfigurations []v1alpha1.RateLimitItemConfiguration) map[string]v1alpha1.RateLimitItemConfiguration {
	configs := map[string]v1alpha1.RateLimitItemConfiguration{}
	for _, fc := range flowControlConfigurations {
		configs[fc.Name] = fc
	}
	return configs
}

func FlowControlStatusToMap(flowControlStatuses []v1alpha1.RateLimitItemStatus) map[string]v1alpha1.RateLimitItemStatus {
	statusMap := map[string]v1alpha1.RateLimitItemStatus{}
	for _, fs := range flowControlStatuses {
		statusMap[fs.Name] = fs
	}
	return statusMap
}
