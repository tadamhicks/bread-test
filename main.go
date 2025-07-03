package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	_ "github.com/lib/pq"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	db     *sql.DB
	tracer trace.Tracer
)

// Book represents a single book entity
type Book struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	Summary string `json:"summary"`
}

// initTracer initializes OpenTelemetry tracing
func initTracer() (*sdktrace.TracerProvider, error) {
	// Create OTLP HTTP exporter
	endpoint := getEnvOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4318")
	log.Printf("OpenTelemetry endpoint: %s", endpoint)
	exporter, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	// Create resource with service information
	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(getEnvOrDefault("OTEL_SERVICE_NAME", "bookapi")),
			semconv.ServiceVersion(getEnvOrDefault("OTEL_SERVICE_VERSION", "1.0.0")),
			semconv.DeploymentEnvironment(getEnvOrDefault("OTEL_ENVIRONMENT", "development")),
			// Add Kubernetes namespace so traces appear in the correct namespace in groundcover
			semconv.K8SNamespaceName(getEnvOrDefault("NAMESPACE", "books")),
			// Add additional Kubernetes metadata for better observability
			semconv.K8SPodName(getEnvOrDefault("POD_NAME", "")),
			semconv.K8SContainerName(getEnvOrDefault("CONTAINER_NAME", "bookapi")),
			semconv.K8SClusterName(getEnvOrDefault("CLUSTER_NAME", "automode-cluster")),
		),
	)
	if err != nil {
		return nil, err
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create tracer
	tracer = tp.Tracer("bookapi")

	return tp, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func booksHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)

	// Add request attributes to span
	span.SetAttributes(
		attribute.String("http.method", r.Method),
		attribute.String("http.url", r.URL.String()),
		attribute.String("http.user_agent", r.Header.Get("User-Agent")),
	)

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
		span.SetAttributes(attribute.String("error", "method_not_allowed"))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetBooks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := tracer.Start(ctx, "handleGetBooks")
	defer span.End()

	id := r.URL.Query().Get("id")
	var err error
	var rows *sql.Rows

	// Add query parameters to span
	span.SetAttributes(
		attribute.String("book.query.id", id),
		attribute.String("operation", "get_books"),
	)

	if id != "" {
		ctx, querySpan := tracer.Start(ctx, "db.query.get_book_by_id")
		querySpan.SetAttributes(
			attribute.String("db.operation", "SELECT"),
			attribute.String("db.table", "books"),
			attribute.String("db.query.id", id),
		)
		rows, err = db.QueryContext(ctx, "SELECT id, title, author, summary FROM books WHERE id = $1", id)
		querySpan.End()
	} else {
		ctx, querySpan := tracer.Start(ctx, "db.query.get_all_books")
		querySpan.SetAttributes(
			attribute.String("db.operation", "SELECT"),
			attribute.String("db.table", "books"),
		)
		rows, err = db.QueryContext(ctx, "SELECT id, title, author, summary FROM books")
		querySpan.End()
	}

	if err != nil {
		span.SetAttributes(
			attribute.String("error", "query_failed"),
			attribute.String("error.message", err.Error()),
		)
		http.Error(w, "Failed to query books", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	ctx, scanSpan := tracer.Start(ctx, "scan_books_results")
	var books []Book
	bookCount := 0

	for rows.Next() {
		var b Book
		if err := rows.Scan(&b.ID, &b.Title, &b.Author, &b.Summary); err != nil {
			scanSpan.SetAttributes(
				attribute.String("error", "scan_failed"),
				attribute.String("error.message", err.Error()),
			)
			scanSpan.End()
			span.SetAttributes(attribute.String("error", "scan_failed"))
			http.Error(w, "Failed to scan book", http.StatusInternalServerError)
			return
		}
		books = append(books, b)
		bookCount++
	}

	scanSpan.SetAttributes(attribute.Int("books.count", bookCount))
	scanSpan.End()

	if err := rows.Err(); err != nil {
		span.SetAttributes(
			attribute.String("error", "rows_error"),
			attribute.String("error.message", err.Error()),
		)
		http.Error(w, "Rows error", http.StatusInternalServerError)
		return
	}

	span.SetAttributes(
		attribute.Int("books.returned", len(books)),
		attribute.String("response.status", "success"),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(books)
}

func handleCreateBook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := tracer.Start(ctx, "handleCreateBook")
	defer span.End()

	span.SetAttributes(attribute.String("operation", "create_book"))

	var book Book
	if err := json.NewDecoder(r.Body).Decode(&book); err != nil {
		span.SetAttributes(
			attribute.String("error", "invalid_request_body"),
			attribute.String("error.message", err.Error()),
		)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Add book details to span
	span.SetAttributes(
		attribute.String("book.title", book.Title),
		attribute.String("book.author", book.Author),
		attribute.Int("book.summary.length", len(book.Summary)),
	)

	ctx, dbSpan := tracer.Start(ctx, "db.insert.book")
	dbSpan.SetAttributes(
		attribute.String("db.operation", "INSERT"),
		attribute.String("db.table", "books"),
	)

	result, err := db.ExecContext(ctx,
		"INSERT INTO books (title, author, summary) VALUES ($1, $2, $3) RETURNING id",
		book.Title, book.Author, book.Summary)
	dbSpan.End()

	if err != nil {
		span.SetAttributes(
			attribute.String("error", "create_failed"),
			attribute.String("error.message", err.Error()),
		)
		http.Error(w, "Failed to create book", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	book.ID = int(id)

	span.SetAttributes(
		attribute.Int("book.id", book.ID),
		attribute.String("response.status", "created"),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(book)
}

func handleUpdateBook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := tracer.Start(ctx, "handleUpdateBook")
	defer span.End()

	id := r.URL.Query().Get("id")
	if id == "" {
		span.SetAttributes(attribute.String("error", "missing_book_id"))
		http.Error(w, "Missing book ID", http.StatusBadRequest)
		return
	}

	span.SetAttributes(
		attribute.String("operation", "update_book"),
		attribute.String("book.id", id),
	)

	var book Book
	if err := json.NewDecoder(r.Body).Decode(&book); err != nil {
		span.SetAttributes(
			attribute.String("error", "invalid_request_body"),
			attribute.String("error.message", err.Error()),
		)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	span.SetAttributes(
		attribute.String("book.title", book.Title),
		attribute.String("book.author", book.Author),
		attribute.Int("book.summary.length", len(book.Summary)),
	)

	ctx, dbSpan := tracer.Start(ctx, "db.update.book")
	dbSpan.SetAttributes(
		attribute.String("db.operation", "UPDATE"),
		attribute.String("db.table", "books"),
		attribute.String("db.query.id", id),
	)

	result, err := db.ExecContext(ctx,
		"UPDATE books SET title = $1, author = $2, summary = $3 WHERE id = $4",
		book.Title, book.Author, book.Summary, id)
	dbSpan.End()

	if err != nil {
		span.SetAttributes(
			attribute.String("error", "update_failed"),
			attribute.String("error.message", err.Error()),
		)
		http.Error(w, "Failed to update book", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	span.SetAttributes(attribute.Int64("db.rows_affected", rowsAffected))

	if rowsAffected == 0 {
		span.SetAttributes(attribute.String("error", "book_not_found"))
		http.Error(w, "Book not found", http.StatusNotFound)
		return
	}

	span.SetAttributes(attribute.String("response.status", "updated"))
	w.WriteHeader(http.StatusOK)
}

func handleDeleteBook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := tracer.Start(ctx, "handleDeleteBook")
	defer span.End()

	id := r.URL.Query().Get("id")
	if id == "" {
		span.SetAttributes(attribute.String("error", "missing_book_id"))
		http.Error(w, "Missing book ID", http.StatusBadRequest)
		return
	}

	span.SetAttributes(
		attribute.String("operation", "delete_book"),
		attribute.String("book.id", id),
	)

	ctx, dbSpan := tracer.Start(ctx, "db.delete.book")
	dbSpan.SetAttributes(
		attribute.String("db.operation", "DELETE"),
		attribute.String("db.table", "books"),
		attribute.String("db.query.id", id),
	)

	result, err := db.ExecContext(ctx, "DELETE FROM books WHERE id = $1", id)
	dbSpan.End()

	if err != nil {
		span.SetAttributes(
			attribute.String("error", "delete_failed"),
			attribute.String("error.message", err.Error()),
		)
		http.Error(w, "Failed to delete book", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	span.SetAttributes(attribute.Int64("db.rows_affected", rowsAffected))

	if rowsAffected == 0 {
		span.SetAttributes(attribute.String("error", "book_not_found"))
		http.Error(w, "Book not found", http.StatusNotFound)
		return
	}

	span.SetAttributes(attribute.String("response.status", "deleted"))
	w.WriteHeader(http.StatusNoContent)
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := tracer.Start(ctx, "healthCheck")
	defer span.End()

	span.SetAttributes(
		attribute.String("operation", "health_check"),
		attribute.String("response.status", "healthy"),
	)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func main() {
	// Initialize OpenTelemetry
	tp, err := initTracer()
	if err != nil {
		log.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	// Connect to Postgres
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.Close()

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	// Create HTTP mux
	mux := http.NewServeMux()
	mux.HandleFunc("/books", booksHandler)
	mux.HandleFunc("/healthz", healthCheckHandler)

	// Wrap mux with OpenTelemetry HTTP middleware
	handler := otelhttp.NewHandler(mux, "bookapi")

	// Create server
	server := &http.Server{
		Addr:    ":9292",
		Handler: handler,
	}

	// Start server in a goroutine
	go func() {
		log.Println("Server is running on :9292")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	log.Println("Shutting down server...")

	// Give outstanding requests 30 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
