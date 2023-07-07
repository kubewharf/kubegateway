// Copyright 2022 ByteDance and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dispatcher

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/kubewharf/kubegateway/pkg/clusters"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/httpstream"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/klog"
)

// NOTICE: most of the following codes are copied from k8s.io/apimachinery/pkg/util/proxy/upgradeawarehandler.go
// we can only do this to inject ErrorHandler into the ReverseProxy util the dependency apimachinery package is
// upgrade to a higher version, e.g. v0.19.16.

// UpgradeAwareHandler is a handler for proxy requests that may require an upgrade
type UpgradeAwareHandler struct {
	*proxy.UpgradeAwareHandler
	endpoint *clusters.EndpointInfo
}

// NewUpgradeAwareHandler creates a new proxy handler with a default flush interval. Responder is required for returning
// errors to the caller.
func NewUpgradeAwareHandler(location *url.URL, transport http.RoundTripper, wrapTransport, upgradeRequired bool, responder proxy.ErrorResponder, endpoint *clusters.EndpointInfo) *UpgradeAwareHandler {
	return &UpgradeAwareHandler{
		UpgradeAwareHandler: proxy.NewUpgradeAwareHandler(location, transport, wrapTransport, upgradeRequired, responder),
		endpoint:            endpoint,
	}
}

// ServeHTTP handles the proxy request
func (h *UpgradeAwareHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if httpstream.IsUpgradeRequest(req) {
		h.UpgradeAwareHandler.ServeHTTP(w, req)
		return
	}

	if h.UpgradeRequired {
		h.Responder.Error(w, req, errors.NewBadRequest("Upgrade request required"))
		return
	}

	loc := *h.Location
	loc.RawQuery = req.URL.RawQuery

	// If original request URL ended in '/', append a '/' at the end of the
	// of the proxy URL
	if !strings.HasSuffix(loc.Path, "/") && strings.HasSuffix(req.URL.Path, "/") {
		loc.Path += "/"
	}

	// From pkg/genericapiserver/endpoints/handlers/proxy.go#ServeHTTP:
	// Redirect requests with an empty path to a location that ends with a '/'
	// This is essentially a hack for http://issue.k8s.io/4958.
	// Note: Keep this code after tryUpgrade to not break that flow.
	if len(loc.Path) == 0 {
		var queryPart string
		if len(req.URL.RawQuery) > 0 {
			queryPart = "?" + req.URL.RawQuery
		}
		w.Header().Set("Location", req.URL.Path+"/"+queryPart)
		w.WriteHeader(http.StatusMovedPermanently)
		return
	}

	if h.Transport == nil || h.WrapTransport {
		h.Transport = h.defaultProxyTransport(req.URL, h.Transport)
	}

	// WithContext creates a shallow clone of the request with the same context.
	newReq := req.WithContext(req.Context())
	newReq.Header = utilnet.CloneHeader(req.Header)
	if !h.UseRequestLocation {
		newReq.URL = &loc
	}

	defer func() {
		if r := recover(); r != nil {
			klog.Errorf("reverseproxy panic'd on %v %v, endpoint: %v, err: %v", req.Method, req.RequestURI, h.Location.Host, r)
			// Send a GOAWAY and tear down the TCP connection when idle.
			w.Header().Set("Connection", "close")
		}
	}()

	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: h.Location.Scheme, Host: h.Location.Host})
	proxy.Transport = h.Transport
	proxy.FlushInterval = h.FlushInterval
	proxy.ErrorLog = log.New(noSuppressPanicError{}, "", log.LstdFlags)
	if h.Responder != nil {
		// if an optional error interceptor/responder was provided wire it
		// the custom responder might be used for providing a unified error reporting
		// or supporting retry mechanisms by not sending non-fatal errors to the clients
		proxy.ErrorHandler = h.Responder.Error
		proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
			if utilnet.IsConnectionRefused(err) {
				klog.Errorf("connection refused err: %v, trigger healthcheck", err)
				h.endpoint.TriggerHealthCheck()
			}

			h.Responder.Error(w, req, err)
		}
	}
	proxy.ServeHTTP(w, newReq)
}

type noSuppressPanicError struct{}

func (noSuppressPanicError) Write(p []byte) (n int, err error) {
	// skip "suppressing panic for copyResponse error in test; copy error" error message
	// that ends up in CI tests on each kube-apiserver termination as noise and
	// everybody thinks this is fatal.
	if strings.Contains(string(p), "suppressing panic") {
		return len(p), nil
	}
	klog.ErrorDepth(4, string(p))
	return len(p), nil
}

func (h *UpgradeAwareHandler) defaultProxyTransport(url *url.URL, internalTransport http.RoundTripper) http.RoundTripper {
	scheme := url.Scheme
	host := url.Host
	suffix := h.Location.Path
	if strings.HasSuffix(url.Path, "/") && !strings.HasSuffix(suffix, "/") {
		suffix += "/"
	}
	pathPrepend := strings.TrimSuffix(url.Path, suffix)
	rewritingTransport := &proxy.Transport{
		Scheme:       scheme,
		Host:         host,
		PathPrepend:  pathPrepend,
		RoundTripper: internalTransport,
	}
	return &corsRemovingTransport{
		RoundTripper: rewritingTransport,
	}
}

// corsRemovingTransport is a wrapper for an internal transport. It removes CORS headers
// from the internal response.
// Implements pkg/util/net.RoundTripperWrapper
type corsRemovingTransport struct {
	http.RoundTripper
}

var _ = utilnet.RoundTripperWrapper(&corsRemovingTransport{})

func (rt *corsRemovingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.RoundTripper.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	removeCORSHeaders(resp)
	return resp, nil
}

func (rt *corsRemovingTransport) WrappedRoundTripper() http.RoundTripper {
	return rt.RoundTripper
}

// removeCORSHeaders strip CORS headers sent from the backend
// This should be called on all responses before returning
func removeCORSHeaders(resp *http.Response) {
	resp.Header.Del("Access-Control-Allow-Credentials")
	resp.Header.Del("Access-Control-Allow-Headers")
	resp.Header.Del("Access-Control-Allow-Methods")
	resp.Header.Del("Access-Control-Allow-Origin")
}
