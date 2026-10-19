package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	api "github.com/w7panel/w7panel-csm/api/v1alpha1"
	"github.com/w7panel/w7panel-csm/controllers"
	"github.com/w7panel/w7panel-csm/pkg/execshell"
	"github.com/w7panel/w7panel-csm/pkg/httpapi"
	"github.com/w7panel/w7panel-csm/pkg/integration"
	"github.com/w7panel/w7panel-csm/pkg/order"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func required(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func run() error {
	discovery, err := required("OIDC_DISCOVERY_URL")
	if err != nil {
		return err
	}
	audience, err := required("OIDC_AUDIENCE")
	if err != nil {
		return err
	}
	oidcClientID, err := required("CSM_OIDC_CLIENT_ID")
	if err != nil {
		return err
	}
	oidcRedirectURI, err := required("CSM_OIDC_REDIRECT_URI")
	if err != nil {
		return err
	}
	gatewayNamespace, err := required("CSM_SSH_GATEWAY_NAMESPACE")
	if err != nil {
		return err
	}
	gatewayName, err := required("CSM_SSH_GATEWAY_NAME")
	if err != nil {
		return err
	}
	productID, err := required("CSM_PAYMENT_PRODUCT_ID")
	if err != nil {
		return err
	}
	callbackHost, err := required("CSM_PAYMENT_CALLBACK_HOST")
	if err != nil {
		return err
	}
	quoteURL, err := required("CSM_PAYMENT_QUOTE_URL")
	if err != nil {
		return err
	}
	quoteToken, err := required("CSM_PAYMENT_QUOTE_TOKEN")
	if err != nil {
		return err
	}
	credentialSecretName, err := required("CSM_PAYMENT_CREDENTIAL_SECRET_NAME")
	if err != nil {
		return err
	}
	if len(validation.IsDNS1123Subdomain(credentialSecretName)) != 0 {
		return fmt.Errorf("CSM_PAYMENT_CREDENTIAL_SECRET_NAME must be a DNS subdomain")
	}
	terminalOrigin := os.Getenv("CSM_TERMINAL_ALLOWED_ORIGIN")
	if terminalOrigin != "" {
		u, err := url.Parse(terminalOrigin)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return fmt.Errorf("CSM_TERMINAL_ALLOWED_ORIGIN must be an HTTPS origin")
		}
	}
	callbackURL, err := url.Parse("https://" + callbackHost)
	if err != nil || callbackURL.Hostname() == "" || callbackURL.Path != "" || callbackURL.RawQuery != "" || callbackURL.User != nil {
		return fmt.Errorf("CSM_PAYMENT_CALLBACK_HOST must be a hostname")
	}
	parsedQuote, err := url.Parse(quoteURL)
	if err != nil || parsedQuote.Scheme != "https" || parsedQuote.Host == "" {
		return fmt.Errorf("CSM_PAYMENT_QUOTE_URL must be HTTPS")
	}
	if len(validation.IsDNS1123Label(gatewayNamespace)) != 0 || len(validation.IsDNS1123Label(gatewayName)) != 0 {
		return fmt.Errorf("SSH Gateway namespace and name must be DNS labels")
	}
	portStart, err := strconv.ParseInt(os.Getenv("CSM_SSH_PORT_START"), 10, 32)
	if err != nil {
		return fmt.Errorf("CSM_SSH_PORT_START: %w", err)
	}
	portEnd, err := strconv.ParseInt(os.Getenv("CSM_SSH_PORT_END"), 10, 32)
	if err != nil {
		return fmt.Errorf("CSM_SSH_PORT_END: %w", err)
	}
	if portStart < 1 || portEnd > 65535 || portStart > portEnd {
		return fmt.Errorf("invalid SSH port range")
	}
	uiRoot := os.Getenv("CSM_UI_ROOT")
	if uiRoot == "" {
		uiRoot = "/app/ui/dist"
	}
	if stat, err := os.Stat(filepath.Join(uiRoot, "index.html")); err != nil || stat.IsDir() {
		return fmt.Errorf("CSM_UI_ROOT must contain index.html: %s", uiRoot)
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	if cfg.BearerToken == "" {
		return fmt.Errorf("Kubernetes service account bearer token is required")
	}
	if cfg.BearerTokenFile == "" {
		return fmt.Errorf("Kubernetes projected service account token file is required")
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	transport, err := rest.TransportFor(cfg)
	if err != nil {
		return err
	}
	kubeHTTP := &http.Client{Transport: integration.ServiceAccountTransport(cfg.BearerTokenFile, transport), Timeout: 15 * time.Second}
	authenticator, err := integration.NewAuthenticator(context.Background(), discovery, audience, oidcClientID, oidcRedirectURI)
	if err != nil {
		return err
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return err
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		return err
	}
	if err := api.AddToScheme(scheme); err != nil {
		return err
	}
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	if err := controllers.EnsureOfficialSystemTemplate(context.Background(), mgr.GetClient()); err != nil {
		return err
	}
	host := &controllers.CsmReconciler{Client: mgr.GetClient(), Scheme: scheme, GatewayNamespace: gatewayNamespace, GatewayName: gatewayName, PortStart: int32(portStart), PortEnd: int32(portEnd)}
	if err := host.SetupWithManager(mgr); err != nil {
		return err
	}
	if err := (&controllers.DataDiskReconciler{Client: mgr.GetClient(), Scheme: scheme}).SetupWithManager(mgr); err != nil {
		return err
	}
	if err := controllers.SetupCsmTemplateController(mgr); err != nil {
		return err
	}
	runner := execshell.KubernetesRunner{Kube: kube, Config: cfg}
	if err := controllers.SetupPreinstallExecutionController(mgr, runner); err != nil {
		return err
	}
	if err := controllers.SetupUpgradeExecutionController(mgr, runner); err != nil {
		return err
	}

	backend := httpapi.KubeBackend{BaseURL: cfg.Host, Token: cfg.BearerToken, Client: kubeHTTP}
	store := order.KubeStore{BaseURL: cfg.Host, Token: cfg.BearerToken, Client: kubeHTTP}
	gateway := order.W7Gateway{NotifyURL: "https://" + callbackHost + httpapi.Prefix + "/order/notify", Credential: integration.SecretCredential(kube, credentialSecretName), Quote: integration.RemoteQuote(quoteURL, quoteToken, productID)}
	server := httpapi.NewServer(authenticator.Authenticate, backend, order.Service{Gateway: gateway, Store: store}, uiRoot)
	server.KubeURL, server.KubeToken, server.KubeClient = cfg.Host, cfg.BearerToken, kubeHTTP
	addr := os.Getenv("CSM_HTTP_ADDR")
	if addr == "" {
		addr = ":8082"
	}
	httpServer := &http.Server{Addr: addr, Handler: authenticator.AuthRoutes(integration.TerminalAuthorization(server.Handler(), terminalOrigin)), ReadHeaderTimeout: 10 * time.Second}
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdown)
		}()
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	})); err != nil {
		return err
	}
	return mgr.Start(ctrl.SetupSignalHandler())
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
