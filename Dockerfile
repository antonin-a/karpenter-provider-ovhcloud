# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o karpenter-ovhcloud ./cmd/controller/

# Final stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /workspace/karpenter-ovhcloud /karpenter-ovhcloud

USER 65532:65532

ENTRYPOINT ["/karpenter-ovhcloud"]
