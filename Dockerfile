FROM --platform=$BUILDPLATFORM alpine:3.20.2 AS builder

# Install Go
RUN apk add --no-cache go

WORKDIR /app

# Copy go mod and sum files
COPY go.mod ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application with cross-platform support
ARG TARGETARCH
RUN GOOS=linux GOARCH=$TARGETARCH go build -o main .

# Final stage
FROM --platform=$TARGETPLATFORM alpine:3.20.2

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/main .

# Expose port 9292
EXPOSE 9292

# Run the executable
CMD ["./main"]
