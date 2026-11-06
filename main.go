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
	"github.com/w7panel/w7panel-csm/pkg/sshidentity"
	"github.com/w7panel/w7panel-csm/pkg/sshjump"
	"golang.org/x/crypto/ssh"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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

func kubernetesConfig() (*rest.Config, error) {
	if path := os.Getenv("KUBECONFIG"); path != "" {
		return clientcmd.BuildConfigFromFlags("", path)
	}
	return rest.InClusterConfig()
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
	if len(validation.IsDNS1123Label(gatewayNamespace)) != 0 || len(validation.IsDNS1123Label(gatewayName)) != 0 {
		return fmt.Errorf("SSH Gateway namespace and name must be DNS labels")
	}
	gatewayServiceName := os.Getenv("CSM_SSH_GATEWAY_SERVICE_NAME")
	if gatewayServiceName != "" && len(validation.IsDNS1123Label(gatewayServiceName)) != 0 {
		return fmt.Errorf("SSH Gateway Service name must be a DNS label")
	}
	mockPay := os.Getenv("MOCK_PAY") == "true"
	productID := os.Getenv("CSM_PAYMENT_PRODUCT_ID")
	callbackHost := os.Getenv("CSM_PAYMENT_CALLBACK_HOST")
	pricingNamespace, err := required("POD_NAMESPACE")
	if err != nil {
		return err
	}
	if !mockPay {
		if callbackHost == "" {
			return fmt.Errorf("CSM_PAYMENT_CALLBACK_HOST is required")
		}
	}
	terminalOrigin := os.Getenv("CSM_TERMINAL_ALLOWED_ORIGIN")
	if terminalOrigin != "" {
		u, err := url.Parse(terminalOrigin)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return fmt.Errorf("CSM_TERMINAL_ALLOWED_ORIGIN must be an HTTPS origin")
		}
	}
	if !mockPay {
		callbackURL, err := url.Parse("https://" + callbackHost)
		if err != nil || callbackURL.Hostname() == "" || callbackURL.Path != "" || callbackURL.RawQuery != "" || callbackURL.User != nil {
			return fmt.Errorf("CSM_PAYMENT_CALLBACK_HOST must be a hostname")
		}
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
	jumpAddr := os.Getenv("CSM_SSH_JUMP_ADDR")
	jumpSuffix := os.Getenv("CSM_SSH_DOMAIN_SUFFIX")
	var jumpPort int64
	if jumpAddr != "" {
		jumpPort, err = strconv.ParseInt(os.Getenv("CSM_SSH_JUMP_PORT"), 10, 32)
		if err != nil || jumpPort < 1 || jumpPort > 65535 || len(validation.IsDNS1123Subdomain(jumpSuffix)) != 0 {
			return fmt.Errorf("invalid SSH jump port or domain suffix")
		}
	}
	uiRoot := os.Getenv("CSM_UI_ROOT")
	if uiRoot == "" {
		uiRoot = "/var/run/ko/ui/dist"
	}
	if stat, err := os.Stat(filepath.Join(uiRoot, "index.html")); err != nil || stat.IsDir() {
		return fmt.Errorf("CSM_UI_ROOT must contain index.html: %s", uiRoot)
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	cfg, err := kubernetesConfig()
	if err != nil {
		return err
	}
	if cfg.BearerToken == "" {
		return fmt.Errorf("Kubernetes bearer token is required")
	}
	if kubeconfig == "" && cfg.BearerTokenFile == "" {
		return fmt.Errorf("Kubernetes projected service account token file is required")
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	resources, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}
	transport, err := rest.TransportFor(cfg)
	if err != nil {
		return err
	}
	if kubeconfig == "" {
		transport = integration.ServiceAccountTransport(cfg.BearerTokenFile, transport)
	}
	kubeHTTP := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	authenticator, err := integration.NewAuthenticator(context.Background(), discovery, audience, oidcClientID, oidcRedirectURI, os.Getenv("CSM_OIDC_PUBLIC_AUTHORIZATION_URL"))
	if err != nil {
		return err
	}
	authenticator.SSHPublicHost = os.Getenv("CSM_SSH_PUBLIC_HOST")
	authenticator.EnsureNamespace = func(ctx context.Context, name string) error {
		_, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
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
	host := &controllers.CsmReconciler{Client: mgr.GetClient(), Scheme: scheme, GatewayNamespace: gatewayNamespace, GatewayName: gatewayName, GatewayServiceName: gatewayServiceName, PortStart: int32(portStart), PortEnd: int32(portEnd), JumpPort: int32(jumpPort), SSHDomainSuffix: jumpSuffix}
	var userCA ssh.Signer
	if jumpAddr != "" {
		userCA, err = sshidentity.LoadCA(context.Background(), kube, pricingNamespace)
		if err != nil {
			return err
		}
		host.SSHCAPublicKey = ssh.MarshalAuthorizedKey(userCA.PublicKey())
	}
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
	coupons := &order.KubeCoupons{Kube: kube, Namespace: pricingNamespace}
	pricing := order.KubePricing{Kube: kube, Store: store, Coupons: coupons, Namespace: pricingNamespace, ProductID: productID}
	var gateway order.Gateway = order.W7Gateway{NotifyURL: "https://" + callbackHost + httpapi.Prefix + "/order/notify", Credential: integration.AccountCredential(kube, resources), SaveToken: integration.AccountTokenSaver(resources), Quote: pricing.Quote}
	if mockPay {
		gateway = order.MockGateway{Quote: pricing.Quote}
	}
	server := httpapi.NewServer(authenticator.Authenticate, backend, order.Service{Gateway: gateway, Store: store, Coupons: coupons, AutoConfirm: mockPay}, uiRoot)
	server.Pricing = pricing
	server.Coupons = coupons
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
	if jumpAddr != "" && os.Getenv("CSM_SSH_PREPARE_ONLY") != "true" {
		signer, err := sshjump.LoadHostKey(context.Background(), kube, pricingNamespace)
		if err != nil {
			return err
		}
		jump := &sshjump.Server{Client: mgr.GetAPIReader(), Suffix: jumpSuffix, Signer: signer, CA: userCA}
		if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error { return jump.Serve(ctx, jumpAddr) })); err != nil {
			return err
		}
	}
	return mgr.Start(ctrl.SetupSignalHandler())
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
