package transport

import (
	"context"
	"net/http"
)

type CancelableTransport struct {
	inner  http.RoundTripper
	ctx    context.Context
	cancel context.CancelFunc
}

func (ts *CancelableTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	reqCtx := r.Context()
	ctx, cancel := context.WithCancel(reqCtx)
	go func() {
		select {
		case <-reqCtx.Done(): // req ctx is done
		case <-ts.ctx.Done(): // transport is done
			cancel()
		}
	}()
	r2 := r.Clone(ctx)
	return ts.inner.RoundTrip(r2)
}
func (ts *CancelableTransport) Close() {
	ts.cancel()
}
func NewCancelableTransport(inner http.RoundTripper) *CancelableTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &CancelableTransport{
		inner:  inner,
		ctx:    ctx,
		cancel: cancel,
	}
}
