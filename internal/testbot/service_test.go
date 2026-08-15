package testbot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestServiceLogsInCachesTokenAndRedactsResponse(t *testing.T) {
	var loginCalls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/account/passwordLogin":
			loginCalls.Add(1)
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("area") != "86" || request.Form.Get("mobile") != "test-mobile" || request.Form.Get("password") != sha1Hex("test-password") {
				t.Fatalf("unexpected login form: %#v", request.Form)
			}
			return jsonResponse(http.StatusOK, `{"success":true,"data":{"uid":123,"token":"login-token"}}`), nil
		case "/profile":
			if request.Header.Get("User-Token") != "login-token" || request.Header.Get("X-TestBot-Request-ID") == "" {
				t.Fatalf("missing testbot headers: %#v", request.Header)
			}
			return jsonResponse(http.StatusOK, `{"uid":123,"access_token":"response-secret","mobile":"hidden"}`), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`), nil
		}
	})}

	service, err := NewService(Config{
		BaseURL: "https://test.invalid", LoginPath: "/account/passwordLogin", MaxResponseBytes: 4096,
		Endpoints: []Endpoint{{Name: "profile", Path: "/profile", Method: http.MethodGet, Encoding: "json"}},
	}, Credentials{Area: "86", Mobile: "test-mobile", Password: "test-password"}, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		output, err := service.Call(context.Background(), CallInput{Endpoint: "profile"})
		if err != nil {
			t.Fatal(err)
		}
		body := output.Body.(map[string]any)
		if body["access_token"] != "[REDACTED]" || body["mobile"] != "[REDACTED]" || output.RequestID == "" {
			t.Fatalf("output = %#v", output)
		}
	}
	if loginCalls.Load() != 1 {
		t.Fatalf("login calls = %d, want 1", loginCalls.Load())
	}
}

func TestServiceRejectsSideEffectWithoutConfirmation(t *testing.T) {
	service, err := NewService(Config{
		BaseURL: "https://test.invalid", LoginPath: "/login", MaxResponseBytes: 4096,
		Endpoints: []Endpoint{{Name: "send", Path: "/send", Method: http.MethodPost, Encoding: "json", SideEffect: true}},
	}, Credentials{Area: "86", Mobile: "test-mobile", Password: "test-password"}, http.DefaultClient, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Call(context.Background(), CallInput{Endpoint: "send"})
	if err == nil || !strings.Contains(err.Error(), "confirm_side_effect") {
		t.Fatalf("error = %v", err)
	}
}

func TestServiceRetriesLoginOnceAfterUnauthorized(t *testing.T) {
	var loginCalls atomic.Int32
	var apiCalls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/login" {
			call := loginCalls.Add(1)
			encoded, _ := json.Marshal(map[string]any{"success": true, "data": map[string]any{"uid": 123, "token": "token-" + string(rune('0'+call))}})
			return jsonResponse(http.StatusOK, string(encoded)), nil
		}
		if apiCalls.Add(1) == 1 {
			return jsonResponse(http.StatusUnauthorized, `{}`), nil
		}
		return jsonResponse(http.StatusOK, `{"ok":true}`), nil
	})}

	service, err := NewService(Config{BaseURL: "https://test.invalid", LoginPath: "/login", MaxResponseBytes: 4096, Endpoints: []Endpoint{{Name: "profile", Path: "/profile", Method: http.MethodGet, Encoding: "json"}}}, Credentials{Area: "86", Mobile: "m", Password: "p"}, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := service.Call(context.Background(), CallInput{Endpoint: "profile"})
	if err != nil {
		t.Fatal(err)
	}
	if output.StatusCode != http.StatusOK || loginCalls.Load() != 2 || apiCalls.Load() != 2 {
		t.Fatalf("output=%#v login=%d api=%d", output, loginCalls.Load(), apiCalls.Load())
	}
}

func TestRedactResponseProtectsSecretsInTruncatedJSON(t *testing.T) {
	body := redactResponse([]byte(`{"access_token":"secret","value":"partial`), "application/json", true).(string)
	if strings.Contains(body, "secret") || !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("body = %q", body)
	}
}
