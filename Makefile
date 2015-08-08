VERSION := $(shell git describe --tags --always --dirty)
REVISION := $(shell git rev-parse --sq HEAD)
COVERAGEOUT := coverage.out
.DEFAULT_GOAL := check

ifndef GOBIN
GOBIN := $(shell echo "$${GOPATH%%:*}/bin")
endif

LINT := $(GOBIN)/golint

$(LINT): ; @go get -v github.com/golang/lint/golint

.PHONY: build
build:
	@go build -ldflags "-X main.VersionString $(VERSION) -X main.RevisionString $(REVISION)" ./...

.PHONY: install
install:
	@go install -ldflags "-X main.VersionString $(VERSION) -X main.RevisionString $(REVISION)" ./...

.PHONY: clean
clean:
	@go clean -i ./...
	@rm -f $(COVERAGEOUT)

.PHONY: vet
vet:
	@go vet ./...

.PHONY: lint
lint: $(LINT)
	@exit $$($(LINT) ./... | tee /dev/tty | wc -l)

.PHONY: cover
cover:
	@go test -coverprofile=$(COVERAGEOUT)
	@go tool cover -html=$(COVERAGEOUT)

.PHONY: test
test:
	@go test

.PHONY: check
check: vet lint test
