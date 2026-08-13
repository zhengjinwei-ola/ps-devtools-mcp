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

func TestServiceCallsAllowlistedEndpointAndRedacts(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Query().Get("uid") != "1" {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"uid":1,"access_token":"secret"}`))}, nil
	})}
	service, err := NewService(Config{Endpoints: []Endpoint{{Name: "user-get", URL: "https://test.invalid/user", Method: "POST", AllowedQueryKeys: []string{"uid"}, AllowedBodyFields: []string{"page"}}}, MaxResponseBytes: 4096}, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := service.Call(context.Background(), CallInput{Endpoint: "user-get", Query: map[string]string{"uid": "1"}, Body: map[string]any{"page": 1}})
	if err != nil {
		t.Fatal(err)
	}
	body := output.Body.(map[string]any)
	if body["access_token"] != "[REDACTED]" {
		t.Fatalf("body = %#v", body)
	}
}

func TestServiceRejectsUnallowlistedInput(t *testing.T) {
	service, err := NewService(Config{Endpoints: []Endpoint{{Name: "ping", URL: "https://test.invalid/ping", Method: "GET"}}, MaxResponseBytes: 4096}, &http.Client{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Call(context.Background(), CallInput{Endpoint: "ping", Query: map[string]string{"unsafe": "1"}}); err == nil {
		t.Fatal("unsafe query was accepted")
	}
}
