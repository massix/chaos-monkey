GO := $(shell which go)
TERRAFORM := $(shell which terraform)
DOCKER := $(shell which docker)
APPNAME ?= chaos-monkey
IMAGE ?= chaos-monkey
TAG ?= 2.0.4

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
	CGO_ENABLED=1 $(GO) test -v -race ./...

clean:
	$(TERRAFORM) destroy --auto-approve || true
	rm -rf ./bin
	rm -f *-cluster-config

cluster-test: bin/$(APPNAME)
	$(TERRAFORM) apply --auto-approve

