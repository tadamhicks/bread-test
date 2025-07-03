FROM --platform=linux/amd64 golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

WORKDIR /app

# Copy go mod and sum files
COPY go.mod ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application for AMD64
RUN GOOS=linux GOARCH=amd64 go build -o main .

# Final stage
FROM --platform=linux/amd64 alpine:3.20.2

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/main .

# Expose port 9292
EXPOSE 9292

# Run the executable
CMD ["./main"]
