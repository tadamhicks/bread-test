CREATE DATABASE bookdb;

\c bookdb;

CREATE TABLE IF NOT EXISTS books (
    id SERIAL PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    author VARCHAR(255) NOT NULL,
    summary BYTEA
);

-- Sample data
INSERT INTO books (title, author, summary) VALUES
    ('The Go Programming Language', 'Alan A. A. Donovan', 'A comprehensive guide to Go programming'::bytea),
    ('Clean Code', 'Robert C. Martin', 'A handbook of agile software craftsmanship'::bytea);
