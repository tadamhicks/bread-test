package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/DataDog/datadog-go/v5/statsd"
	"github.com/opentracing/opentracing-go"
	ddext "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
	ddopentracer "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/opentracer"
	ddtracer "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"

	_ "github.com/lib/pq"
)

var (
	db           *sql.DB
	statsdClient *statsd.Client
)

// Book represents a single book entity
type Book struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	Summary string `json:"summary"`
}

// init initializes the StatsD client
func init() {
	var err error
	// Initialize StatsD client with extended metrics
	host := os.Getenv("DD_AGENT_HOST")
	if host == "" {
		host = "otel-collector" // Default fallback
	}
	port := os.Getenv("DD_DOGSTATSD_PORT")
	if port == "" {
		port = "8125" // Default fallback
	}
	statsdEndpoint := fmt.Sprintf("%s:%s", host, port)

	statsdClient, err = statsd.New(
		statsdEndpoint,
		statsd.WithNamespace("bookapi."), // Add namespace prefix to all metrics
		statsd.WithTags([]string{
			"env:" + os.Getenv("DD_ENV"),
			"service:" + os.Getenv("DD_SERVICE"),
			"version:" + os.Getenv("DD_VERSION"),
		}),
		statsd.WithoutTelemetry(), // Disable internal telemetry to avoid duplicates
	)
	if err != nil {
		log.Fatalf("Failed to create StatsD client: %v", err)
	}
}

// OpenTracingMiddleware is a simple middleware that starts an OpenTracing span
// for each incoming request, sets relevant Datadog tags, and injects the span
// into the request context.
type OpenTracingMiddleware struct {
	handler http.Handler
}

func (mw *OpenTracingMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract any existing span context from the incoming request
	spanCtx, _ := opentracing.GlobalTracer().Extract(
		opentracing.HTTPHeaders,
		opentracing.HTTPHeadersCarrier(r.Header),
	)

	// Start a new span named "http.request" as the child of any extracted span
	span := opentracing.StartSpan(
		"http.request",
		opentracing.ChildOf(spanCtx),
	)
	defer span.Finish()

	// Add Datadog-specific tags
	span.SetTag(ddext.SpanType, ddext.SpanTypeWeb)
	span.SetTag(ddext.ResourceName, r.URL.Path)
	span.SetTag(ddext.HTTPMethod, r.Method)
	span.SetTag(ddext.HTTPURL, r.URL.String())
	span.SetTag(ddext.Component, "booksapi")

	// Add environment, service, version as well (Datadog best practice)
	if env := os.Getenv("DD_ENV"); env != "" {
		span.SetTag("env", env)
	}
	if svc := os.Getenv("DD_SERVICE"); svc != "" {
		span.SetTag("service.name", svc)
	}
	if ver := os.Getenv("DD_VERSION"); ver != "" {
		span.SetTag("service.version", ver)
	}

	// Put this span into the context so downstream handlers can retrieve it
	ctx := opentracing.ContextWithSpan(r.Context(), span)
	r = r.WithContext(ctx)

	// Continue to the next handler
	mw.handler.ServeHTTP(w, r)
}

func booksHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Retrieve span from context if you want to annotate further
	span := opentracing.SpanFromContext(r.Context())
	// Optionally set extra tags
	span.SetTag("endpoint", "/books")

	switch r.Method {
	case http.MethodGet:
		handleGetBooks(w, r)
	case http.MethodPost:
		handleCreateBook(w, r)
	case http.MethodPut:
		handleUpdateBook(w, r)
	case http.MethodDelete:
		handleDeleteBook(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		statsdClient.Incr("books.requests.error", []string{"method:" + r.Method, "error:method_not_allowed"}, 1)
	}

	statsdClient.Timing("books.requests.duration", time.Since(start), []string{"method:" + r.Method}, 1)
}

func handleGetBooks(w http.ResponseWriter, r *http.Request) {
	span := opentracing.SpanFromContext(r.Context())
	id := r.URL.Query().Get("id")
	var err error
	var rows *sql.Rows

	var operation string
	if id != "" {
		operation = "get_by_id"
		span.SetTag("book.id", id)
		span.SetTag("operation", operation)
		rows, err = db.QueryContext(r.Context(), "SELECT id, title, author, summary FROM books WHERE id = $1", id)
		statsdClient.Incr("books.queries.count", []string{"operation:" + operation}, 1)
	} else {
		operation = "get_all"
		span.SetTag("operation", operation)
		rows, err = db.QueryContext(r.Context(), "SELECT id, title, author, summary FROM books")
		statsdClient.Incr("books.queries.count", []string{"operation:" + operation}, 1)
	}

	if err != nil {
		http.Error(w, "Failed to query books", http.StatusInternalServerError)
		statsdClient.Incr("books.queries.errors", []string{"error:db_query_failed"}, 1)
		return
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(&b.ID, &b.Title, &b.Author, &b.Summary); err != nil {
			http.Error(w, "Failed to scan book", http.StatusInternalServerError)
			statsdClient.Incr("books.queries.errors", []string{"error:scan_failed"}, 1)
			return
		}
		books = append(books, b)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Rows error", http.StatusInternalServerError)
		statsdClient.Incr("books.queries.errors", []string{"error:rows_error"}, 1)
		return
	}

	statsdClient.Incr("books.queries.success", []string{"operation:" + operation}, 1)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(books)
}

func handleCreateBook(w http.ResponseWriter, r *http.Request) {
	span := opentracing.SpanFromContext(r.Context())
	span.SetTag("operation", "create_book")

	var book Book
	if err := json.NewDecoder(r.Body).Decode(&book); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		statsdClient.Incr("books.operations.errors", []string{"operation:create", "error:invalid_body"}, 1)
		return
	}

	result, err := db.ExecContext(r.Context(),
		"INSERT INTO books (title, author, summary) VALUES ($1, $2, $3) RETURNING id",
		book.Title, book.Author, book.Summary)
	if err != nil {
		http.Error(w, "Failed to create book", http.StatusInternalServerError)
		statsdClient.Incr("books.operations.errors", []string{"operation:create", "error:db_insert_failed"}, 1)
		return
	}

	id, _ := result.LastInsertId()
	book.ID = int(id)
	statsdClient.Incr("books.operations.success", []string{"operation:create"}, 1)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(book)
}

func handleUpdateBook(w http.ResponseWriter, r *http.Request) {
	span := opentracing.SpanFromContext(r.Context())
	span.SetTag("operation", "update_book")

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing book ID", http.StatusBadRequest)
		statsdClient.Incr("books.operations.errors", []string{"operation:update", "error:missing_id"}, 1)
		return
	}
	span.SetTag("book.id", id)

	var book Book
	if err := json.NewDecoder(r.Body).Decode(&book); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		statsdClient.Incr("books.operations.errors", []string{"operation:update", "error:invalid_body"}, 1)
		return
	}

	result, err := db.ExecContext(r.Context(),
		"UPDATE books SET title = $1, author = $2, summary = $3 WHERE id = $4",
		book.Title, book.Author, book.Summary, id)
	if err != nil {
		http.Error(w, "Failed to update book", http.StatusInternalServerError)
		statsdClient.Incr("books.operations.errors", []string{"operation:update", "error:db_update_failed"}, 1)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "Book not found", http.StatusNotFound)
		statsdClient.Incr("books.operations.errors", []string{"operation:update", "error:not_found"}, 1)
		return
	}

	statsdClient.Incr("books.operations.success", []string{"operation:update"}, 1)
	w.WriteHeader(http.StatusOK)
}

func handleDeleteBook(w http.ResponseWriter, r *http.Request) {
	span := opentracing.SpanFromContext(r.Context())
	span.SetTag("operation", "delete_book")

	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing book ID", http.StatusBadRequest)
		statsdClient.Incr("books.operations.errors", []string{"operation:delete", "error:missing_id"}, 1)
		return
	}
	span.SetTag("book.id", id)

	result, err := db.ExecContext(r.Context(), "DELETE FROM books WHERE id = $1", id)
	if err != nil {
		http.Error(w, "Failed to delete book", http.StatusInternalServerError)
		statsdClient.Incr("books.operations.errors", []string{"operation:delete", "error:db_delete_failed"}, 1)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "Book not found", http.StatusNotFound)
		statsdClient.Incr("books.operations.errors", []string{"operation:delete", "error:not_found"}, 1)
		return
	}

	statsdClient.Incr("books.operations.success", []string{"operation:delete"}, 1)
	w.WriteHeader(http.StatusNoContent)
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	statsdClient.Incr("health.checks", []string{"status:ok"}, 1)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func main() {
	// Initialize Datadog tracer using OpenTracing bridging
	t := ddopentracer.New(
		ddtracer.WithEnv(os.Getenv("DD_ENV")),
		ddtracer.WithServiceName(os.Getenv("DD_SERVICE")),
		ddtracer.WithServiceVersion(os.Getenv("DD_VERSION")),
		ddtracer.WithRuntimeMetrics(),        // Enable runtime metrics
		ddtracer.WithProfilerEndpoints(true), // Enable profiler endpoints
	)
	opentracing.SetGlobalTracer(t)
	defer ddtracer.Stop()

	// Connect to Postgres
	var err error
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/books", booksHandler)
	mux.HandleFunc("/healthz", healthCheckHandler)

	// Wrap the mux with our OpenTracing middleware
	wrappedMux := &OpenTracingMiddleware{handler: mux}

	log.Println("Server is running on :9292")
	// No httptrace.WrapHandler here; we’re manually handling the tracing
	http.ListenAndServe(":9292", wrappedMux)
}
