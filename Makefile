CONTROLLER_GEN ?= controller-gen
CHART_DIR := charts/w7panel-csm

.PHONY: test build-ui manifests helm-template

test:
	go test ./...

build-ui:
	cd ui && npm run build

manifests:
	$(CONTROLLER_GEN) crd:allowDangerousTypes=true paths="./api/..." output:crd:dir=config/crd/bases
	mkdir -p $(CHART_DIR)/crds
	cp config/crd/bases/*.yaml $(CHART_DIR)/crds/
	cp pkg/order/csmorders.yaml $(CHART_DIR)/crds/csm.w7.cc_csmorders.yaml

helm-template:
	helm template csm $(CHART_DIR)
