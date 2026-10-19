package integration

import (
	"net/http"
	"strings"
)

// TerminalAuthorization adapts the browser's bearer subprotocol to the HTTP API.
func TerminalAuthorization(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/terminal") && strings.HasPrefix(r.URL.Path, "/csm-api/v1/csms/") && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			if r.Header.Get("Origin") == "" || !sameOrigin(r, allowedOrigin) {
				http.Error(w, "terminal origin rejected", http.StatusForbidden)
				return
			}
			if r.Header.Get("Authorization") == "" && r.Header.Get("Authorization-csm") == "" {
				for _, item := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
					item = strings.TrimSpace(item)
					if strings.HasPrefix(item, "bearer.") {
						r.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(item, "bearer."))
						break
					}
				}
			}
			r.Header.Set("Sec-WebSocket-Protocol", "v4.channel.k8s.io")
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request, allowedOrigin string) bool {
	origin := r.Header.Get("Origin")
	// The public host is controlled by the ingress; Origin must match it exactly.
	return origin == "https://"+r.Host || origin == "http://"+r.Host || allowedOrigin != "" && origin == allowedOrigin
}
