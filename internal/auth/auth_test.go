package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequire(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	tests := []struct {
		role   string
		action Action
		want   int
	}{
		{"admin", ActionDelete, 200},
		{"admin", ActionCreate, 200},
		{"contributor", ActionCreate, 200},
		{"contributor", ActionUpdate, 200},
		{"contributor", ActionDelete, 403},
		{"", ActionCreate, 403},
		{"superuser", ActionDelete, 403},
		{"", ActionRead, 200},
	}
	for _, tc := range tests {
		h := Middleware(Require(tc.action)(ok))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.role != "" {
			req.Header.Set("X-User-Role", tc.role)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("role=%q action=%s: got %d want %d", tc.role, tc.action, rec.Code, tc.want)
		}
	}
}
