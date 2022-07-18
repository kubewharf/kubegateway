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

package options

import (
	"fmt"
	"net"
	"strconv"

	"github.com/libp2p/go-reuseport"
	proxyproto "github.com/pires/go-proxyproto"
	"github.com/spf13/pflag"
	"github.com/zoumo/golib/netutil"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/rest"
)

// secure serving options with reuse port and loop back
type SecureServingOptions struct {
	*genericoptions.SecureServingOptionsWithLoopback

	ReusePort           bool
	OtherPorts          []int
	LoopbackClientToken string
}

func NewSecureServingOptions() *SecureServingOptions {
	sso := genericoptions.NewSecureServingOptions()

	// We are composing recommended options for an aggregated api-server,
	// whose client is typically a proxy multiplexing many operations ---
	// notably including long-running ones --- into one HTTP/2 connection
	// into this server.  So allow many concurrent operations.
	sso.HTTP2MaxStreamsPerConnection = 1000

	return &SecureServingOptions{
		SecureServingOptionsWithLoopback: sso.WithLoopback(),
	}
}

func (s *SecureServingOptions) Validate() []error {
	if s == nil {
		return nil
	}
	errors := []error{}
	if s.ReusePort && len(s.LoopbackClientToken) == 0 {
		errors = append(errors, fmt.Errorf("--loopback-client-token must be set when reuse port is enabled"))
	}

	usedPorts := sets.NewInt(s.BindPort)
	for _, port := range s.OtherPorts {
		if port < 1 || port > 65535 {
			errors = append(errors, fmt.Errorf("port %v in --orther-secure-ports must be between 1 and 65535, inclusive. It cannot be turned off with 0", port))
		}
		if usedPorts.Has(port) {
			errors = append(errors, fmt.Errorf("port %v in --orther-secure-ports is duplicate", port))
		} else {
			usedPorts.Insert(port)
		}
	}

	errors = append(errors, s.SecureServingOptionsWithLoopback.Validate()...)
	return errors
}

func (s *SecureServingOptions) AddFlags(fs *pflag.FlagSet) {
	if s == nil {
		return
	}

	s.SecureServingOptions.AddFlags(fs)
	fs.IntSliceVar(&s.OtherPorts, "other-secure-ports", s.OtherPorts, "A list of ports which to serve HTTPS with authentication and authorization. The same with --secure-ports")
	fs.BoolVar(&s.ReusePort, "enable-reuse-port", s.ReusePort, "enable reuse port on secure serving port")
	fs.StringVar(&s.LoopbackClientToken, "loopback-client-token", s.LoopbackClientToken, "privileged loopback client token used for reuse port mode")
}

// ApplyTo fills up serving information in the server configuration.
func (s *SecureServingOptions) ApplyTo(secureServingInfo **server.SecureServingInfo, loopbackClientConfig **rest.Config) error {
	if s == nil || s.SecureServingOptionsWithLoopback == nil || s.SecureServingOptions == nil || secureServingInfo == nil {
		return nil
	}

	if s.ReusePort {
		var err error
		addr := net.JoinHostPort(s.BindAddress.String(), strconv.Itoa(s.BindPort))
		s.Listener, s.BindPort, err = CreateReusePortListener(s.BindNetwork, addr)
		if err != nil {
			return fmt.Errorf("failed to create listener: %v", err)
		}
		// wrap listener with proxy protocol
		s.Listener = wrapProxyProtoListener(s.Listener)
	}

	if err := s.SecureServingOptionsWithLoopback.ApplyTo(secureServingInfo, loopbackClientConfig); err != nil {
		return err
	}

	if len(s.OtherPorts) > 0 {
		// listen on other secure ports
		listeners := []net.Listener{s.Listener}
		for _, port := range s.OtherPorts {
			addr := net.JoinHostPort(s.BindAddress.String(), strconv.Itoa(port))
			var l net.Listener
			var err error
			if s.ReusePort {
				l, _, err = CreateReusePortListener(s.BindNetwork, addr)
			} else {
				l, _, err = genericoptions.CreateListener(s.BindNetwork, addr)
			}
			if err != nil {
				return fmt.Errorf("failed to create listener: %v", err)
			}
			// wrap listener with proxy protocol
			l = wrapProxyProtoListener(l)
			listeners = append(listeners, l)
		}

		aggregate, err := netutil.NewAggregatedListener(listeners...)
		if err != nil {
			return err
		}
		s.Listener = aggregate
		(*secureServingInfo).Listener = aggregate
	}

	if s.ReusePort {
		if loopbackClientConfig != nil && *loopbackClientConfig != nil {
			// loopback client will connect to another server when using reuse port.
			// so we need to skip verify server's TLS certificate and use the same
			// privileged bearer token
			(*loopbackClientConfig).TLSClientConfig.Insecure = true
			(*loopbackClientConfig).TLSClientConfig.CAData = nil
			(*loopbackClientConfig).BearerToken = s.LoopbackClientToken
		}
	}
	return nil
}

func CreateReusePortListener(network, addr string) (net.Listener, int, error) {
	if len(network) == 0 {
		network = "tcp"
	}
	ln, err := reuseport.Listen(network, addr)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to listen on %v: %v", addr, err)
	}

	// get port
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		ln.Close()
		return nil, 0, fmt.Errorf("invalid listen address: %q", ln.Addr().String())
	}

	return ln, tcpAddr.Port, nil
}

func wrapProxyProtoListener(in net.Listener) net.Listener {
	return &proxyproto.Listener{
		Listener: in,
	}
}
