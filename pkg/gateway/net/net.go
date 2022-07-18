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

package net

import (
	"net"
	"os"
	"strings"

	"k8s.io/apiserver/pkg/server"
)

var (
	localServerNames = GetLocalServerNames()
)

// GetNetInterfaceIPs returns the IP on local network interfaces of the host
func GetNetInterfaceIPs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}

	locals := []string{}

	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok {
			locals = append(locals, ipnet.IP.String())
		}
	}
	return locals
}

// get proxy domain
func GetLocalServerNames() []string {
	localServerNames := []string{"localhost"}
	if proxyDomain := os.Getenv("APISERVER_PROXY_DOMAIN"); len(proxyDomain) > 0 {
		localServerNames = append(localServerNames, proxyDomain)
	}
	if localIP := GetNetInterfaceIPs(); len(localIP) > 0 {
		localServerNames = append(localServerNames, localIP...)
	}
	return localServerNames
}

// check whether request should be processed by apiserver itself
func IsLocalRequest(host string) bool {
	if len(host) == 0 {
		return true
	}
	host = HostWithoutPort(host)
	if host == server.LoopbackClientServerNameOverride {
		// LoopbackClientServerNameOverride is passed to the apiserver from the loopback client in order to
		// select the loopback certificate via SNI if TLS is used.
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() {
			return true
		}
	}
	for _, localServerName := range localServerNames {
		if strings.HasPrefix(host, localServerName) {
			return true
		}
	}
	return false
}

func HostWithoutPort(hostport string) string {
	hostport = strings.ToLower(hostport)
	noport, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return noport
}
