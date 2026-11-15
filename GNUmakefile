.PHONY: build install fmt lint generate test testacc

build:
	go build -v ./...

install:
	go install .

fmt:
	gofmt -w .
	terraform fmt -recursive examples

lint:
	go vet ./...
	terraform fmt -check -recursive examples

generate:
	terraform fmt -recursive examples
	go tool tfplugindocs generate --provider-name allot

test:
	go test -race -timeout 5m ./...

testacc:
	TF_ACC=1 TF_ACC_PROVIDER_NAMESPACE=byteshuttle go test -race -count=1 -timeout 30m ./...
