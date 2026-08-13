package httptransport

import (
	"crypto/subtle"
	"log"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxMCPRequestBody = 1 << 20

func NewHandler(server *mcp.Server, tokens map[string]string, maxConcurrent int, logger *log.Logger) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		MaxRequestBodyBytes:          maxMCPRequestBody,
		PropagateRequestCancellation: true,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/mcp", authenticate(tokens, limitConcurrent(maxConcurrent, streamable), logger))
	return mux
}

func authenticate(tokens map[string]string, next http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		provided, validHeader := parseBearerToken(req.Header.Get("Authorization"))
		name, authorized := matchToken(tokens, provided)
		if !validHeader || !authorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			if logger != nil {
				logger.Printf("transport=http path=/mcp remote=%q authorized=false", req.RemoteAddr)
			}
			return
		}
		if logger != nil {
			logger.Printf("transport=http path=/mcp remote=%q authorized=true token_name=%q", req.RemoteAddr, name)
		}
		next.ServeHTTP(w, req)
	})
}

func parseBearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return token, token != ""
}

func matchToken(tokens map[string]string, provided string) (string, bool) {
	matchedName := ""
	matched := 0
	for name, token := range tokens {
		// Compare every configured token so the matching entry's map position does
		// not affect authentication timing.
		equal := subtle.ConstantTimeCompare([]byte(provided), []byte(token))
		if equal == 1 {
			matchedName = name
		}
		matched |= equal
	}
	return matchedName, matched == 1
}

func limitConcurrent(maxConcurrent int, next http.Handler) http.Handler {
	semaphore := make(chan struct{}, maxConcurrent)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
			next.ServeHTTP(w, req)
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
		}
	})
}
