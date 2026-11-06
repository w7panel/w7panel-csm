package integration

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/w7panel/w7panel-csm/pkg/order"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestAccountCredentialUsesMatchingAccountAndSiteLicense(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "alice.w7-config", Namespace: "accounts"}, Data: map[string][]byte{
		"thirdparty_cd_token": []byte("alice-token"), "cluster_id": []byte("cluster"),
		"cd_token_expire_time": []byte("1893456000"), "userinfo": []byte(`{"open_id":"alice-openid"}`),
	}}
	kube := kubefake.NewClientset(secret)
	license := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "w7panel.w7.com/v1alpha1", "kind": "License", "metadata": map[string]any{"name": "license"}, "spec": map[string]any{"appId": "site-app"}}}
	resources := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), license)
	credential, err := AccountCredential(kube, resources, "accounts")(context.Background(), "csm-alice")
	if err != nil || credential.Account != "alice" || credential.Token != "alice-token" || credential.OpenID != "alice-openid" || credential.AppID != "site-app" || credential.ClusterID != "cluster" || !credential.ExpiresAt.Equal(time.Unix(1893456000, 0)) {
		t.Fatalf("credential = %+v, err = %v", credential, err)
	}
	if _, err := AccountCredential(kube, resources, "accounts")(context.Background(), "csm-bob"); err == nil {
		t.Fatal("missing account secret accepted")
	}
	if _, err := AccountCredential(kube, resources, "accounts")(context.Background(), "default"); err == nil {
		t.Fatal("untrusted namespace accepted")
	}
}

func TestAccountCredentialReadsUserLicense(t *testing.T) {
	account := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "alice.w7-config", Namespace: "accounts"}, Data: map[string][]byte{
		"thirdparty_cd_token": []byte("token"),
	}}
	user := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "w7panel.w7.com/v1alpha1", "kind": "User",
		"metadata": map[string]any{"name": "founder"},
		"spec":     map[string]any{"license": map[string]any{"appId": "user-app"}},
	}}
	resources := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{userGVR: "UserList"}, user)
	credential, err := AccountCredential(kubefake.NewClientset(account), resources, "accounts")(context.Background(), "csm-alice")
	if err != nil || credential.AppID != "user-app" {
		t.Fatalf("user license credential = %+v, err = %v", credential, err)
	}
}

func TestAccountTokenSaverUpdatesAccountSecret(t *testing.T) {
	account := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "alice.w7-config", Namespace: "accounts"}}
	kube := kubefake.NewClientset(account)
	expiresAt := time.Unix(1893456000, 0)
	if err := AccountTokenSaver(kube, "accounts")(context.Background(), "csm-alice", "new-token", expiresAt); err != nil {
		t.Fatal(err)
	}
	secret, err := kube.CoreV1().Secrets("accounts").Get(context.Background(), "alice.w7-config", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["thirdparty_cd_token"]) != "new-token" || string(secret.Data["cd_token_expire_time"]) != "1893456000" {
		t.Fatalf("saved token data = %v", secret.Data)
	}
}

func TestValidateQuoteTermsRejectsOverwrittenSize(t *testing.T) {
	choice := order.Request{Kind: "csm", Action: "buy", Name: "host", CPU: 2, MemoryGi: 4, StorageGi: 40, BandwidthMbps: 5, Quantity: 1, Unit: "month"}
	params := url.Values{"hour": {"720"}, "cpu": {"2"}, "memory": {"4"}, "storage": {"40"}, "bandwidth": {"5"}}
	if err := validateQuoteTerms(choice, params, "42", 1200); err != nil {
		t.Fatal(err)
	}
	choice.Quantity, choice.Unit = 3, "day"
	params.Set("hour", "72")
	if err := validateQuoteTerms(choice, params, "42", 1200); err != nil {
		t.Fatal(err)
	}
	params.Set("cpu", "1")
	if err := validateQuoteTerms(choice, params, "42", 1200); err == nil {
		t.Fatal("mismatched CPU quote was accepted")
	}
}
