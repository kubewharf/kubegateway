package transport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCancelableTransportClose(t *testing.T) {
	ts := httptest.NewServer(new(testServer))
	defer ts.Close()
	ct := NewCancelableTransport(http.DefaultTransport)
	client := http.Client{
		Transport: ct,
	}
	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Errorf("get error: %v", err)
	}
	resp.Body.Close()
	time.AfterFunc(200*time.Millisecond, func() {
		ct.Close()
	})
	start := time.Now()
	_, err = client.Get(ts.URL)
	latency := time.Since(start)
	if err == nil {
		t.Errorf("should have error")
	}
	if latency > 300*time.Millisecond {
		t.Errorf("should cancel request")
	}
}

func TestCancelableTransportCancel(t *testing.T) {
	ct := NewCancelableTransport(new(mockTransport))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "localhost", nil)
	if err != nil {
		t.Errorf("new request error")
	}
	_, err = ct.RoundTrip(req)
	if err == nil {
		t.Errorf("should have error")
	}
	if err.Error() != "req context done" {
		t.Errorf("unexpected error: %v", err)
	}
}

type mockTransport struct {
}

// RoundTrip implements http.RoundTripper.
func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("req context done")
	default:
	}
	return nil, fmt.Errorf("mock error")
}

type testServer struct {
}

func (s *testServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	time.Sleep(500 * time.Millisecond)
	w.WriteHeader(200)
}
