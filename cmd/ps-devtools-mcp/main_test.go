package main

import "testing"

func TestDefaultQueryURLUsesLoopback(t *testing.T) {
	const want = "http://127.0.0.1/gk/v1/external/testEnvQuery"
	if defaultQueryURL != want {
		t.Fatalf("defaultQueryURL = %q, want %q", defaultQueryURL, want)
	}
}
