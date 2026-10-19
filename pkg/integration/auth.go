package integration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/w7panel/w7panel-csm/pkg/httpapi"
	"golang.org/x/oauth2"
	"k8s.io/apimachinery/pkg/util/validation"
)

type Authenticator struct {
	EnsureNamespace       func(context.Context, string) error
	verifier              *oidc.IDTokenVerifier
	idVerifier            *oidc.IDTokenVerifier
	clientID              string
	redirectURI           string
	authorizationEndpoint string
	tokenEndpoint         string
	client                *http.Client
	mu                    sync.Mutex
	roles                 map[[32]byte]cachedRole
}

type oidcClaims struct {
	Subject   string `json:"sub"`
	Username  string `json:"preferred_username"`
	Role      string `json:"role"`
	IsFounder *bool  `json:"is_founder"`
}
type cachedRole struct {
	Subject, Username string
	Founder           bool
	Expires           time.Time
}

// NewAuthenticator pins the issuer and JWKS endpoint from discovery at startup.
func NewAuthenticator(ctx context.Context, discoveryURL, audience, clientID, redirectURI, publicAuthorizationURL string) (*Authenticator, error) {
	u, err := url.Parse(discoveryURL)
	if err != nil || !trustedOIDCURL(u) || audience == "" {
		return nil, fmt.Errorf("OIDC discovery URL must use HTTPS or an explicitly configured internal HTTP host, and audience must be set")
	}
	redirect, err := url.Parse(redirectURI)
	if clientID == "" || err != nil || redirect.Scheme != "https" || redirect.Host == "" || redirect.Fragment != "" {
		return nil, fmt.Errorf("OIDC client ID and HTTPS redirect URI are required")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery returned HTTP %d", resp.StatusCode)
	}
	var doc struct {
		Issuer                string `json:"issuer"`
		JWKSURI               string `json:"jwks_uri"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, err
	}
	issuer, err := url.Parse(doc.Issuer)
	if err != nil || !trustedOIDCURL(issuer) {
		return nil, fmt.Errorf("OIDC discovery has invalid issuer")
	}
	jwks, err := url.Parse(doc.JWKSURI)
	if err != nil || !trustedOIDCURL(jwks) {
		return nil, fmt.Errorf("OIDC discovery has invalid JWKS URL")
	}
	authURL, authErr := url.Parse(doc.AuthorizationEndpoint)
	tokenURL, tokenErr := url.Parse(doc.TokenEndpoint)
	if authErr != nil || tokenErr != nil || !trustedOIDCURL(authURL) || !trustedOIDCURL(tokenURL) {
		return nil, fmt.Errorf("OIDC discovery has invalid authorization or token endpoint")
	}
	authorizationEndpoint := doc.AuthorizationEndpoint
	if publicAuthorizationURL != "" {
		publicURL, err := url.Parse(publicAuthorizationURL)
		if err != nil || publicURL.Scheme != "https" || publicURL.Host == "" || publicURL.User != nil || publicURL.RawQuery != "" || publicURL.Fragment != "" || publicURL.Path != authURL.Path {
			return nil, fmt.Errorf("public OIDC authorization URL must be HTTPS and match the discovered path")
		}
		authorizationEndpoint = publicAuthorizationURL
	}
	if strings.HasPrefix(authorizationEndpoint, "http://") {
		return nil, fmt.Errorf("browser OIDC authorization URL must use HTTPS")
	}
	keys := oidc.NewRemoteKeySet(context.WithValue(context.Background(), oauth2.HTTPClient, client), doc.JWKSURI)
	return &Authenticator{verifier: oidc.NewVerifier(doc.Issuer, keys, &oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{oidc.RS256}}), idVerifier: oidc.NewVerifier(doc.Issuer, keys, &oidc.Config{ClientID: clientID, SupportedSigningAlgs: []string{oidc.RS256}}), clientID: clientID, redirectURI: redirectURI, authorizationEndpoint: authorizationEndpoint, tokenEndpoint: doc.TokenEndpoint, client: client, roles: make(map[[32]byte]cachedRole)}, nil
}

func trustedOIDCURL(u *url.URL) bool {
	if u == nil || u.Host == "" || u.User != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) || strings.HasSuffix(host, ".svc") || strings.HasSuffix(host, ".svc.cluster.local")
}

func (a *Authenticator) Authenticate(ctx context.Context, token string) (httpapi.Identity, error) {
	var empty httpapi.Identity
	if a == nil || a.verifier == nil || token == "" {
		return empty, fmt.Errorf("OIDC verifier or token missing")
	}
	verified, err := a.verifier.Verify(ctx, token)
	if err != nil {
		return empty, err
	}
	var claims oidcClaims
	if err := verified.Claims(&claims); err != nil {
		return empty, err
	}
	if claims.Subject == "" {
		return empty, fmt.Errorf("OIDC token has invalid user identity")
	}
	a.mu.Lock()
	cached, ok := a.roles[sha256.Sum256([]byte(token))]
	a.mu.Unlock()
	if ok && cached.Expires.After(time.Now()) && cached.Subject == claims.Subject {
		if claims.Username != "" && claims.Username != cached.Username {
			return empty, fmt.Errorf("OIDC token username differs from verified ID token")
		}
		claims.Username = cached.Username
	}
	// CSM maps business users to csm-<preferred_username>; never infer it from an opaque subject.
	if claims.Username == "" || len(validation.IsDNS1123Label("csm-"+claims.Username)) != 0 {
		return empty, fmt.Errorf("OIDC token has invalid user identity")
	}
	founder := strings.EqualFold(claims.Role, "founder")
	if claims.IsFounder != nil {
		founder = *claims.IsFounder
	} else if claims.Role == "" {
		if !ok || !cached.Expires.After(time.Now()) || cached.Subject != claims.Subject || cached.Username != claims.Username {
			return empty, fmt.Errorf("OIDC token has no verified role claim; log in again")
		}
		founder = cached.Founder
	}
	return httpapi.Identity{Namespace: "csm-" + claims.Username, Subject: claims.Subject, Founder: founder}, nil
}

func (a *Authenticator) cacheIDToken(ctx context.Context, accessToken, rawIDToken string) error {
	if rawIDToken == "" {
		return nil
	}
	id, err := a.idVerifier.Verify(ctx, rawIDToken)
	if err != nil {
		return err
	}
	access, err := a.verifier.Verify(ctx, accessToken)
	if err != nil {
		return err
	}
	var idClaims, accessClaims oidcClaims
	if err := id.Claims(&idClaims); err != nil {
		return err
	}
	if err := access.Claims(&accessClaims); err != nil {
		return err
	}
	if idClaims.Subject == "" || idClaims.Subject != accessClaims.Subject || idClaims.Username == "" || (accessClaims.Username != "" && idClaims.Username != accessClaims.Username) {
		return fmt.Errorf("OIDC ID and access token identities differ")
	}
	if idClaims.Role == "" && idClaims.IsFounder == nil {
		return nil
	}
	founder := strings.EqualFold(idClaims.Role, "founder")
	if idClaims.IsFounder != nil {
		founder = *idClaims.IsFounder
	}
	a.mu.Lock()
	for key, value := range a.roles {
		if !value.Expires.After(time.Now()) {
			delete(a.roles, key)
		}
	}
	a.roles[sha256.Sum256([]byte(accessToken))] = cachedRole{Subject: idClaims.Subject, Username: idClaims.Username, Founder: founder, Expires: access.Expiry}
	a.mu.Unlock()
	return nil
}
