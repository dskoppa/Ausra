FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum main.go repositories.go webhooks.go ./
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /ausra .

FROM python:3.12-slim-bookworm
ARG TERRAFORM_VERSION=1.16.6
ARG TOFU_VERSION=1.13.1
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl unzip openssh-client git && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir ansible-core
RUN set -eux; \
    ARCH="$(dpkg --print-architecture)"; \
    case "$ARCH" in amd64|arm64) ;; *) exit 1 ;; esac; \
    curl -fsSLo /tmp/terraform.zip "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${ARCH}.zip"; \
    curl -fsSLo /tmp/terraform_SHA256SUMS "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_SHA256SUMS"; \
    (cd /tmp && grep " terraform_${TERRAFORM_VERSION}_linux_${ARCH}.zip$" terraform_SHA256SUMS | sed "s/terraform_${TERRAFORM_VERSION}_linux_${ARCH}.zip/terraform.zip/" | sha256sum -c -); \
    unzip -p /tmp/terraform.zip terraform > /usr/local/bin/terraform; chmod +x /usr/local/bin/terraform; \
    curl -fsSLo /tmp/tofu.zip "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_linux_${ARCH}.zip"; \
    curl -fsSLo /tmp/tofu_SHA256SUMS "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_SHA256SUMS"; \
    (cd /tmp && grep " tofu_${TOFU_VERSION}_linux_${ARCH}.zip$" tofu_SHA256SUMS | sed "s/tofu_${TOFU_VERSION}_linux_${ARCH}.zip/tofu.zip/" | sha256sum -c -); \
    unzip -p /tmp/tofu.zip tofu > /usr/local/bin/tofu; chmod +x /usr/local/bin/tofu; \
    rm -f /tmp/terraform.zip /tmp/terraform_SHA256SUMS /tmp/tofu.zip /tmp/tofu_SHA256SUMS
RUN useradd -r -m -u 10001 ausra && mkdir /data /workspace && chown ausra:ausra /data /workspace
COPY --from=build /ausra /usr/local/bin/ausra
USER ausra
ENV AUSRA_ADDR=0.0.0.0:8080 AUSRA_WORKSPACE=/workspace AUSRA_DATA_DIR=/data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/ausra"]
