# Build stage
FROM golang:1.23-alpine AS builder

# Allow Go to download newer toolchain if required by dependencies
ENV GOTOOLCHAIN=auto

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary (GOARCH is set by buildx for multi-arch builds)
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o karpenter-ovhcloud ./cmd/controller/

# Final stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/karpenter-ovhcloud /karpenter-ovhcloud

USER 65532:65532

ENTRYPOINT ["/karpenter-ovhcloud"]
