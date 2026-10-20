APP_NAME ?= csm
IMG ?= w7panel-csm:latest
SYSTEM_IMG ?= w7panel-csm-system:ubuntu-24.04
CONTROLLER_GEN ?= controller-gen
CHART_DIR ?= charts/w7panel-csm
NAMESPACE ?= default
CSM_SSH_GATEWAY_NAMESPACE ?= higress-system
CSM_SSH_GATEWAY_NAME ?= higress
GO_CACHE_DIR ?= $(CURDIR)/.cache/go
CRD_BUILD_DIR ?= $(GO_CACHE_DIR)/crd
GO_ENV = GOCACHE=$(GO_CACHE_DIR)/build GOMODCACHE=$(GO_CACHE_DIR)/mod GOTMPDIR=$(GO_CACHE_DIR)/tmp

.PHONY: tidy fmt test build run build-ui docker-build docker-build-system manifests crd install uninstall helm-template helm-lint packageHelm deployHelm prepare-go-cache

prepare-go-cache:
	@mkdir -p $(GO_CACHE_DIR)/build $(GO_CACHE_DIR)/mod $(GO_CACHE_DIR)/tmp

tidy: prepare-go-cache
	$(GO_ENV) go mod tidy

fmt:
	gofmt -w main.go api/v1alpha1/*.go controllers/*.go pkg/*/*.go

test: prepare-go-cache
	$(GO_ENV) go test ./...

build: prepare-go-cache
	@mkdir -p bin
	$(GO_ENV) go build -buildvcs=false -o bin/$(APP_NAME) .

run:
	CSM_OIDC_ALLOW_INSECURE_HTTP=true \
	OIDC_DISCOVERY_URL="http://218.23.2.48:9090/panel-api/v1/oidc/.well-known/openid-configuration" \
	OIDC_AUDIENCE=default CSM_OIDC_CLIENT_ID=default CSM_OIDC_REDIRECT_URI=http://172.16.1.3:8082/callback \
	CSM_PAYMENT_PRODUCT_ID=17057 CSM_PAYMENT_CALLBACK_HOST=localhost \
	POD_NAMESPACE=default CSM_PAYMENT_CREDENTIAL_NAMESPACE=default \
	CSM_SSH_GATEWAY_NAMESPACE="$(CSM_SSH_GATEWAY_NAMESPACE)" CSM_SSH_GATEWAY_NAME="$(CSM_SSH_GATEWAY_NAME)" \
	CSM_SSH_PORT_START=30000 CSM_SSH_PORT_END=30999 \
	CSM_UI_ROOT="$(HOME)/workspace/ai/w7panel-csm/ui/dist" \
	KUBECONFIG="$(HOME)/.kube/218.config" go run .

build-ui:
	cd ui && npm run build

docker-build:
	docker build -t $(IMG) .

docker-build-system:
	docker build -f Dockerfile.system -t $(SYSTEM_IMG) .

manifests: prepare-go-cache
	@mkdir -p $(CRD_BUILD_DIR)
	$(GO_ENV) $(CONTROLLER_GEN) crd:allowDangerousTypes=true paths="./api/..." output:crd:dir=$(CRD_BUILD_DIR)
	mkdir -p $(CHART_DIR)/crds
	cp $(CRD_BUILD_DIR)/csm.w7.cc_csms.yaml $(CRD_BUILD_DIR)/csm.w7.cc_datadisks.yaml config/crd/bases/
	cp config/crd/bases/csm.w7.cc_csms.yaml config/crd/bases/csm.w7.cc_datadisks.yaml $(CHART_DIR)/crds/
	cp pkg/order/csmorders.yaml $(CHART_DIR)/crds/csm.w7.cc_csmorders.yaml

crd: manifests

install:
	kubectl apply -f $(CHART_DIR)/crds

uninstall:
	kubectl delete -f $(CHART_DIR)/crds

helm-template:
	@helm template csm $(CHART_DIR) --include-crds

helm-lint:
	helm lint $(CHART_DIR)

packageHelm:
	helm package $(CHART_DIR)

deployHelm:
	helm upgrade --install w7panel-csm $(CHART_DIR) --namespace $(NAMESPACE) --create-namespace $(HELM_ARGS)
