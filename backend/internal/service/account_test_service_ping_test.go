package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type upstreamPingRoundTripFunc func(*http.Request) (*http.Response, error)

func (f upstreamPingRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func swapMonitorPingHTTPClient(t *testing.T, fn upstreamPingRoundTripFunc) {
	t.Helper()
	orig := monitorPingHTTPClient
	monitorPingHTTPClient = &http.Client{Transport: fn}
	t.Cleanup(func() { monitorPingHTTPClient = orig })
}

func TestPingUpstreamAccountEndpointOrigin_HeadSuccess(t *testing.T) {
	var methods []string
	swapMonitorPingHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method)
		if req.Method != http.MethodHead {
			t.Fatalf("expected no fallback request, got method %s", req.Method)
		}
		return upstreamPingResponse(http.StatusNoContent), nil
	})

	ping := pingUpstreamAccountEndpointOrigin(context.Background(), "https://example.com/v1/messages")

	if ping == nil {
		t.Fatal("expected HEAD latency")
	}
	if len(methods) != 1 || methods[0] != http.MethodHead {
		t.Fatalf("expected only HEAD request, got %v", methods)
	}
}

func TestPingUpstreamAccountEndpointOrigin_FallsBackToGetOnHeadError(t *testing.T) {
	var methods []string
	swapMonitorPingHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method)
		if req.Method == http.MethodHead {
			return nil, errors.New("head blocked")
		}
		return upstreamPingResponse(http.StatusOK), nil
	})

	ping := pingUpstreamAccountEndpointOrigin(context.Background(), "https://example.com/v1/messages")

	if ping == nil {
		t.Fatal("expected GET fallback latency")
	}
	if strings.Join(methods, ",") != "HEAD,GET" {
		t.Fatalf("expected HEAD then GET, got %v", methods)
	}
}

func TestPingUpstreamAccountEndpointOrigin_FallsBackToGetOnUnsupportedHead(t *testing.T) {
	var methods []string
	swapMonitorPingHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		methods = append(methods, req.Method)
		if req.Method == http.MethodHead {
			return upstreamPingResponse(http.StatusMethodNotAllowed), nil
		}
		return upstreamPingResponse(http.StatusOK), nil
	})

	ping := pingUpstreamAccountEndpointOrigin(context.Background(), "https://example.com/v1/messages")

	if ping == nil {
		t.Fatal("expected GET fallback latency")
	}
	if strings.Join(methods, ",") != "HEAD,GET" {
		t.Fatalf("expected HEAD then GET, got %v", methods)
	}
}

func TestPingUpstreamAccountEndpointOrigin_InvalidEndpoint(t *testing.T) {
	ping := pingUpstreamAccountEndpointOrigin(context.Background(), "not-a-url")

	if ping != nil {
		t.Fatalf("expected nil latency for invalid endpoint, got %d", *ping)
	}
}

func upstreamPingResponse(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
	}
}
