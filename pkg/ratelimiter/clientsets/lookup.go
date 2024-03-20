package clientsets

import (
	"net"
	"net/url"
	"strings"
)

var serviceLookupFuncs []func(string) []string

func ServiceLookup(service string) []string {
	for _, lookupFunc := range serviceLookupFuncs {
		if endpoints := lookupFunc(service); len(endpoints) > 0 {
			return endpoints
		}
	}
	return nil
}

func AddServiceLookup(fun func(string) []string) {
	serviceLookupFuncs = append(serviceLookupFuncs, fun)
}

func init() {
	AddServiceLookup(multiServiceLookup)
}

func multiServiceLookup(service string) []string {
	endpoints := strings.Split(service, ",")

	var adders []string
	for _, ep := range endpoints {
		if url, err := url.Parse(ep); err == nil && len(url.Host) > 0 {
			adders = append(adders, ep)
			continue
		}
		if _, _, err := net.SplitHostPort(ep); err == nil {
			adders = append(adders, ep)
		}
	}
	return adders
}
