# =========================
# Config
# =========================
BINS := gcurl gtree gnode gnicstat gwatch gvault gxfer gkafka gb64 gsh

GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0

LDFLAGS := -s -w

# vendor 사용 여부 (기본: 사용)
USE_VENDOR ?= 1

# =========================
# Build metadata
# =========================
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "nogit")
BUILD_DATE := $(shell date "+%Y-%m-%d %H:%M:%S")

LDFLAGS := -s -w \
	-X 'gtools/pkg/version.GitCommit=$(GIT_COMMIT)' \
	-X 'gtools/pkg/version.BuildDate=$(BUILD_DATE)'

# =========================
# 내부 처리
# =========================
ifeq ($(USE_VENDOR),1)
		GOFLAGS := -mod=vendor
else
		GOFLAGS := -mod=mod
endif

BUILD_ENV := CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH)

# =========================
# 공통 빌드 함수
# =========================
define build_bin
		@echo "Building $(1) (vendor=$(USE_VENDOR))..."
		@mkdir -p bin
		@$(BUILD_ENV) go build $(GOFLAGS) -ldflags="$(LDFLAGS)" \
				-o bin/$(1) ./cmd/$(1)
endef

# =========================
# Targets
# =========================

.PHONY: all clean $(BINS)

all: $(BINS)

gcurl:
		$(call build_bin,gcurl)

gtree:
		$(call build_bin,gtree)

gnode:
		$(call build_bin,gnode)

gnicstat:
		$(call build_bin,gnicstat)

gwatch:
		$(call build_bin,gwatch)

gvault:
		$(call build_bin,gvault)

gxfer:
		$(call build_bin,gxfer)

gkafka:
		$(call build_bin,gkafka)

gb64:
		$(call build_bin,gb64)

gsh:
		$(call build_bin,gsh)

clean:
		rm -rf bin/g*
