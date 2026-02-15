package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
)

//go:embed schema.sql
var schema string

func main() {
	dsn := os.Getenv("PGCONN")
	if dsn == "" {
		log.Fatal("PGCONN environment variable is required")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	log.Println("connected to database")

	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("failed to apply schema: %v", err)
	}

	http.Handle("/", http.FileServer(http.Dir("static")))

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			http.Error(w, "db unhealthy", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
