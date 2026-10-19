package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTerminalAuthorization(t *testing.T) {
	handler := TerminalAuthorization(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer signed.jwt" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("Sec-WebSocket-Protocol"); got != "v4.channel.k8s.io" {
			t.Errorf("subprotocol = %q", got)
		}
	}), "")
	for _, tc := range []struct {
		origin string
		want   int
	}{{"https://csm.example", 200}, {"https://attacker.example", 403}} {
		req := httptest.NewRequest(http.MethodGet, "https://csm.example/csm-api/v1/csms/host/terminal", nil)
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Sec-WebSocket-Protocol", "v4.channel.k8s.io, bearer.signed.jwt")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != tc.want {
			t.Errorf("origin %q: status %d, want %d", tc.origin, out.Code, tc.want)
		}
	}
}
