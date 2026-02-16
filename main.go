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
	"log"
	"math/rand"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	texttemplate "text/template"

	"github.com/lib/pq"
	"google.golang.org/api/idtoken"
)

//go:embed schema.sql
var schema string

var (
	htmlTemplates *template.Template
	jsTemplates   *texttemplate.Template
	levelKinds    = map[string][]string{
		"student": {"prefer", "prefer_not"},
		"parent":  {"must_not"},
		"admin":   {"must", "prefer", "prefer_not", "must_not"},
	}
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
	http.HandleFunc("GET /api/trips/{tripID}/me", handleTripMe(db))
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
	http.HandleFunc("POST /api/trips/{tripID}/solve", handleSolve(db))
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
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html")
		t.Execute(w, templateData())
	}
}

func serveJS(name string) http.HandlerFunc {
	t := jsTemplates.Lookup(name)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
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
	return slices.ContainsFunc(strings.Split(os.Getenv("ADMINS"), ","), func(a string) bool {
		return strings.TrimSpace(a) == email
	})
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

func tripRole(db *sql.DB, email string, tripID int64) (string, []int64) {
	if isAdmin(email) || isTripAdmin(db, email, tripID) {
		return "admin", nil
	}
	var studentIDs []int64
	rows, _ := db.Query("SELECT id FROM students WHERE trip_id = $1 AND email = $2", tripID, email)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			studentIDs = append(studentIDs, id)
		}
	}
	if len(studentIDs) > 0 {
		return "student", studentIDs
	}
	rows2, _ := db.Query("SELECT s.id FROM parents p JOIN students s ON s.id = p.student_id WHERE s.trip_id = $1 AND p.email = $2", tripID, email)
	if rows2 != nil {
		defer rows2.Close()
		for rows2.Next() {
			var id int64
			rows2.Scan(&id)
			studentIDs = append(studentIDs, id)
		}
	}
	if len(studentIDs) > 0 {
		return "parent", studentIDs
	}
	return "", nil
}

func requireTripMember(db *sql.DB, w http.ResponseWriter, r *http.Request) (string, int64, string, []int64, bool) {
	email, ok := authorize(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", 0, "", nil, false
	}
	tripID, err := strconv.ParseInt(r.PathValue("tripID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid trip ID", http.StatusBadRequest)
		return "", 0, "", nil, false
	}
	role, studentIDs := tripRole(db, email, tripID)
	if role == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", 0, "", nil, false
	}
	return email, tripID, role, studentIDs, true
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
			SELECT t.id, t.name, t.room_size, t.prefer_not_multiple, t.no_prefer_cost, COALESCE(
				json_agg(json_build_object('id', ta.id, 'email', ta.email)) FILTER (WHERE ta.id IS NOT NULL),
				'[]'
			)
			FROM trips t
			LEFT JOIN trip_admins ta ON ta.trip_id = t.id
			GROUP BY t.id, t.name, t.room_size, t.prefer_not_multiple, t.no_prefer_cost
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
			ID                 int64       `json:"id"`
			Name               string      `json:"name"`
			RoomSize           int         `json:"room_size"`
			PreferNotMultiple  int         `json:"prefer_not_multiple"`
			NoPreferCost       int         `json:"no_prefer_cost"`
			Admins             []tripAdmin `json:"admins"`
		}

		var trips []trip
		for rows.Next() {
			var t trip
			var adminsJSON string
			if err := rows.Scan(&t.ID, &t.Name, &t.RoomSize, &t.PreferNotMultiple, &t.NoPreferCost, &adminsJSON); err != nil {
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

func handleTripMe(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, role, studentIDs, ok := requireTripMember(db, w, r)
		if !ok {
			return
		}
		type studentInfo struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		var students []studentInfo
		for _, sid := range studentIDs {
			var name string
			if err := db.QueryRow("SELECT name FROM students WHERE id = $1 AND trip_id = $2", sid, tripID).Scan(&name); err == nil {
				students = append(students, studentInfo{ID: sid, Name: name})
			}
		}
		if students == nil {
			students = []studentInfo{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"role": role, "students": students, "level_kinds": levelKinds})
	}
}

func handleGetTrip(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, _, _, ok := requireTripMember(db, w, r)
		if !ok {
			return
		}
		var name string
		var roomSize, preferNotMultiple, noPreferCost int
		err := db.QueryRow("SELECT name, room_size, prefer_not_multiple, no_prefer_cost FROM trips WHERE id = $1", tripID).Scan(&name, &roomSize, &preferNotMultiple, &noPreferCost)
		if err != nil {
			http.Error(w, "trip not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": tripID, "name": name, "room_size": roomSize, "prefer_not_multiple": preferNotMultiple, "no_prefer_cost": noPreferCost})
	}
}

func handleListStudents(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, role, _, ok := requireTripMember(db, w, r)
		if !ok {
			return
		}

		if role != "admin" {
			rows, err := db.Query("SELECT id, name FROM students WHERE trip_id = $1 ORDER BY name", tripID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer rows.Close()
			type studentBasic struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			}
			var students []studentBasic
			for rows.Next() {
				var s studentBasic
				if err := rows.Scan(&s.ID, &s.Name); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				students = append(students, s)
			}
			if students == nil {
				students = []studentBasic{}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(students)
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
			RoomSize          *int `json:"room_size"`
			PreferNotMultiple *int `json:"prefer_not_multiple"`
			NoPreferCost      *int `json:"no_prefer_cost"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.RoomSize != nil {
			if *body.RoomSize < 1 {
				http.Error(w, "room_size must be at least 1", http.StatusBadRequest)
				return
			}
		}
		if body.PreferNotMultiple != nil {
			if *body.PreferNotMultiple < 1 {
				http.Error(w, "prefer_not_multiple must be at least 1", http.StatusBadRequest)
				return
			}
		}
		if body.NoPreferCost != nil {
			if *body.NoPreferCost < 0 {
				http.Error(w, "no_prefer_cost must be at least 0", http.StatusBadRequest)
				return
			}
		}
		if body.RoomSize != nil {
			if _, err := db.Exec("UPDATE trips SET room_size = $1 WHERE id = $2", *body.RoomSize, tripID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if body.PreferNotMultiple != nil {
			if _, err := db.Exec("UPDATE trips SET prefer_not_multiple = $1 WHERE id = $2", *body.PreferNotMultiple, tripID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if body.NoPreferCost != nil {
			if _, err := db.Exec("UPDATE trips SET no_prefer_cost = $1 WHERE id = $2", *body.NoPreferCost, tripID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListConstraints(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, role, myStudentIDs, ok := requireTripMember(db, w, r)
		if !ok {
			return
		}
		var query string
		var args []any
		switch role {
		case "admin":
			query = `SELECT rc.id, rc.student_a_id, sa.name, rc.student_b_id, sb.name, rc.kind::text, rc.level::text
				FROM roommate_constraints rc
				JOIN students sa ON sa.id = rc.student_a_id
				JOIN students sb ON sb.id = rc.student_b_id
				WHERE sa.trip_id = $1
				ORDER BY rc.id`
			args = []any{tripID}
		case "student":
			query = `SELECT rc.id, rc.student_a_id, sa.name, rc.student_b_id, sb.name, rc.kind::text, rc.level::text
				FROM roommate_constraints rc
				JOIN students sa ON sa.id = rc.student_a_id
				JOIN students sb ON sb.id = rc.student_b_id
				WHERE sa.trip_id = $1 AND rc.level = 'student' AND rc.student_a_id = ANY($2)
				ORDER BY rc.id`
			args = []any{tripID, pq.Array(myStudentIDs)}
		case "parent":
			query = `SELECT rc.id, rc.student_a_id, sa.name, rc.student_b_id, sb.name, rc.kind::text, rc.level::text
				FROM roommate_constraints rc
				JOIN students sa ON sa.id = rc.student_a_id
				JOIN students sb ON sb.id = rc.student_b_id
				WHERE sa.trip_id = $1 AND rc.level = 'parent' AND rc.student_a_id = ANY($2)
				ORDER BY rc.id`
			args = []any{tripID, pq.Array(myStudentIDs)}
		}
		rows, err := db.Query(query, args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type constraint struct {
			ID           int64   `json:"id"`
			StudentAID   int64   `json:"student_a_id"`
			StudentAName string  `json:"student_a_name"`
			StudentBID   int64   `json:"student_b_id"`
			StudentBName string  `json:"student_b_name"`
			Kind         string  `json:"kind"`
			Level        string  `json:"level"`
			Override     *string `json:"override"`
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

		type levelKind struct {
			Level string `json:"level"`
			Kind  string `json:"kind"`
		}
		type overrideEntry struct {
			Names     string      `json:"names"`
			Positives []levelKind `json:"positives"`
			Negatives []levelKind `json:"negatives"`
		}
		var overrides []overrideEntry
		type overallEntry struct {
			StudentAID   int64  `json:"student_a_id"`
			StudentBID   int64  `json:"student_b_id"`
			StudentBName string `json:"student_b_name"`
			Kind         string `json:"kind"`
			Level        string `json:"level"`
		}
		var overalls []overallEntry
		type mismatchEntry struct {
			NameA string `json:"name_a"`
			NameB string `json:"name_b"`
			KindA string `json:"kind_a"`
			KindB string `json:"kind_b"`
		}
		var mismatches []mismatchEntry

		if role == "admin" {
			type pairKey struct{ a, b int64 }
			pairGroups := map[pairKey][]int{}
			for i := range constraints {
				pk := pairKey{constraints[i].StudentAID, constraints[i].StudentBID}
				pairGroups[pk] = append(pairGroups[pk], i)
			}
			isPositive := func(kind string) bool { return kind == "must" || kind == "prefer" }
			kindLabel := map[string]string{"must": "Must", "prefer": "Prefer", "prefer_not": "Prefer Not", "must_not": "Must Not"}
			levelPriority := map[string]int{"admin": 0, "parent": 1, "student": 2}
			for _, idxs := range pairGroups {
				bestIdx := idxs[0]
				for _, i := range idxs[1:] {
					if levelPriority[constraints[i].Level] < levelPriority[constraints[bestIdx].Level] {
						bestIdx = i
					}
				}
				overalls = append(overalls, overallEntry{
					StudentAID:   constraints[bestIdx].StudentAID,
					StudentBID:   constraints[bestIdx].StudentBID,
					StudentBName: constraints[bestIdx].StudentBName,
					Kind:         constraints[bestIdx].Kind,
					Level:        constraints[bestIdx].Level,
				})

				var posIdx, negIdx []int
				for _, i := range idxs {
					if isPositive(constraints[i].Kind) {
						posIdx = append(posIdx, i)
					} else {
						negIdx = append(negIdx, i)
					}
				}
				if len(posIdx) == 0 || len(negIdx) == 0 {
					continue
				}
				var positives, negatives []levelKind
				for _, i := range posIdx {
					positives = append(positives, levelKind{constraints[i].Level, constraints[i].Kind})
				}
				for _, i := range negIdx {
					negatives = append(negatives, levelKind{constraints[i].Level, constraints[i].Kind})
				}
				overrides = append(overrides, overrideEntry{
					Names:     constraints[idxs[0]].StudentAName + " \u2192 " + constraints[idxs[0]].StudentBName,
					Positives: positives,
					Negatives: negatives,
				})
				for _, i := range idxs {
					var opposing []int
					if isPositive(constraints[i].Kind) {
						opposing = negIdx
					} else {
						opposing = posIdx
					}
					parts := make([]string, len(opposing))
					for j, o := range opposing {
						parts[j] = strings.ToUpper(constraints[o].Level[:1]) + constraints[o].Level[1:] + " says " + kindLabel[constraints[o].Kind]
					}
					desc := strings.Join(parts, ", ")
					constraints[i].Override = &desc
				}
			}
			overallMap := map[pairKey]overallEntry{}
			for _, o := range overalls {
				overallMap[pairKey{o.StudentAID, o.StudentBID}] = o
			}
			for _, o := range overalls {
				rev, ok := overallMap[pairKey{o.StudentBID, o.StudentAID}]
				if !ok {
					continue
				}
				if isPositive(o.Kind) && !isPositive(rev.Kind) {
					mismatches = append(mismatches, mismatchEntry{
						NameA: rev.StudentBName,
						NameB: o.StudentBName,
						KindA: o.Kind,
						KindB: rev.Kind,
					})
				}
			}
		}
		if mismatches == nil {
			mismatches = []mismatchEntry{}
		}
		if overrides == nil {
			overrides = []overrideEntry{}
		}
		if overalls == nil {
			overalls = []overallEntry{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"constraints": constraints, "overrides": overrides, "overalls": overalls, "mismatches": mismatches})
	}
}

func handleCreateConstraint(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, role, myStudentIDs, ok := requireTripMember(db, w, r)
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
		validKinds := levelKinds[body.Level]
		if validKinds == nil {
			http.Error(w, "invalid level", http.StatusBadRequest)
			return
		}
		if !slices.Contains(validKinds, body.Kind) {
			http.Error(w, "invalid kind for level", http.StatusBadRequest)
			return
		}
		if role != "admin" {
			if body.Level != role {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !slices.Contains(myStudentIDs, body.StudentAID) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
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
		_, tripID, role, myStudentIDs, ok := requireTripMember(db, w, r)
		if !ok {
			return
		}
		constraintID, err := strconv.ParseInt(r.PathValue("constraintID"), 10, 64)
		if err != nil {
			http.Error(w, "invalid constraint ID", http.StatusBadRequest)
			return
		}
		var query string
		var args []any
		if role == "admin" {
			query = `DELETE FROM roommate_constraints WHERE id = $1
				AND student_a_id IN (SELECT id FROM students WHERE trip_id = $2)`
			args = []any{constraintID, tripID}
		} else {
			query = `DELETE FROM roommate_constraints WHERE id = $1
				AND student_a_id = ANY($2) AND level = $3::constraint_level`
			args = []any{constraintID, pq.Array(myStudentIDs), role}
		}
		result, err := db.Exec(query, args...)
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

func handleSolve(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, tripID, ok := requireTripAdmin(db, w, r)
		if !ok {
			return
		}

		var roomSize, pnMultiple, npCost int
		err := db.QueryRow("SELECT room_size, prefer_not_multiple, no_prefer_cost FROM trips WHERE id = $1", tripID).Scan(&roomSize, &pnMultiple, &npCost)
		if err != nil {
			http.Error(w, "trip not found", http.StatusNotFound)
			return
		}

		rows, err := db.Query("SELECT id, name FROM students WHERE trip_id = $1 ORDER BY id", tripID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var studentIDs []int64
		studentName := map[int64]string{}
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			studentIDs = append(studentIDs, id)
			studentName[id] = name
		}

		if len(studentIDs) == 0 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"rooms": []any{}, "score": 0})
			return
		}

		crows, err := db.Query(`
			SELECT rc.student_a_id, rc.student_b_id, rc.kind::text, rc.level::text
			FROM roommate_constraints rc
			JOIN students sa ON sa.id = rc.student_a_id
			WHERE sa.trip_id = $1`, tripID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer crows.Close()

		type constraint struct {
			aID, bID     int64
			kind, level  string
		}
		var allConstraints []constraint
		for crows.Next() {
			var c constraint
			if err := crows.Scan(&c.aID, &c.bID, &c.kind, &c.level); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			allConstraints = append(allConstraints, c)
		}

		type pairKey struct{ a, b int64 }
		overalls := map[pairKey]string{}
		byPair := map[pairKey]map[string]string{}
		for _, c := range allConstraints {
			pk := pairKey{c.aID, c.bID}
			if byPair[pk] == nil {
				byPair[pk] = map[string]string{}
			}
			byPair[pk][c.level] = c.kind
		}
		levelPriority := []string{"admin", "parent", "student"}
		for pk, levels := range byPair {
			for _, lev := range levelPriority {
				if kind, ok := levels[lev]; ok {
					overalls[pk] = kind
					break
				}
			}
		}

		idx := map[int64]int{}
		for i, id := range studentIDs {
			idx[id] = i
		}
		n := len(studentIDs)

		mustTogether := map[[2]int]bool{}
		mustApart := map[[2]int]bool{}
		for pk, kind := range overalls {
			ai, bi := idx[pk.a], idx[pk.b]
			switch kind {
			case "must":
				p := [2]int{ai, bi}
				if p[0] > p[1] { p[0], p[1] = p[1], p[0] }
				mustTogether[p] = true
			case "must_not":
				p := [2]int{ai, bi}
				if p[0] > p[1] { p[0], p[1] = p[1], p[0] }
				mustApart[p] = true
			}
		}

		uf := make([]int, n)
		for i := range uf { uf[i] = i }
		var ufFind func(int) int
		ufFind = func(x int) int {
			if uf[x] != x { uf[x] = ufFind(uf[x]) }
			return uf[x]
		}
		ufUnion := func(a, b int) {
			ra, rb := ufFind(a), ufFind(b)
			if ra != rb { uf[ra] = rb }
		}

		for p := range mustTogether {
			ufUnion(p[0], p[1])
		}

		hasConflict := false
		for p := range mustApart {
			if ufFind(p[0]) == ufFind(p[1]) {
				hasConflict = true
				break
			}
		}

		if hasConflict {
			http.Error(w, "hard conflicts exist, resolve before solving", http.StatusBadRequest)
			return
		}

		groups := map[int][]int{}
		for i := range n {
			root := ufFind(i)
			groups[root] = append(groups[root], i)
		}

		hasPrefer := make([]bool, n)
		for pk, kind := range overalls {
			if kind == "prefer" {
				hasPrefer[idx[pk.a]] = true
			}
		}

		score := func(assignment []int) int {
			s := 0
			gotPrefer := make([]bool, n)
			for pk, kind := range overalls {
				ai, bi := idx[pk.a], idx[pk.b]
				sameRoom := assignment[ai] == assignment[bi]
				switch kind {
				case "prefer":
					if sameRoom {
						s++
						gotPrefer[ai] = true
					}
				case "prefer_not":
					if sameRoom { s -= pnMultiple }
				}
			}
			for i := range n {
				if hasPrefer[i] && !gotPrefer[i] {
					s -= npCost
				}
			}
			return s
		}

		feasible := func(assignment []int) bool {
			for p := range mustApart {
				if assignment[p[0]] == assignment[p[1]] { return false }
			}
			roomCounts := map[int]int{}
			for _, room := range assignment {
				roomCounts[room]++
			}
			for _, cnt := range roomCounts {
				if cnt > roomSize { return false }
			}
			return true
		}

		numRooms := (n + roomSize - 1) / roomSize

		assignment := make([]int, n)
		groupList := make([][]int, 0, len(groups))
		for _, members := range groups {
			groupList = append(groupList, members)
		}
		slices.SortFunc(groupList, func(a, b []int) int { return len(b) - len(a) })

		roomCap := make([]int, numRooms)
		for i := range roomCap { roomCap[i] = roomSize }

		placed := false
		var placeGroups func(gi int) bool
		placeGroups = func(gi int) bool {
			if gi >= len(groupList) { return true }
			grp := groupList[gi]
			for room := range numRooms {
				if roomCap[room] < len(grp) { continue }
				ok := true
				for _, member := range grp {
					for p := range mustApart {
						partner := -1
						if p[0] == member { partner = p[1] }
						if p[1] == member { partner = p[0] }
						if partner >= 0 && assignment[partner] == room {
							alreadyPlaced := false
							for gj := range gi {
								if slices.Contains(groupList[gj], partner) {
									alreadyPlaced = true
									break
								}
							}
							if alreadyPlaced { ok = false; break }
						}
					}
					if !ok { break }
				}
				if !ok { continue }
				for _, member := range grp { assignment[member] = room }
				roomCap[room] -= len(grp)
				if placeGroups(gi + 1) { return true }
				roomCap[room] += len(grp)
			}
			return false
		}
		placed = placeGroups(0)

		if !placed {
			for i := range n {
				assignment[i] = i % numRooms
			}
		}

		initialAssignment := make([]int, n)
		copy(initialAssignment, assignment)

		bestAssignment := make([]int, n)
		copy(bestAssignment, assignment)
		bestScore := score(assignment)

		roomCount := func(a []int, room int) int {
			c := 0
			for _, r := range a {
				if r == room { c++ }
			}
			return c
		}

		uniqueGroups := make([]int, 0, len(groups))
		for root := range groups {
			uniqueGroups = append(uniqueGroups, root)
		}
		slices.Sort(uniqueGroups)

		hillClimb := func(assignment []int) int {
			currentScore := score(assignment)
			for {
				bestDelta := 0
				bestMove := -1
				bestTarget := -1
				bestSwapJ := -1

				for gi, gRoot := range uniqueGroups {
					grp := groups[gRoot]
					gRoom := assignment[grp[0]]

					for room := range numRooms {
						if room == gRoom { continue }
						if roomCount(assignment, room)+len(grp) > roomSize { continue }
						for _, m := range grp { assignment[m] = room }
						if feasible(assignment) {
							delta := score(assignment) - currentScore
							if delta > bestDelta {
								bestDelta = delta
								bestMove = gi
								bestTarget = room
								bestSwapJ = -1
							}
						}
						for _, m := range grp { assignment[m] = gRoom }
					}

					for gj := gi + 1; gj < len(uniqueGroups); gj++ {
						grp2 := groups[uniqueGroups[gj]]
						g2Room := assignment[grp2[0]]
						if gRoom == g2Room { continue }
						newGRoom := roomCount(assignment, gRoom) - len(grp) + len(grp2)
						newG2Room := roomCount(assignment, g2Room) - len(grp2) + len(grp)
						if newGRoom > roomSize || newG2Room > roomSize { continue }
						for _, m := range grp { assignment[m] = g2Room }
						for _, m := range grp2 { assignment[m] = gRoom }
						if feasible(assignment) {
							delta := score(assignment) - currentScore
							if delta > bestDelta {
								bestDelta = delta
								bestMove = gi
								bestTarget = -1
								bestSwapJ = gj
							}
						}
						for _, m := range grp { assignment[m] = gRoom }
						for _, m := range grp2 { assignment[m] = g2Room }
					}
				}

				if bestDelta <= 0 { break }

				grp := groups[uniqueGroups[bestMove]]
				gRoom := assignment[grp[0]]
				if bestSwapJ < 0 {
					for _, m := range grp { assignment[m] = bestTarget }
				} else {
					grp2 := groups[uniqueGroups[bestSwapJ]]
					g2Room := assignment[grp2[0]]
					for _, m := range grp { assignment[m] = g2Room }
					for _, m := range grp2 { assignment[m] = gRoom }
				}
				currentScore += bestDelta
			}
			return currentScore
		}

		randomPlacement := func() bool {
			perm := rand.Perm(len(groupList))
			for i := range roomCap { roomCap[i] = roomSize }
			for _, pi := range perm {
				grp := groupList[pi]
				placed := false
				order := rand.Perm(numRooms)
				for _, room := range order {
					if roomCap[room] < len(grp) { continue }
					valid := true
					for _, member := range grp {
						for p := range mustApart {
							partner := -1
							if p[0] == member { partner = p[1] }
							if p[1] == member { partner = p[0] }
							if partner >= 0 && assignment[partner] == room {
								valid = false
								break
							}
						}
						if !valid { break }
					}
					if !valid { continue }
					for _, member := range grp { assignment[member] = room }
					roomCap[room] -= len(grp)
					placed = true
					break
				}
				if !placed { return false }
			}
			return true
		}

		perturb := func(src []int, count int) {
			copy(assignment, src)
			indices := rand.Perm(len(uniqueGroups))
			count = min(count, len(indices))
			for _, gi := range indices[:count] {
				grp := groups[uniqueGroups[gi]]
				oldRoom := assignment[grp[0]]
				rooms := rand.Perm(numRooms)
				for _, room := range rooms {
					if room == oldRoom { continue }
					if roomCount(assignment, room)+len(grp) > roomSize { continue }
					for _, m := range grp { assignment[m] = room }
					if feasible(assignment) { break }
					for _, m := range grp { assignment[m] = oldRoom }
				}
			}
		}

		copy(assignment, initialAssignment)
		s := hillClimb(assignment)
		if s > bestScore {
			bestScore = s
			copy(bestAssignment, assignment)
		}

		for range 30 {
			if randomPlacement() {
				s := hillClimb(assignment)
				if s > bestScore {
					bestScore = s
					copy(bestAssignment, assignment)
				}
			}
		}

		for range 200 {
			perturb(bestAssignment, 2+rand.Intn(3))
			s := hillClimb(assignment)
			if s > bestScore {
				bestScore = s
				copy(bestAssignment, assignment)
			}
		}

		type roomMember struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		roomMap := map[int][]roomMember{}
		for i, room := range bestAssignment {
			sid := studentIDs[i]
			roomMap[room] = append(roomMap[room], roomMember{ID: sid, Name: studentName[sid]})
		}
		var rooms [][]roomMember
		for room := range numRooms {
			if members, ok := roomMap[room]; ok {
				slices.SortFunc(members, func(a, b roomMember) int { return strings.Compare(a.Name, b.Name) })
				rooms = append(rooms, members)
			}
		}
		slices.SortFunc(rooms, func(a, b []roomMember) int { return strings.Compare(a[0].Name, b[0].Name) })

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"rooms": rooms, "score": bestScore})
	}
}
