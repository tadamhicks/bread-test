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
	_ "github.com/lib/pq"
	httptrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/net/http"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

var statsdClient *statsd.Client

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
	statsdClient, err = statsd.New(statsdEndpoint,
		statsd.WithNamespace("bookapi."), // Add namespace prefix to all metrics
		statsd.WithTags([]string{
			"env:" + os.Getenv("DD_ENV"),
			"service:" + os.Getenv("DD_SERVICE"),
			"version:" + os.Getenv("DD_VERSION"),
		}),
		statsd.WithoutTelemetry(), // Disable internal telemetry to avoid duplicate metrics
	)
	if err != nil {
		log.Fatalf("Failed to create StatsD client: %v", err)
	}
}

type Book struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	Summary string `json:"summary"`
}

var db *sql.DB

func booksHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	span, ctx := tracer.StartSpanFromContext(r.Context(), "http.request")
	defer span.Finish()

	switch r.Method {
	case http.MethodGet:
		handleGetBooks(w, r.WithContext(ctx), span)
	case http.MethodPost:
		handleCreateBook(w, r.WithContext(ctx), span)
	case http.MethodPut:
		handleUpdateBook(w, r.WithContext(ctx), span)
	case http.MethodDelete:
		handleDeleteBook(w, r.WithContext(ctx), span)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		statsdClient.Incr("books.requests.error", []string{"method:" + r.Method, "error:method_not_allowed"}, 1)
	}

	statsdClient.Timing("books.requests.duration", time.Since(start), []string{"method:" + r.Method}, 1)
}

func handleGetBooks(w http.ResponseWriter, r *http.Request, span tracer.Span) {
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

func handleCreateBook(w http.ResponseWriter, r *http.Request, span tracer.Span) {
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

func handleUpdateBook(w http.ResponseWriter, r *http.Request, span tracer.Span) {
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

func handleDeleteBook(w http.ResponseWriter, r *http.Request, span tracer.Span) {
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
	// Start tracer with runtime metrics enabled
	tracer.Start(
		tracer.WithEnv(os.Getenv("DD_ENV")),
		tracer.WithServiceName(os.Getenv("DD_SERVICE")),
		tracer.WithServiceVersion(os.Getenv("DD_VERSION")),
		tracer.WithRuntimeMetrics(),        // Enable runtime metrics collection
		tracer.WithProfilerEndpoints(true), // Enable profiling endpoints
	)
	defer tracer.Stop()

	var err error
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/books", booksHandler)
	mux.HandleFunc("/healthz", healthCheckHandler)

	log.Println("Server is running on :9292")
	http.ListenAndServe(":9292", httptrace.WrapHandler(mux, os.Getenv("DD_SERVICE"), "books-api"))
}
