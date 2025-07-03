# Book API with OpenTelemetry Instrumentation

A Go-based REST API for managing books with comprehensive OpenTelemetry tracing instrumentation.

## Features

- **HTTP Server Tracing**: Automatic instrumentation of HTTP requests/responses
- **Database Tracing**: PostgreSQL query instrumentation with detailed spans
- **Custom Business Logic Spans**: Manual instrumentation for book operations
- **Error Tracking**: Comprehensive error attributes in traces
- **Performance Metrics**: Request duration, database operation timing
- **Graceful Shutdown**: Proper cleanup of telemetry resources
- **Groundcover Integration**: Direct integration with Groundcover inCloud managed endpoint

## OpenTelemetry Integration for Groundcover

### Required Resource Attributes for Groundcover Searchability

For OpenTelemetry traces to appear properly in Groundcover and be searchable by cluster, namespace, and workload, the following resource attributes **MUST** be configured:

#### Essential Kubernetes Metadata
```go
// These semantic conventions map to groundcover's search dimensions
semconv.K8SClusterName(clusterName)     // Maps to: cluster field in groundcover
semconv.K8SNamespaceName(namespace)     // Maps to: namespace field in groundcover  
semconv.K8SPodName(podName)             // Maps to: podName field in groundcover
semconv.K8SContainerName(containerName) // Maps to: containerName field in groundcover
```

#### Service Identification
```go
semconv.ServiceName(serviceName)        // Maps to: workload field in groundcover
semconv.ServiceVersion(version)         // Used for version tracking
semconv.DeploymentEnvironment(env)      // Maps to: env field in groundcover
```

### Environment Variable Configuration

The application uses `getEnvOrDefault()` to allow all attributes to be overridden via environment variables. Instead of hardcoding values, you can use Kubernetes APIs to dynamically populate these values:

| Environment Variable | OpenTelemetry Attribute | Groundcover Field | Required | Default |
|---------------------|------------------------|------------------|----------|---------|
| `CLUSTER_NAME` | `k8s.cluster.name` | `cluster` | **YES** | `automode-cluster` |
| `NAMESPACE` | `k8s.namespace.name` | `namespace` | **YES** | `books` |
| `POD_NAME` | `k8s.pod.name` | `podName` | **YES** | Auto-injected |
| `CONTAINER_NAME` | `k8s.container.name` | `containerName` | **YES** | `bookapi` |
| `OTEL_SERVICE_NAME` | `service.name` | `workload` | **YES** | `bookapi` |
| `OTEL_SERVICE_VERSION` | `service.version` | N/A | No | `1.0.0` |
| `OTEL_ENVIRONMENT` | `deployment.environment` | `env` | No | `development` |

### Kubernetes API-Based Metadata (Recommended)

Instead of hardcoding cluster and container metadata, use Kubernetes APIs for dynamic discovery:

#### Available via Kubernetes APIs

| Metadata | Downward API | ConfigMap | AWS Metadata | K8s API Call | Recommendation |
|----------|-------------|-----------|-------------|--------------|----------------|
| **NAMESPACE** | ✅ `metadata.namespace` | ✅ | ❌ | ✅ | **Use Downward API** |
| **POD_NAME** | ✅ `metadata.name` | ❌ | ❌ | ✅ | **Use Downward API** |
| **NODE_NAME** | ✅ `spec.nodeName` | ❌ | ✅ | ✅ | **Use Downward API** |
| **CLUSTER_NAME** | ❌ | ✅ | ✅ (EKS) | ✅ | **Use ConfigMap** |
| **CONTAINER_NAME** | ❌ | ✅ | ❌ | ❌ | **Use ConfigMap or hardcode** |
| **SERVICE_NAME** | ❌ | ✅ | ❌ | ❌ | **Use environment variable** |

#### Option 1: ConfigMap Approach (Recommended)
```yaml
# Create cluster-info ConfigMap once per cluster
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-info
  namespace: books
data:
  cluster-name: "automode-cluster"  # Update per environment
  environment: "production"
---
# Reference in deployment
env:
- name: CLUSTER_NAME
  valueFrom:
    configMapKeyRef:
      name: cluster-info
      key: cluster-name
```

#### Option 2: Downward API (Limited)
```yaml
env:
# Available via Downward API
- name: NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
- name: POD_NAME
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: NODE_NAME
  valueFrom:
    fieldRef:
      fieldPath: spec.nodeName

# Not available via Downward API - use ConfigMap instead
- name: CONTAINER_NAME
  value: "bookapi"  # Must be specified manually
```

#### Option 3: AWS Metadata Service (EKS-specific)
```go
// Add to main.go for dynamic AWS discovery
func getAWSClusterName() string {
    // Call AWS metadata service to get cluster name
    resp, err := http.Get("http://169.254.169.254/latest/meta-data/cluster-name")
    if err != nil {
        return "unknown-cluster"
    }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)
    return string(body)
}
```

#### Option 4: Kubernetes API Discovery
```yaml
# Requires ServiceAccount with cluster read permissions
apiVersion: v1
kind: ServiceAccount
metadata:
  name: bookapi-cluster-reader
---
# Use in deployment
spec:
  serviceAccountName: bookapi-cluster-reader
```

### Complete Deployment Example

The `k8s/app-deployment.yaml` shows the complete configuration:

```yaml
env:
# OpenTelemetry service identification (required for groundcover)
- name: OTEL_SERVICE_NAME
  value: "bookapi"                    # → workload field
- name: OTEL_SERVICE_VERSION  
  value: "1.0.0"
- name: OTEL_ENVIRONMENT
  value: "production"                 # → env field

# Kubernetes metadata (REQUIRED for proper groundcover organization)
- name: NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace   # → namespace field
- name: POD_NAME
  valueFrom:
    fieldRef:
      fieldPath: metadata.name        # → podName field  
- name: CONTAINER_NAME
  value: "bookapi"                    # → containerName field
- name: CLUSTER_NAME
  value: "automode-cluster"           # → cluster field
```

### Groundcover Search Examples

With proper resource attributes, traces will be searchable in groundcover:

| Search Filter | Example Values | Purpose |
|--------------|----------------|---------|
| **Cluster** | `automode-cluster` | Filter by EKS cluster |
| **Namespace** | `books` | Filter by Kubernetes namespace |
| **Workload** | `bookapi` | Filter by service/application |
| **Source** | `opentelemetry` | Filter by telemetry source |
| **Trace ID** | `8907aaca208201935f9989c511de21fe` | Find specific trace |

### Collector-Based Metadata Enrichment (Alternative Approach)

**Instead of configuring metadata in every application**, you can configure the OpenTelemetry collector to automatically add Kubernetes metadata to **ANY** application's telemetry:

#### Key Processors for Metadata Enrichment

| Processor | Purpose | Adds |
|-----------|---------|------|
| **`k8sattributes`** | Kubernetes API integration | `k8s.namespace.name`, `k8s.pod.name`, `k8s.container.name`, etc. |
| **`resourcedetection`** | Environment detection | `k8s.cluster.name`, `cloud.provider`, `cloud.platform` |
| **`resource`** | Static attributes | Custom attributes, fallback values |

#### Enhanced Collector Configuration

```yaml
processors:
  k8sattributes:
    auth_type: "serviceAccount"
    extract:
      metadata:
        - k8s.namespace.name    # → namespace field
        - k8s.pod.name         # → podName field  
        - k8s.container.name   # → containerName field
        - k8s.deployment.name
        - k8s.node.name

  resourcedetection:
    detectors: [kubernetes]
    kubernetes:
      cluster_name: "automode-cluster"  # → cluster field

  resource:
    attributes:
      - key: k8s.cluster.name
        value: "automode-cluster"
        action: upsert  # Only add if missing
```

#### Benefits of Collector-Based Approach

✅ **Zero application changes** - works with any OpenTelemetry-enabled app  
✅ **Centralized configuration** - one place to manage metadata  
✅ **Automatic enrichment** - no developer configuration needed  
✅ **Consistent metadata** - all apps get the same attributes  
✅ **Legacy app support** - works with apps that can't be modified  

#### Example: Minimal Application Configuration

```yaml
# Application only needs basic OTEL config
env:
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: "http://otel-collector:4318"
- name: OTEL_SERVICE_NAME
  value: "my-app"

# NO Kubernetes metadata needed!
# Collector automatically adds:
# - k8s.namespace.name: "books"
# - k8s.pod.name: "my-app-xxx-yyy"  
# - k8s.container.name: "my-app"
# - k8s.cluster.name: "automode-cluster"
```

#### Files for Collector-Based Approach

- `k8s/otel-collector-enhanced-configmap.yaml` - Enhanced collector config with k8sattributes
- `k8s/otel-collector-enhanced-deployment.yaml` - Deployment with RBAC permissions
- `k8s/minimal-app-example.yaml` - Example app relying on collector enrichment

### Verification

To verify your traces have the correct attributes, check that groundcover queries return:

```json
{
  "cluster": "automode-cluster",     // ✅ Required
  "namespace": "books",              // ✅ Required  
  "workload": "bookapi",             // ✅ Required
  "podName": "bookapi-xxx-yyy",      // ✅ Required
  "containerName": "bookapi",        // ✅ Required
  "source": "opentelemetry",         // ✅ Required
  "env": "production"                // Optional but recommended
}
```

## OpenTelemetry Instrumentation Details

This application includes comprehensive OpenTelemetry tracing:

### HTTP Tracing
- Automatic span creation for all HTTP requests
- Request/response headers and status codes
- User agent and URL tracking
- HTTP method classification

### Database Tracing  
- SQL query instrumentation
- Database operation timing
- Table and operation type tracking
- Connection pool metrics

### Custom Application Spans
- Book CRUD operation tracing
- JSON parsing and validation spans
- Result scanning and processing spans
- Error handling with detailed attributes

### Trace Attributes
- `operation`: Type of business operation (get_books, create_book, etc.)
- `book.id`, `book.title`, `book.author`: Book-specific metadata
- `db.operation`, `db.table`: Database operation details
- `books.count`, `books.returned`: Result set information
- `error` and `error.message`: Detailed error information

## Setup Instructions

### 1. Install Dependencies

First, clean up the go.mod file and install correct dependencies:

```bash
# Remove problematic lines from go.mod
sed -i '/go.opentelemetry.io\/otel\/semconv\/v1.17.0/d' go.mod

# Install OpenTelemetry dependencies
go get go.opentelemetry.io/contrib/instrumentation/database/sql/otelsql
go get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
go get go.opentelemetry.io/otel
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp
go get go.opentelemetry.io/otel/propagation
go get go.opentelemetry.io/otel/sdk
go get go.opentelemetry.io/otel/semconv/v1.17.0
go get go.opentelemetry.io/otel/trace

# Clean up dependencies
go mod tidy
```

### 2. Environment Variables

Set the following environment variables:

```bash
# Database connection
export DATABASE_URL="postgres://user:password@localhost:5432/bookdb?sslmode=disable"

# OpenTelemetry configuration
export OTEL_SERVICE_NAME="bookapi"
export OTEL_SERVICE_VERSION="1.0.0"
export OTEL_ENVIRONMENT="development"
export OTEL_EXPORTER_OTLP_ENDPOINT="http://localhost:4318"
```

## Deployment Options

### Option 1: Local Development with Docker Compose

For local testing with PostgreSQL and OpenTelemetry collector:

```bash
# Start PostgreSQL and OpenTelemetry collector
docker-compose up -d postgres otel-collector

# Run the application locally
go run main.go
```

This setup will:
- Start PostgreSQL database on port 5432
- Start OpenTelemetry collector that forwards data to Groundcover
- Send traces, logs, and metrics to `experiments.platform-dev.grcv.io`

### Option 2: Kubernetes Deployment

#### Deploy OpenTelemetry Collector

```bash
# Deploy the OpenTelemetry collector configured for Groundcover
kubectl apply -f k8s/otel-collector-configmap.yaml
kubectl apply -f k8s/otel-collector-deployment.yaml
```

#### Deploy the Book API

```bash
# Deploy PostgreSQL and the Book API
kubectl apply -f k8s/postgres-deployment.yaml
kubectl apply -f k8s/app-deployment.yaml
```

#### Update Application Environment

Ensure your bookapi deployment has the correct OTLP endpoint:

```yaml
env:
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: "http://otel-collector:4318"
- name: OTEL_SERVICE_NAME
  value: "bookapi"
- name: OTEL_ENVIRONMENT
  value: "production"  # or your environment name
```

### Option 3: Direct Integration (No Collector)

For direct integration with Groundcover inCloud managed endpoint, configure your application directly:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT="https://experiments.platform-dev.grcv.io"
export OTEL_EXPORTER_OTLP_HEADERS="apikey=3trT8IbWi2y04rWKp8ygZSLzmqtCzjsS"
```

## Groundcover Integration

This project is configured to send telemetry data directly to Groundcover's inCloud managed endpoint:

- **Endpoint**: `experiments.platform-dev.grcv.io`
- **Authentication**: API key `3trT8IbWi2y04rWKp8ygZSLzmqtCzjsS`
- **Protocols**: OTLP HTTP/gRPC over TLS
- **Data Types**: Traces, Logs, and Metrics

### Viewing Data in Groundcover

1. Access your Groundcover dashboard at `https://experiments.platform-dev.grcv.io`
2. Navigate to the **Traces** section to see distributed traces
3. Check **Logs** for application log correlation
4. View **Metrics** for custom application metrics
5. Use **APM** features for service topology and performance insights

### Custom Resource Attributes

The collector automatically adds these attributes to all telemetry:
- `service.instance.id`: "otel-collector"
- `service.name`: "bookapi"
- `service.version`: "1.0.0"
- `deployment.environment`: "development" (or configured environment)

## API Endpoints

All endpoints are automatically instrumented with OpenTelemetry:

### Get Books
```bash
# Get all books
curl http://localhost:9292/books

# Get specific book
curl http://localhost:9292/books?id=1
```

### Create Book
```bash
curl -X POST http://localhost:9292/books \
  -H "Content-Type: application/json" \
  -d '{"title":"Go Programming","author":"John Doe","summary":"Learn Go programming"}'
```

### Update Book
```bash
curl -X PUT http://localhost:9292/books?id=1 \
  -H "Content-Type: application/json" \
  -d '{"title":"Advanced Go","author":"John Doe","summary":"Advanced Go concepts"}'
```

### Delete Book
```bash
curl -X DELETE http://localhost:9292/books?id=1
```

### Health Check
```bash
curl http://localhost:9292/healthz
```

## Trace Examples

### Successful Book Creation
```
bookapi: handleCreateBook
├── db.insert.book (INSERT INTO books...)
└── response: 201 Created
```

### Book Query with Database Scan
```
bookapi: handleGetBooks
├── db.query.get_all_books (SELECT * FROM books)
├── scan_books_results (3 books processed)
└── response: 200 OK
```

### Error Handling
```
bookapi: handleGetBooks
├── db.query.get_book_by_id (SELECT * FROM books WHERE id=999)
├── error: "book_not_found"
└── response: 404 Not Found
```

## Configuration Files

- `otel-collector-config.yml`: OpenTelemetry collector configuration for Groundcover
- `k8s/otel-collector-configmap.yaml`: Kubernetes ConfigMap for collector config
- `k8s/otel-collector-deployment.yaml`: Kubernetes deployment for collector
- `docker-compose.yml`: Local development environment

## Troubleshooting

### Common Issues

1. **Connection to Groundcover**: Verify internet connectivity and API key
2. **Missing Traces**: Check service name and exporter configuration
3. **Database Connection**: Verify DATABASE_URL and PostgreSQL availability
4. **Collector Logs**: Check collector logs for export errors

### Debug Mode

Enable debug logging:
```bash
export OTEL_LOG_LEVEL=debug
export OTEL_TRACES_EXPORTER=console  # Output traces to console for debugging
```

### Verify Collector Status

```bash
# Check collector health
curl http://localhost:13133/

# View collector metrics
curl http://localhost:8888/metrics

# Access zpages for detailed collector info
open http://localhost:55679/debug/servicez
```

## Security Notes

- The API key `3trT8IbWi2y04rWKp8ygZSLzmqtCzjsS` is configured for the experiments environment
- For production deployments, use Kubernetes secrets to manage API keys
- Ensure network policies allow outbound HTTPS traffic to Groundcover endpoints

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

