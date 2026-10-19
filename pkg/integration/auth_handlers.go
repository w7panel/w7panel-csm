package integration

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// AuthRoutes exposes a public PKCE code exchange and refresh endpoint.
// Both responses contain only tokens verified against the configured issuer.
func (a *Authenticator) AuthRoutes(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /csm-api/v1/auth/config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]string{"authorizationEndpoint": a.authorizationEndpoint, "clientId": a.clientID, "redirectUri": a.redirectURI, "scope": "openid profile", "responseType": "code"})
	})
	mux.HandleFunc("GET /csm-api/v1/login/config", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/csm-api/v1/auth/config"
		mux.ServeHTTP(w, r)
	})
	mux.HandleFunc("POST /csm-api/v1/auth/login", a.login)
	mux.HandleFunc("POST /csm-api/v1/login", a.login)
	mux.HandleFunc("POST /csm-api/v1/auth/refresh", a.refresh)
	mux.Handle("/", next)
	return mux
}

func (a *Authenticator) oauthConfig() oauth2.Config {
	return oauth2.Config{ClientID: a.clientID, RedirectURL: a.redirectURI, Endpoint: oauth2.Endpoint{TokenURL: a.tokenEndpoint, AuthStyle: oauth2.AuthStyleInParams}}
}

func (a *Authenticator) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
		RedirectURI  string `json:"redirect_uri"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil || input.Code == "" || input.RedirectURI != a.redirectURI || len(input.CodeVerifier) < 43 || len(input.CodeVerifier) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid code, code_verifier and configured redirect_uri are required"})
		return
	}
	ctx := context.WithValue(r.Context(), oauth2.HTTPClient, a.client)
	config := a.oauthConfig()
	token, err := config.Exchange(ctx, input.Code, oauth2.SetAuthURLParam("code_verifier", input.CodeVerifier))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "OIDC code exchange failed"})
		return
	}
	a.tokenResponse(w, r, token, "")
}

func (a *Authenticator) refresh(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RefreshToken string `json:"refresh_token"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil || strings.TrimSpace(input.RefreshToken) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "refresh_token is required"})
		return
	}
	ctx := context.WithValue(r.Context(), oauth2.HTTPClient, a.client)
	config := a.oauthConfig()
	token, err := config.TokenSource(ctx, &oauth2.Token{RefreshToken: input.RefreshToken}).Token()
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "OIDC refresh failed; log in again"})
		return
	}
	a.tokenResponse(w, r, token, input.RefreshToken)
}

func (a *Authenticator) tokenResponse(w http.ResponseWriter, r *http.Request, token *oauth2.Token, previousRefresh string) {
	if rawID, ok := token.Extra("id_token").(string); ok {
		if err := a.cacheIDToken(r.Context(), token.AccessToken, rawID); err != nil {
			log.Printf("OIDC ID token verification failed: %v", err)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "OIDC ID token verification failed"})
			return
		}
	}
	identity, err := a.Authenticate(r.Context(), token.AccessToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "OIDC access token verification failed"})
		return
	}
	if a.EnsureNamespace != nil {
		if err := a.EnsureNamespace(r.Context(), identity.Namespace); err != nil {
			log.Printf("CSM tenant namespace creation failed: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "CSM tenant namespace creation failed"})
			return
		}
	}
	refresh := token.RefreshToken
	if refresh == "" {
		refresh = previousRefresh
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"access_token": token.AccessToken, "refresh_token": refresh, "token_type": "Bearer", "expires_in": int64(time.Until(token.Expiry).Seconds()), "namespace": identity.Namespace})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
