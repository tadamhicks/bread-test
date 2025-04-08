# Book API Service

A Go-based REST API service for managing books, built with Gorilla Mux and PostgreSQL.

## Project Structure

```
.
├── README.md
├── go.mod
├── main.go
├── db/
│   └── schema.sql
├── Dockerfile
└── k8s/
    ├── app-deployment.yaml
    └── postgres-deployment.yaml
```

## Prerequisites

- Go 1.20 or later
- Podman
- Kubernetes cluster (for deployment)
- PostgreSQL (for local development)

## Local Development

1. Initialize the Go module:
```bash
go mod tidy
```

2. Run the application locally:
```bash
go run main.go
```

## Building and Running with Podman

### Building the Image

The project uses a multi-architecture Dockerfile to support both ARM (M1/M2) and Intel-based systems:

```bash
# Build for AMD64 (Intel) architecture
podman build --platform linux/amd64 -t bookapi:latest .

# For local testing on M1/M2 Mac
podman build --platform linux/arm64 -t bookapi:latest .
```

### Running Locally with Podman

1. Create a pod for the application:
```bash
podman pod create --name bookapp -p 9292:9292 -p 5432:5432
```

2. Start PostgreSQL:
```bash
podman run --pod bookapp --name postgres -d \
  -e POSTGRES_PASSWORD=postgres \
  -v ./db/schema.sql:/docker-entrypoint-initdb.d/init.sql:Z \
  postgres:latest
```

3. Start the application:
```bash
podman run --pod bookapp --name bookapi -d \
  -e DATABASE_URL="postgres://postgres:postgres@localhost:5432/bookdb?sslmode=disable" \
  bookapi:latest
```

### Managing Images

To prepare for deployment:

1. Tag the image:
```bash
podman tag bookapi:latest <registry-url>/bookapi:latest
```

2. Push to registry:
```bash
podman push <registry-url>/bookapi:latest
```

## Kubernetes Deployment

1. Update the image reference in `k8s/app-deployment.yaml`:
```yaml
image: <registry-url>/bookapi:latest
```

2. Deploy PostgreSQL:
```bash
kubectl apply -f k8s/postgres-deployment.yaml
```

3. Deploy the application:
```bash
kubectl apply -f k8s/app-deployment.yaml
```

4. Verify the deployment:
```bash
kubectl get pods
kubectl get services
```

## API Endpoints

### Books API

#### GET Endpoints

- `GET /books` - Retrieve all books
- `GET /books?id=<id>` - Retrieve a specific book by ID

#### POST Endpoint

- `POST /books` - Create a new book

```json
{
  "title": "Book Title",
  "author": "Author Name",
  "summary": "Book summary"
}
```

#### PUT Endpoint

- `PUT /books?id=<id>` - Update an existing book

```json
{
  "title": "Updated Title",
  "author": "Updated Author",
  "summary": "Updated summary"
}
```

#### DELETE Endpoint

- `DELETE /books?id=<id>` - Delete a book

### Health Check

- `GET /healthz` - Service health check

## Environment Variables

- `PORT` - Server port (default: 9292)
- `DATABASE_URL` - PostgreSQL connection string (default: postgres://postgres:postgres@localhost:5432/bookdb?sslmode=disable)

## Resource Configuration

The application is configured with the following resource limits:

### API Service
- CPU Limit: 500m
- Memory Limit: 512Mi
- CPU Request: 200m
- Memory Request: 256Mi

### PostgreSQL

- CPU Limit: 1000m
- Memory Limit: 1Gi
- CPU Request: 500m
- Memory Request: 512Mi

## Observability

The application is instrumented with Datadog APM and StatsD metrics, using OpenTelemetry Collector for data collection and export.

### Metrics

The following metrics are collected:

- `bookapi.books.get.duration` - Histogram of request duration
- `bookapi.books.get.by_id` - Counter for individual book requests
- `bookapi.books.get.all` - Counter for all books requests

### Traces

The application creates traces for:

- Book retrieval operations
- Database queries

### OpenTelemetry Collector Setup

1. Create the configuration:

```bash
kubectl apply -f k8s/otel-collector-config.yaml
```

1. Create secrets (replace with your actual values):

```bash
kubectl create secret generic otel-collector-secrets \
  --from-literal=DD_API_KEY=6708a085cef1cb9c10c4f53e5b32c064 \
  --from-literal=DT_API_TOKEN=your_dynatrace_token \
  --from-literal=DT_API_URL=your_dynatrace_url
```

1. Deploy the collector:

```bash
kubectl apply -f k8s/otel-collector-deployment.yaml
```

### Local Testing

For local testing with Podman:

```bash
# Run the OpenTelemetry Collector
podman run -d --name otel-collector \
  -v ./k8s/otel-collector-config.yaml:/etc/otel/config.yaml:Z \
  -p 8126:8126 -p 8125:8125/udp \
  -e DD_API_KEY=your_datadog_api_key \
  -e DT_API_TOKEN=your_dynatrace_token \
  -e DT_API_URL=your_dynatrace_url \
  otel/opentelemetry-collector-contrib:latest
```

### Load Testing with k6

The project includes automated load testing using k6, which generates consistent traffic to help visualize traces and metrics in Datadog.

1. Deploy the k6 test configuration:

```bash
kubectl apply -f k8s/k6-configmap.yaml -n bookapi
kubectl apply -f k8s/k6-cronjob.yaml -n bookapi
```

This will:

- Run k6 tests every minute via a CronJob
- Generate ~100 requests per minute with:
  - 40% GET /books (list all)
  - 30% POST /books (create new)
  - 30% PUT /books/{id} (update)

Monitor the tests:


```bash
# View scheduled jobs
kubectl get cronjobs -n bookapi

# View running/completed jobs
kubectl get jobs -n bookapi

# Check test results
kubectl logs -n bookapi -l job-name=k6-load-test-<timestamp>
```

