APP_NAME ?= csm
IMG ?= w7panel-csm:latest
SYSTEM_IMG ?= w7panel-csm-system:ubuntu-24.04
KO_DOCKER_REPO ?= docker.cnb.cool/i0358/w7panel-csm
KO_DEFAULTBASEIMAGE ?= ccr.ccs.tencentyun.com/afan-public/ubuntu:24.04-offlineui
KO_PLATFORM ?= linux/amd64
KO_IMAGE_TAG ?= $(if $(CNB_BRANCH),$(CNB_BRANCH),$(shell git describe --tags --exact-match 2>/dev/null || git branch --show-current))
CONTROLLER_GEN ?= controller-gen
CHART_DIR ?= charts/w7panel-csm
NAMESPACE ?= default
CSM_SSH_GATEWAY_NAMESPACE ?= higress-system
CSM_SSH_GATEWAY_NAME ?= gateway
CSM_SSH_GATEWAY_SERVICE_NAME ?= higress-gateway
CSM_SSH_PUBLIC_HOST ?= csm.fan.b2.sz.w7.com
GO_CACHE_DIR ?= $(CURDIR)/.cache/go
CRD_BUILD_DIR ?= $(GO_CACHE_DIR)/crd
GO_ENV = GOCACHE=$(GO_CACHE_DIR)/build GOMODCACHE=$(GO_CACHE_DIR)/mod GOTMPDIR=$(GO_CACHE_DIR)/tmp

.PHONY: tidy fmt test build run build-ui ko-build docker-build docker-build-system manifests crd install uninstall helm-template helm-lint packageHelm deployHelm prepare-go-cache

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
	MOCK_PAY=true CSM_OIDC_ALLOW_INSECURE_HTTP=true \
	OIDC_DISCOVERY_URL="http://218.23.2.48:9090/panel-api/v1/oidc/.well-known/openid-configuration" \
	OIDC_AUDIENCE=default CSM_OIDC_CLIENT_ID=default CSM_OIDC_REDIRECT_URI=http://172.16.1.3:8082/callback \
	POD_NAMESPACE=default \
	CSM_SSH_GATEWAY_NAMESPACE="$(CSM_SSH_GATEWAY_NAMESPACE)" CSM_SSH_GATEWAY_NAME="$(CSM_SSH_GATEWAY_NAME)" CSM_SSH_GATEWAY_SERVICE_NAME="$(CSM_SSH_GATEWAY_SERVICE_NAME)" \
	CSM_SSH_PUBLIC_HOST="$(CSM_SSH_PUBLIC_HOST)" \
	CSM_SSH_PORT_START=30000 CSM_SSH_PORT_END=30999 \
	CSM_UI_ROOT="$(HOME)/workspace/ai/w7panel-csm/ui/dist" \
	KUBECONFIG="$(HOME)/.kube/218.config" go run .

build-ui:
	cd ui && npm run build

ko-build: prepare-go-cache build-ui
	@mkdir -p kodata/ui/dist
	@rsync -a --delete ui/dist/ kodata/ui/dist/
	@test -n "$(KO_IMAGE_TAG)" || { echo "Set KO_IMAGE_TAG to a non-empty image tag" >&2; exit 1; }
	@if [ -n "$${CNB_TOKEN:-}" ]; then ko login docker.cnb.cool -u cnb -p "$$CNB_TOKEN"; fi
	$(GO_ENV) KO_DOCKER_REPO="$(KO_DOCKER_REPO)" KO_DEFAULTBASEIMAGE="$(KO_DEFAULTBASEIMAGE)" ko build --bare --tags="$(KO_IMAGE_TAG)" --tag-only --sbom=none --platform="$(KO_PLATFORM)" .

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
	helm upgrade --install w7panel-csm $(CHART_DIR) --set image.tag=$(KO_IMAGE_TAG) --namespace $(NAMESPACE) --create-namespace $(HELM_ARGS)
