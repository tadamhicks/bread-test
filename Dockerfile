# Build natively on the host architecture and cross-compile for the target;
# running the Go toolchain under amd64 emulation crashes on Apple silicon.
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# Install build dependencies
RUN apk add --no-cache git

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application for the target platform
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o main .

# Final stage
FROM --platform=linux/amd64 alpine:3.20.2

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/main .

# Expose port 9292
EXPOSE 9292

# Run the executable
CMD ["./main"]
