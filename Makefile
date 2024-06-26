GO := $(shell which go)
TERRAFORM := $(shell which terraform)
APPNAME ?= chaos-monkey

all: bin/$(APPNAME)
.PHONY: clean generate bin/$(APPNAME)

generate:
	./hack/update-codegen.sh

bin/$(APPNAME):
	mkdir -p ./bin
	CGO_ENABLED=1 $(GO) build -o ./bin/$(APPNAME) ./cmd/chaosmonkey/main.go

test:
	$(GO) vet ./...
	CGO_ENABLED=1 $(GO) test -v -race ./...

clean:
	$(TERRAFORM) destroy --auto-approve
	rm -rf ./bin
	rm *-cluster-config

cluster-test: bin/$(APPNAME)
	$(TERRAFORM) apply --auto-approve
