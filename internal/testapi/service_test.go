package testapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestServiceCallsArbitraryEndpointAndRedacts(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPatch || request.URL.String() != "http://127.0.0.1:8080/user?uid=1" || request.Header.Get("X-Test") != "yes" {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"uid":1,"access_token":"secret"}`))}, nil
	})}
	service, err := NewService(client, 4096, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := service.Call(context.Background(), CallInput{URL: "http://127.0.0.1:8080/user", Method: "PATCH", Query: map[string]string{"uid": "1"}, Headers: map[string]string{"X-Test": "yes"}, Body: map[string]any{"page": 1}})
	if err != nil {
		t.Fatal(err)
	}
	body := output.Body.(map[string]any)
	if body["access_token"] != "[REDACTED]" {
		t.Fatalf("body = %#v", body)
	}
}

func TestServiceRejectsInvalidURL(t *testing.T) {
	service, err := NewService(&http.Client{}, 4096, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Call(context.Background(), CallInput{URL: "not-a-url"}); err == nil {
		t.Fatal("invalid URL was accepted")
	}
}
