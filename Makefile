GO := $(shell which go)
TERRAFORM := $(shell which terraform)
DOCKER := $(shell which docker)
APPNAME ?= chaos-monkey
IMAGE ?= chaos-monkey
TAG ?= 2.2.0

all: bin/$(APPNAME)
.PHONY: clean generate bin/$(APPNAME) image-version cluster-test

generate:
	./hack/update-codegen.sh

bin/$(APPNAME):
	mkdir -p ./bin
	CGO_ENABLED=1 $(GO) build -ldflags "-X main.Version=$(TAG)" -o ./bin/$(APPNAME) ./cmd/chaosmonkey/main.go

docker:
	$(DOCKER) build -t "$(IMAGE):$(TAG)" -f Dockerfile .

image-version:
	@echo $(IMAGE):$(TAG)

image-name:
	@echo $(IMAGE)

image-tag:
	@echo $(TAG)

test:
	$(GO) vet ./...
	CGO_ENABLED=1 $(GO) test -bench=. -v -race ./...

clean:
	$(TERRAFORM) destroy --auto-approve || true
	rm -rf ./bin
	rm -f *-cluster-config

cluster-test: bin/$(APPNAME)
	$(TERRAFORM) apply --auto-approve

# Deploy a simple monitoring stack onto the existing cluster
# This is mainly used for local testing
deploy-monitoring: cluster-test
	kubectl config use-context kind-chaosmonkey-cluster
	kubectl delete namespace monitoring --now=true --ignore-not-found=true
	kubectl create namespace monitoring
	kubectl create secret generic \
		-n monitoring \
		grafana \
		--from-literal=admin-username=grafana \
		--from-literal=admin-password=grafana
	kubectl create configmap \
		-n monitoring \
		grafana-dashboard \
		--from-file=chaos-monkey.json=assets/grafana-dashboard.json
	helm install prometheus prometheus \
		--repo https://prometheus-community.github.io/helm-charts \
		-n monitoring \
		--create-namespace \
		--set "server.persistentVolume.enabled=false" \
		--set "alertmanager.enabled=false" \
		--set "prometheus-node-exporter.enabled=true" \
		--set "prometheus-pushgateway.enabled=false" \
		--set "kube-state-metrics.enabled=true"
	helm install grafana grafana \
		--repo https://grafana.github.io/helm-charts \
		-n monitoring \
		--create-namespace \
		--values ./assets/grafana-values.yaml


