package util

import (
	"fmt"
	"strings"
)

func GenerateRateLimitConditionName(domain, instance string) string {
	return fmt.Sprintf("%s.%s", domain, strings.ReplaceAll(instance, ":", "-"))
}
