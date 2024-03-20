package filters

import (
	"context"
	"fmt"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	"k8s.io/apiserver/pkg/endpoints/request"
	"net/http"
)

type key int

const (
	// requestInfoKey is the context key for the extra request info.
	requestInfoKey key = iota
)

// WithRequestInfo attaches a RequestInfo to the context.
func WithRequestInfo(handler http.Handler, resolver request.RequestInfoResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		info, err := resolver.NewRequestInfo(req)
		if err != nil {
			responsewriters.InternalError(w, req, fmt.Errorf("failed to create RequestInfo: %v", err))
			return
		}

		req = req.WithContext(request.WithRequestInfo(ctx, info))

		handler.ServeHTTP(w, req)
	})
}

// WithExtraRequestInfo attaches extra request info to the context.
func WithExtraRequestInfo(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		info := &ExtraRequestInfo{}
		req = req.WithContext(WithExtraReqeustInfo(ctx, info))
		handler.ServeHTTP(w, req)
	})
}

type ExtraRequestInfo struct {
	UpstreamCluster string
	Instance        string
}

// WithExtraReqeustInfo returns a copy of parent in which the ExtraRequestInfo value is set
func WithExtraReqeustInfo(parent context.Context, info *ExtraRequestInfo) context.Context {
	return context.WithValue(parent, requestInfoKey, info)
}

// ExtraReqeustInfoFrom returns the value of the ExtraRequestInfo key on the ctx
func ExtraReqeustInfoFrom(ctx context.Context) (*ExtraRequestInfo, bool) {
	info, ok := ctx.Value(requestInfoKey).(*ExtraRequestInfo)
	return info, ok
}

func SetExtraRequestInfo(ctx context.Context, upstream, instance string) error {
	info, ok := ExtraReqeustInfoFrom(ctx)
	if !ok {
		return fmt.Errorf("no proxy info found in context")
	}
	if len(upstream) > 0 {
		info.UpstreamCluster = upstream
	}
	if len(instance) > 0 {
		info.Instance = instance
	}
	return nil
}
