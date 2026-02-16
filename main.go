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
	http.HandleFunc("GET /trip/{tripID}", serveHTML("trip.html"))
	http.HandleFunc("GET /trip.js", serveJS("trip.js"))
	http.HandleFunc("GET /api/trips/{tripID}", handleGetTrip(db))
	http.HandleFunc("PATCH /api/trips/{tripID}", handleUpdateTrip(db))
	http.HandleFunc("GET /api/trips/{tripID}/students", handleListStudents(db))
	http.HandleFunc("POST /api/trips/{tripID}/students", handleCreateStudent(db))
	http.HandleFunc("DELETE /api/trips/{tripID}/students/{studentID}", handleDeleteStudent(db))
	http.HandleFunc("POST /api/trips/{tripID}/students/{studentID}/parents", handleAddParent(db))
	http.HandleFunc("DELETE /api/trips/{tripID}/students/{studentID}/parents/{parentID}", handleRemoveParent(db))
	http.HandleFunc("GET /api/trips/{tripID}/constraints", handleListConstraints(db))
	http.HandleFunc("POST /api/trips/{tripID}/constraints", handleCreateConstraint(db))
	http.HandleFunc("DELETE /api/trips/{tripID}/constraints/{constraintID}", handleDeleteConstraint(db))
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

func isTripAdmin(db *sql.DB, email string, tripID int64) bool {
	var exists bool
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM trip_admins WHERE trip_id = $1 AND email = $2)", tripID, email).Scan(&exists)
	return exists
}

func requireTripAdmin(db *sql.DB, w http.ResponseWriter, r *http.Request) (string, int64, bool) {
	email, ok := authorize(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", 0, false
	}
	tripID, err := strconv.ParseInt(r.PathValue("tripID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid trip ID", http.StatusBadRequest)
		return "", 0, false
	}
	if !isAdmin(email) && !isTripAdmin(db, email, tripID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", 0, false
	}
	return email, tripID, true
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
			SELECT t.id, t.name, t.room_size, COALESCE(
				json_agg(json_build_object('id', ta.id, 'email', ta.email)) FILTER (WHERE ta.id IS NOT NULL),
				'[]'
			)
			FROM trips t
			LEFT JOIN trip_admins ta ON ta.trip_id = t.id
			GROUP BY t.id, t.name, t.room_size
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
			ID       int64       `json:"id"`
			Name     string      `json:"name"`
			RoomSize int         `json:"room_size"`
			Admins   []tripAdmin `json:"admins"`
		}

		var trips []trip
		for rows.Next() {
			var t trip
			var adminsJSON string
			if err := rows.Scan(&t.ID, &t.Name, &t.RoomSize, &adminsJSON); err != nil {
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

func handleGetTrip(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, ok := requireTripAdmin(db, w, r)
		if !ok {
			return
		}
		var name string
		var roomSize int
		err := db.QueryRow("SELECT name, room_size FROM trips WHERE id = $1", tripID).Scan(&name, &roomSize)
		if err != nil {
			http.Error(w, "trip not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": tripID, "name": name, "room_size": roomSize})
	}
}

func handleListStudents(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, ok := requireTripAdmin(db, w, r)
		if !ok {
			return
		}
		rows, err := db.Query(`
			SELECT s.id, s.name, s.email, COALESCE(
				json_agg(json_build_object('id', p.id, 'email', p.email)) FILTER (WHERE p.id IS NOT NULL),
				'[]'
			)
			FROM students s
			LEFT JOIN parents p ON p.student_id = s.id
			WHERE s.trip_id = $1
			GROUP BY s.id, s.name, s.email
			ORDER BY s.name`, tripID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type parent struct {
			ID    int64  `json:"id"`
			Email string `json:"email"`
		}
		type student struct {
			ID      int64    `json:"id"`
			Name    string   `json:"name"`
			Email   string   `json:"email"`
			Parents []parent `json:"parents"`
		}

		var students []student
		for rows.Next() {
			var s student
			var parentsJSON string
			if err := rows.Scan(&s.ID, &s.Name, &s.Email, &parentsJSON); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.Unmarshal([]byte(parentsJSON), &s.Parents)
			students = append(students, s)
		}
		if students == nil {
			students = []student{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(students)
	}
}

func handleCreateStudent(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, ok := requireTripAdmin(db, w, r)
		if !ok {
			return
		}
		var body struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.Email == "" {
			http.Error(w, "name and email are required", http.StatusBadRequest)
			return
		}
		var id int64
		err := db.QueryRow("INSERT INTO students (trip_id, name, email) VALUES ($1, $2, $3) RETURNING id", tripID, body.Name, body.Email).Scan(&id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": id, "name": body.Name, "email": body.Email})
	}
}

func handleDeleteStudent(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, ok := requireTripAdmin(db, w, r)
		if !ok {
			return
		}
		studentID, err := strconv.ParseInt(r.PathValue("studentID"), 10, 64)
		if err != nil {
			http.Error(w, "invalid student ID", http.StatusBadRequest)
			return
		}
		result, err := db.Exec("DELETE FROM students WHERE id = $1 AND trip_id = $2", studentID, tripID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			http.Error(w, "student not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAddParent(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, ok := requireTripAdmin(db, w, r)
		if !ok {
			return
		}
		studentID, err := strconv.ParseInt(r.PathValue("studentID"), 10, 64)
		if err != nil {
			http.Error(w, "invalid student ID", http.StatusBadRequest)
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
		err = db.QueryRow("INSERT INTO parents (student_id, email) VALUES ((SELECT id FROM students WHERE id = $1 AND trip_id = $2), $3) RETURNING id",
			studentID, tripID, body.Email).Scan(&id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": id, "email": body.Email})
	}
}

func handleRemoveParent(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, ok := requireTripAdmin(db, w, r)
		if !ok {
			return
		}
		parentID, err := strconv.ParseInt(r.PathValue("parentID"), 10, 64)
		if err != nil {
			http.Error(w, "invalid parent ID", http.StatusBadRequest)
			return
		}
		result, err := db.Exec(`DELETE FROM parents WHERE id = $1 AND student_id IN (SELECT id FROM students WHERE trip_id = $2)`, parentID, tripID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			http.Error(w, "parent not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleUpdateTrip(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, ok := requireTripAdmin(db, w, r)
		if !ok {
			return
		}
		var body struct {
			RoomSize int `json:"room_size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RoomSize < 1 {
			http.Error(w, "room_size must be at least 1", http.StatusBadRequest)
			return
		}
		_, err := db.Exec("UPDATE trips SET room_size = $1 WHERE id = $2", body.RoomSize, tripID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListConstraints(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, ok := requireTripAdmin(db, w, r)
		if !ok {
			return
		}
		rows, err := db.Query(`
			SELECT rc.id, rc.student_a_id, sa.name, rc.student_b_id, sb.name, rc.kind::text, rc.level::text
			FROM roommate_constraints rc
			JOIN students sa ON sa.id = rc.student_a_id
			JOIN students sb ON sb.id = rc.student_b_id
			WHERE sa.trip_id = $1
			ORDER BY rc.id`, tripID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type constraint struct {
			ID           int64  `json:"id"`
			StudentAID   int64  `json:"student_a_id"`
			StudentAName string `json:"student_a_name"`
			StudentBID   int64  `json:"student_b_id"`
			StudentBName string `json:"student_b_name"`
			Kind         string `json:"kind"`
			Level        string `json:"level"`
		}

		var constraints []constraint
		for rows.Next() {
			var c constraint
			if err := rows.Scan(&c.ID, &c.StudentAID, &c.StudentAName, &c.StudentBID, &c.StudentBName, &c.Kind, &c.Level); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			constraints = append(constraints, c)
		}
		if constraints == nil {
			constraints = []constraint{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(constraints)
	}
}

func handleCreateConstraint(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, ok := requireTripAdmin(db, w, r)
		if !ok {
			return
		}
		var body struct {
			StudentAID int64  `json:"student_a_id"`
			StudentBID int64  `json:"student_b_id"`
			Kind       string `json:"kind"`
			Level      string `json:"level"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.StudentAID == body.StudentBID {
			http.Error(w, "students must be different", http.StatusBadRequest)
			return
		}
		switch body.Level {
		case "student":
			if body.Kind != "prefer" && body.Kind != "prefer_not" {
				http.Error(w, "students may only use prefer or prefer not", http.StatusBadRequest)
				return
			}
		case "parent":
			if body.Kind != "must_not" {
				http.Error(w, "parents may only use must not", http.StatusBadRequest)
				return
			}
		case "admin":
		default:
			http.Error(w, "invalid level", http.StatusBadRequest)
			return
		}
		var id int64
		err := db.QueryRow(`
			INSERT INTO roommate_constraints (student_a_id, student_b_id, kind, level)
			SELECT $1, $2, $3::constraint_kind, $4::constraint_level
			FROM students sa
			JOIN students sb ON sb.id = $2 AND sb.trip_id = $5
			WHERE sa.id = $1 AND sa.trip_id = $5
			ON CONFLICT (student_a_id, student_b_id, level) DO UPDATE SET kind = EXCLUDED.kind
			RETURNING id`, body.StudentAID, body.StudentBID, body.Kind, body.Level, tripID).Scan(&id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": id})
	}
}

func handleDeleteConstraint(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, ok := requireTripAdmin(db, w, r)
		if !ok {
			return
		}
		constraintID, err := strconv.ParseInt(r.PathValue("constraintID"), 10, 64)
		if err != nil {
			http.Error(w, "invalid constraint ID", http.StatusBadRequest)
			return
		}
		result, err := db.Exec(`DELETE FROM roommate_constraints WHERE id = $1
			AND student_a_id IN (SELECT id FROM students WHERE trip_id = $2)`, constraintID, tripID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			http.Error(w, "constraint not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
