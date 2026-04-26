APP ?= jcemb
OUT_DIR ?= .
PKG ?= .
GOOS ?= $(shell go env GOOS)
GOLANGCI_LINT ?= golangci-lint

ifeq ($(OS),Windows_NT)
MKDIR_P = powershell -NoProfile -Command "New-Item -ItemType Directory -Force -Path '$(OUT_DIR)' | Out-Null"
else
MKDIR_P = mkdir -p "$(OUT_DIR)"
endif

ifeq ($(GOOS),windows)
EXE_EXT ?= .exe
else
EXE_EXT ?=
endif

OUT ?= $(OUT_DIR)/$(APP)$(EXE_EXT)

.PHONY: all lint tidy build

all: lint tidy build

lint:
	$(GOLANGCI_LINT) run ./...

tidy:
	go mod tidy

build:
	$(MKDIR_P)
	go build -o "$(OUT)" $(PKG)
