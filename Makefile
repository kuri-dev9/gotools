# =========================
# Config
# =========================
BINS := gcurl gtree gwatch

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

gwatch:
		$(call build_bin,gwatch)

clean:
		rm -rf bin/*
