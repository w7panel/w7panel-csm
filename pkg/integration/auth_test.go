package integration

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

func TestAuthenticateVerifiesAudienceAndTenant(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keys := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}
	a := &Authenticator{verifier: oidc.NewVerifier("https://issuer.example", keys, &oidc.Config{ClientID: "csm-api", SupportedSigningAlgs: []string{oidc.RS256}}), idVerifier: oidc.NewVerifier("https://issuer.example", keys, &oidc.Config{ClientID: "csm-ui", SupportedSigningAlgs: []string{oidc.RS256}}), roles: make(map[[32]byte]cachedRole)}
	sign := func(audience, subject, role string) string {
		claims := map[string]any{"iss": "https://issuer.example", "aud": audience, "exp": time.Now().Add(time.Hour).Unix(), "sub": subject, "role": role}
		if audience != "csm-api" {
			claims["preferred_username"] = "alice"
		}
		payload, _ := json.Marshal(claims)
		encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(payload)
		digest := sha256.Sum256([]byte(encoded))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		return encoded + "." + base64.RawURLEncoding.EncodeToString(signature)
	}
	access := sign("csm-api", "user-1", "")
	if _, err := a.Authenticate(context.Background(), access); err == nil {
		t.Fatal("role-less access token was accepted before login")
	}
	if err := a.cacheIDToken(context.Background(), access, sign("csm-ui", "user-1", "founder")); err != nil {
		t.Fatal(err)
	}
	identity, err := a.Authenticate(context.Background(), access)
	if err != nil || identity.Namespace != "csm-alice" || !identity.Founder {
		t.Fatalf("identity = %+v, %v", identity, err)
	}
	if _, err := a.Authenticate(context.Background(), sign("wrong-audience", "user-1", "founder")); err == nil {
		t.Fatal("wrong audience was accepted")
	}
	if err := a.cacheIDToken(context.Background(), access, sign("csm-ui", "another-user", "founder")); err == nil {
		t.Fatal("mismatched ID token was accepted")
	}
}

func TestTrustedOIDCURL(t *testing.T) {
	for _, item := range []struct {
		raw     string
		trusted bool
	}{{"http://w7panel-offline.default.svc:8000/oidc", true}, {"http://127.0.0.1:8000/oidc", true}, {"http://id.example/oidc", false}, {"https://id.example/oidc", true}} {
		u, err := url.Parse(item.raw)
		if err != nil || trustedOIDCURL(u) != item.trusted {
			t.Errorf("trustedOIDCURL(%q) = %v, parse error %v", item.raw, trustedOIDCURL(u), err)
		}
	}
	t.Setenv("CSM_OIDC_ALLOW_INSECURE_HTTP", "true")
	u, _ := url.Parse("http://218.23.2.48:9090/oidc")
	if !trustedOIDCURL(u) {
		t.Fatal("explicit local HTTP setting did not allow the configured OIDC host")
	}
}

func TestAuthenticatorAllowsHTTPForLocalRun(t *testing.T) {
	t.Setenv("CSM_OIDC_ALLOW_INSECURE_HTTP", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host + "/oidc"
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer": base, "jwks_uri": base + "/jwks",
			"authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token",
		})
	}))
	defer server.Close()
	if _, err := NewAuthenticator(context.Background(), server.URL+"/oidc/.well-known/openid-configuration", "default", "default", "http://localhost:8082/callback", ""); err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticatorUsesPublicAuthorizationWithInternalIssuer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host + "/oidc"
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer": base, "jwks_uri": base + "/jwks",
			"authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token",
		})
	}))
	defer server.Close()
	if _, err := NewAuthenticator(context.Background(), server.URL+"/oidc/.well-known/openid-configuration", "default", "default", "https://csm.example/callback", ""); err == nil {
		t.Fatal("HTTP browser authorization accepted without local setting")
	}
	publicURL := "https://auth.example/oidc/authorize"
	a, err := NewAuthenticator(context.Background(), server.URL+"/oidc/.well-known/openid-configuration", "default", "default", "https://csm.example/callback", publicURL)
	if err != nil || a.authorizationEndpoint != publicURL {
		t.Fatalf("authenticator = %+v, %v", a, err)
	}
	if _, err := NewAuthenticator(context.Background(), server.URL+"/oidc/.well-known/openid-configuration", "default", "default", "https://csm.example/callback", "https://auth.example/wrong/authorize"); err == nil {
		t.Fatal("mismatched public authorization path accepted")
	}
}

func TestCommerceLoginConfigAlias(t *testing.T) {
	a := &Authenticator{authorizationEndpoint: "https://id.example/authorize", clientID: "csm-ui", redirectURI: "https://csm.example/callback"}
	req := httptest.NewRequest(http.MethodGet, "/csm-api/v1/login/config", nil)
	out := httptest.NewRecorder()
	a.AuthRoutes(http.NotFoundHandler()).ServeHTTP(out, req)
	if out.Code != http.StatusOK {
		t.Fatalf("status = %d", out.Code)
	}
	var response map[string]string
	if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["authorizationEndpoint"] != a.authorizationEndpoint || response["clientId"] != a.clientID || response["redirectUri"] != a.redirectURI {
		t.Fatalf("login config = %v", response)
	}
}
