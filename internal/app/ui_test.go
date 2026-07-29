package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRequiresContentTypeAndSingleValue(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantError   bool
	}{
		{"valid", "application/json; charset=utf-8", `{"name":"work-vpn"}`, false},
		{"wrong type", "text/plain", `{"name":"work-vpn"}`, true},
		{"two values", "application/json", `{"name":"work-vpn"} {}`, true},
		{"unknown field", "application/json", `{"unknown":true}`, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			var value struct {
				Name string `json:"name"`
			}
			err := decodeJSON(request, &value)
			if (err != nil) != test.wantError {
				t.Fatalf("decodeJSON() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestLocalSessionRejectsForeignOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := localSession("secret", "http://127.0.0.1:54321", next)

	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:54321/api/quit", nil)
	request.Host = "127.0.0.1:54321"
	request.Header.Set("Origin", "https://attacker.example")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "secret"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestLocalSessionAcceptsAuthenticatedSameOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := localSession("secret", "http://127.0.0.1:54321", next)

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:54321/api/profiles", nil)
	request.Host = "127.0.0.1:54321"
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "secret"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
