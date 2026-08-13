package testdb

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestHTTPClientQuery(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected request: method=%s content-type=%s", req.Method, req.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"engine":3`) {
			t.Fatalf("request body = %s", body)
		}
		return jsonResponse(http.StatusOK, `{"common":{"code":0,"msg":""},"resultJson":"{\"columns\":[\"id\"],\"rows\":[[1]]}","rowCount":1,"elapsedMs":4}`), nil
	})}

	client, err := NewHTTPClient("http://test.invalid/query", httpClient, 2048)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Query(context.Background(), QueryInput{Statement: "SELECT id FROM t LIMIT 1", Engine: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Engine != 3 || result.RowCount != 1 || result.ElapsedMS != 4 {
		t.Fatalf("result = %+v", result)
	}
}

func TestHTTPClientQueryAcceptsSnakeCaseResponse(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"common":{"code":0,"msg":""},"result_json":"{\"columns\":[],\"rows\":[]}","row_count":"0","elapsed_ms":"2"}`), nil
	})}
	client, err := NewHTTPClient("http://test.invalid/query", httpClient, 2048)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Query(context.Background(), QueryInput{Statement: "SELECT id FROM t LIMIT 1", Engine: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResultJSON == "" || result.ElapsedMS != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestHTTPClientRejectsApplicationError(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"common":{"code":400,"msg":"forbidden"}}`), nil
	})}
	client, err := NewHTTPClient("http://test.invalid/query", httpClient, 2048)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Query(context.Background(), QueryInput{Statement: "SELECT id FROM t LIMIT 1", Engine: 1})
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPClientRejectsOversizedResponse(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, strings.Repeat("x", 33)), nil
	})}
	client, err := NewHTTPClient("http://test.invalid/query", httpClient, 32)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Query(context.Background(), QueryInput{Statement: "SELECT id FROM t LIMIT 1", Engine: 1})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v", err)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
