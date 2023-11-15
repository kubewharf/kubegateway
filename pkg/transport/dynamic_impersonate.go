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

package transport

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/transport"
	"k8s.io/klog"
)

type requestCanceler interface {
	CancelRequest(*http.Request)
}

var _ net.RoundTripperWrapper = &dynamicImpersonatingRoundTripper{}
var _ proxy.UpgradeRequestRoundTripper = &dynamicImpersonatingRoundTripper{}
var _ requestCanceler = &dynamicImpersonatingRoundTripper{}

type dynamicImpersonatingRoundTripper struct {
	delegate http.RoundTripper
}

func NewDynamicImpersonatingRoundTripper(rt http.RoundTripper) http.RoundTripper {
	return &dynamicImpersonatingRoundTripper{
		delegate: rt,
	}
}

// WrapRequest implements k8s.io/apimachinery/pkg/util/proxy.UpgradeRequestRoundTripper interface.
// It retrieve request user from context and add it to new request in headers.
func (rt *dynamicImpersonatingRoundTripper) WrapRequest(req *http.Request) (*http.Request, error) {
	if len(req.Header.Get(transport.ImpersonateUserHeader)) != 0 {
		// impersonate header already be set
		return req, nil
	}
	// no impersonate header
	requestor, exists := request.UserFrom(req.Context())
	if !exists {
		// no user found in context, pass request to delegator
		return req, nil
	}

	if klog.V(5) {
		klog.Infof("Add impersonator headers:")
		klog.Infof("     Name: %s", requestor.GetName())
		klog.Infof("    Group: %s", groupsToString(requestor.GetGroups()))
		klog.Infof("    Extra: %s", extraToString(requestor.GetExtra()))
	}

	req = net.CloneRequest(req)
	req.Header.Set(transport.ImpersonateUserHeader, requestor.GetName())

	for _, group := range requestor.GetGroups() {
		req.Header.Add(transport.ImpersonateGroupHeader, group)
	}
	for k, vv := range requestor.GetExtra() {
		for _, v := range vv {
			req.Header.Add(transport.ImpersonateUserExtraHeaderPrefix+headerKeyEscape(k), v)
		}
	}

	return req, nil
}

func (rt *dynamicImpersonatingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	klog.V(5).Infof("%s %s Host:%s", req.Method, req.URL.String(), req.Host)
	newReq, err := rt.WrapRequest(req)
	if err != nil {
		return nil, err
	}
	return rt.delegate.RoundTrip(newReq)
}

func (rt *dynamicImpersonatingRoundTripper) CancelRequest(req *http.Request) {
	if canceler, ok := rt.delegate.(requestCanceler); ok {
		canceler.CancelRequest(req)
	} else {
		klog.Errorf("CancelRequest not implemented by %T", rt.delegate)
	}
}

func (rt *dynamicImpersonatingRoundTripper) WrappedRoundTripper() http.RoundTripper {
	return rt.delegate
}

// func impersonationConfigKey(in transport.ImpersonationConfig) string {
// 	return fmt.Sprintf("name=%s|groups=%v|extra=%v", in.UserName, groupsToString(in.Groups), extraToString(in.Extra))
// }

func groupsToString(in []string) string {
	sort.Strings(in)
	return fmt.Sprintf("%v", in)
}

func extraToString(in map[string][]string) string {
	if len(in) == 0 {
		return ""
	}
	keys := []string{}
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b := strings.Builder{}
	b.WriteString("map{")
	v := []string{}
	for _, k := range keys {
		sort.Strings(in[k])
		v = append(v, fmt.Sprintf("%s=%v", k, in[k]))
	}
	b.WriteString(strings.Join(v, ","))
	b.WriteString("}")

	return b.String()
}

// legalHeaderKeyBytes was copied from net/http/lex.go's isTokenTable.
// See https://httpwg.github.io/specs/rfc7230.html#rule.token.separators
var legalHeaderKeyBytes = [127]bool{
	'%':  true,
	'!':  true,
	'#':  true,
	'$':  true,
	'&':  true,
	'\'': true,
	'*':  true,
	'+':  true,
	'-':  true,
	'.':  true,
	'0':  true,
	'1':  true,
	'2':  true,
	'3':  true,
	'4':  true,
	'5':  true,
	'6':  true,
	'7':  true,
	'8':  true,
	'9':  true,
	'A':  true,
	'B':  true,
	'C':  true,
	'D':  true,
	'E':  true,
	'F':  true,
	'G':  true,
	'H':  true,
	'I':  true,
	'J':  true,
	'K':  true,
	'L':  true,
	'M':  true,
	'N':  true,
	'O':  true,
	'P':  true,
	'Q':  true,
	'R':  true,
	'S':  true,
	'T':  true,
	'U':  true,
	'W':  true,
	'V':  true,
	'X':  true,
	'Y':  true,
	'Z':  true,
	'^':  true,
	'_':  true,
	'`':  true,
	'a':  true,
	'b':  true,
	'c':  true,
	'd':  true,
	'e':  true,
	'f':  true,
	'g':  true,
	'h':  true,
	'i':  true,
	'j':  true,
	'k':  true,
	'l':  true,
	'm':  true,
	'n':  true,
	'o':  true,
	'p':  true,
	'q':  true,
	'r':  true,
	's':  true,
	't':  true,
	'u':  true,
	'v':  true,
	'w':  true,
	'x':  true,
	'y':  true,
	'z':  true,
	'|':  true,
	'~':  true,
}

func legalHeaderByte(b byte) bool {
	return int(b) < len(legalHeaderKeyBytes) && legalHeaderKeyBytes[b]
}

func shouldEscape(b byte) bool {
	// url.PathUnescape() returns an error if any '%' is not followed by two
	// hexadecimal digits, so we'll intentionally encode it.
	return !legalHeaderByte(b) || b == '%'
}

func headerKeyEscape(key string) string {
	buf := strings.Builder{}
	for i := 0; i < len(key); i++ {
		b := key[i]
		if shouldEscape(b) {
			// %-encode bytes that should be escaped:
			// https://tools.ietf.org/html/rfc3986#section-2.1
			fmt.Fprintf(&buf, "%%%02X", b)
			continue
		}
		buf.WriteByte(b)
	}
	return buf.String()
}
