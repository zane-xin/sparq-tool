SHELL := /bin/sh
GO ?= go
VERSION ?=
GOCACHE ?= $(CURDIR)/.cache/go-build
export GOCACHE

.PHONY: test build vet check-release release

test:
	$(GO) test ./...

build:
	$(GO) build -o bin/sparq-tool ./cmd/sparq-tool

vet:
	$(GO) vet ./...

# 该入口可在独立仓库执行，始终忽略外层 go.work。
check-release:
	python3 scripts/check-release.py check

release:
	python3 scripts/check-release.py release --version "$(VERSION)"
