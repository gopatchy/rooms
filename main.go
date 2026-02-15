package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	texttemplate "text/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
	"google.golang.org/api/idtoken"
)

//go:embed schema.sql
var schema string

var (
	htmlTemplates *template.Template
	jsTemplates   *texttemplate.Template
)

func main() {
	for _, key := range []string{"PGCONN", "CLIENT_ID", "CLIENT_SECRET", "ADMINS"} {
		if os.Getenv(key) == "" {
			log.Fatalf("%s environment variable is required", key)
		}
	}

	db, err := sql.Open("postgres", os.Getenv("PGCONN"))
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

	htmlTemplates = template.Must(template.New("").ParseGlob("static/*.html"))
	jsTemplates = texttemplate.Must(texttemplate.New("").ParseGlob("static/*.js"))

	http.HandleFunc("GET /{$}", serveHTML("index.html"))
	http.HandleFunc("GET /admin", serveHTML("admin.html"))
	http.HandleFunc("GET /app.js", serveJS("app.js"))
	http.HandleFunc("GET /admin.js", serveJS("admin.js"))
	http.HandleFunc("POST /auth/google/callback", handleGoogleCallback)
	http.HandleFunc("GET /api/admin/check", handleAdminCheck)
	http.HandleFunc("GET /api/trips", handleListTrips(db))
	http.HandleFunc("POST /api/trips", handleCreateTrip(db))
	http.HandleFunc("DELETE /api/trips/{tripID}", handleDeleteTrip(db))
	http.HandleFunc("POST /api/trips/{tripID}/admins", handleAddTripAdmin(db))
	http.HandleFunc("DELETE /api/trips/{tripID}/admins/{adminID}", handleRemoveTripAdmin(db))
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

func templateData() map[string]any {
	return map[string]any{
		"env": envMap(),
	}
}

func envMap() map[string]string {
	m := map[string]string{}
	for _, e := range os.Environ() {
		if parts := strings.SplitN(e, "=", 2); len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

func serveHTML(name string) http.HandlerFunc {
	t := htmlTemplates.Lookup(name)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html")
		t.Execute(w, templateData())
	}
}

func serveJS(name string) http.HandlerFunc {
	t := jsTemplates.Lookup(name)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "application/javascript")
		t.Execute(w, templateData())
	}
}

func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	credential := r.FormValue("credential")
	if credential == "" {
		http.Error(w, "missing credential", http.StatusBadRequest)
		return
	}

	payload, err := idtoken.Validate(context.Background(), credential, os.Getenv("CLIENT_ID"))
	if err != nil {
		log.Println("failed to validate token:", err)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	email := payload.Claims["email"].(string)

	profile := map[string]any{
		"email":   email,
		"name":    payload.Claims["name"],
		"picture": payload.Claims["picture"],
		"token":   signEmail(email),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

func signEmail(email string) string {
	h := hmac.New(sha256.New, []byte(os.Getenv("CLIENT_SECRET")))
	h.Write([]byte(email))
	sig := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(email)) + "." + sig
}

func authorize(r *http.Request) (string, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	emailBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	email := string(emailBytes)
	if signEmail(email) != token {
		return "", false
	}
	return email, true
}

func isAdmin(email string) bool {
	for _, a := range strings.Split(os.Getenv("ADMINS"), ",") {
		if strings.TrimSpace(a) == email {
			return true
		}
	}
	return false
}

func requireAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	email, ok := authorize(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	if !isAdmin(email) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", false
	}
	return email, true
}

func handleAdminCheck(w http.ResponseWriter, r *http.Request) {
	email, ok := authorize(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"admin": isAdmin(email)})
}

func handleListTrips(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireAdmin(w, r); !ok {
			return
		}
		rows, err := db.Query(`
			SELECT t.id, t.name, COALESCE(
				json_agg(json_build_object('id', ta.id, 'email', ta.email)) FILTER (WHERE ta.id IS NOT NULL),
				'[]'
			)
			FROM trips t
			LEFT JOIN trip_admins ta ON ta.trip_id = t.id
			GROUP BY t.id, t.name
			ORDER BY t.id`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type tripAdmin struct {
			ID    int64  `json:"id"`
			Email string `json:"email"`
		}
		type trip struct {
			ID     int64       `json:"id"`
			Name   string      `json:"name"`
			Admins []tripAdmin `json:"admins"`
		}

		var trips []trip
		for rows.Next() {
			var t trip
			var adminsJSON string
			if err := rows.Scan(&t.ID, &t.Name, &adminsJSON); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.Unmarshal([]byte(adminsJSON), &t.Admins)
			trips = append(trips, t)
		}
		if trips == nil {
			trips = []trip{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(trips)
	}
}

func handleCreateTrip(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireAdmin(w, r); !ok {
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		var id int64
		err := db.QueryRow("INSERT INTO trips (name) VALUES ($1) RETURNING id", body.Name).Scan(&id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": id, "name": body.Name})
	}
}

func handleDeleteTrip(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireAdmin(w, r); !ok {
			return
		}
		tripID, err := strconv.ParseInt(r.PathValue("tripID"), 10, 64)
		if err != nil {
			http.Error(w, "invalid trip ID", http.StatusBadRequest)
			return
		}
		result, err := db.Exec("DELETE FROM trips WHERE id = $1", tripID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			http.Error(w, "trip not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAddTripAdmin(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireAdmin(w, r); !ok {
			return
		}
		tripID, err := strconv.ParseInt(r.PathValue("tripID"), 10, 64)
		if err != nil {
			http.Error(w, "invalid trip ID", http.StatusBadRequest)
			return
		}
		var body struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
			http.Error(w, "email is required", http.StatusBadRequest)
			return
		}
		var id int64
		err = db.QueryRow("INSERT INTO trip_admins (trip_id, email) VALUES ($1, $2) RETURNING id", tripID, body.Email).Scan(&id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": id, "email": body.Email})
	}
}

func handleRemoveTripAdmin(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requireAdmin(w, r); !ok {
			return
		}
		adminID, err := strconv.ParseInt(r.PathValue("adminID"), 10, 64)
		if err != nil {
			http.Error(w, "invalid admin ID", http.StatusBadRequest)
			return
		}
		result, err := db.Exec("DELETE FROM trip_admins WHERE id = $1", adminID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			http.Error(w, "trip admin not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
