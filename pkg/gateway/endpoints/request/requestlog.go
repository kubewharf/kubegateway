package request

import (
	"k8s.io/klog"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	ShortRequestLogThreshold = time.Second * 5
	ListRequestLogThreshold  = time.Second * 30
)

func init() {
	if val := os.Getenv("SHORT_REQUEST_LOG_THRESHOLD_SECONDS"); len(val) > 0 {
		i, err := strconv.Atoi(val)
		if err != nil {
			klog.Warningf("Illegal REQUEST_TRACE_LOG_THRESHOLD_SECONDS: %v", val)
		} else {
			ShortRequestLogThreshold = time.Second * time.Duration(i)
		}
	}

	if val := os.Getenv("LIST_REQUEST_LOG_THRESHOLD_SECONDS"); len(val) > 0 {
		i, err := strconv.Atoi(val)
		if err != nil {
			klog.Warningf("Illegal LIST_REQUEST_TRACE_LOG_THRESHOLD_SECONDS: %v", val)
		} else {
			ListRequestLogThreshold = time.Second * time.Duration(i)
		}
	}
}

func LogThreshold(verb string) time.Duration {
	threshold := ShortRequestLogThreshold
	if strings.Contains(strings.ToLower(verb), "list") {
		threshold = ListRequestLogThreshold
	}
	return threshold
}
