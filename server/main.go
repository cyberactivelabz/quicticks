package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type config struct{ port, dbPath, cors, adminUser, adminPassword, sessionSecret, moniepointURL, moniepointKey, webhookSecret string }
type app struct {
	db      *sql.DB
	cfg     config
	limiter *limiter
}
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}
type orderInput struct {
	TicketType string `json:"ticket_type"`
	Quantity   int    `json:"quantity"`
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Email      string `json:"email"`
}
type order struct {
	Reference  string   `json:"reference"`
	Status     string   `json:"status"`
	TicketType string   `json:"ticket_type"`
	Name       string   `json:"name"`
	Phone      string   `json:"phone"`
	Email      string   `json:"email"`
	Quantity   int      `json:"quantity"`
	Total      int      `json:"total"`
	CreatedAt  string   `json:"created_at"`
	Tickets    []ticket `json:"tickets,omitempty"`
}
type ticket struct {
	Code  string `json:"code"`
	QRPng string `json:"qr_png"`
	Used  bool   `json:"used"`
}

func getenv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func main() {
	cfg := config{getenv("PORT", "8080"), getenv("DATABASE_PATH", "./data/quicticks.db"), getenv("CORS_ORIGIN", "http://localhost:8080"), os.Getenv("ADMIN_USERNAME"), os.Getenv("ADMIN_PASSWORD"), os.Getenv("SESSION_SECRET"), os.Getenv("MONIEPOINT_API_URL"), os.Getenv("MONIEPOINT_API_KEY"), os.Getenv("MONIEPOINT_WEBHOOK_SECRET")}
	if cfg.adminUser == "" || cfg.adminPassword == "" || len(cfg.sessionSecret) < 32 {
		log.Fatal("ADMIN_USERNAME, ADMIN_PASSWORD and a 32+ character SESSION_SECRET are required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.dbPath), 0750); err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("sqlite", cfg.dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	a := &app{db: db, cfg: cfg, limiter: &limiter{hits: map[string][]time.Time{}}}
	if err = a.migrate(); err != nil {
		log.Fatal(err)
	}
	log.Printf("ticket API listening on :%s", cfg.port)
	log.Fatal(http.ListenAndServe(":"+cfg.port, a.routes()))
}
func (a *app) migrate() error {
	_, err := a.db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL); CREATE TABLE IF NOT EXISTS orders (reference TEXT PRIMARY KEY, idempotency_key TEXT UNIQUE NOT NULL, status TEXT NOT NULL, ticket_type TEXT NOT NULL, quantity INTEGER NOT NULL, total INTEGER NOT NULL, name TEXT NOT NULL, phone TEXT NOT NULL, email TEXT NOT NULL, created_at TEXT NOT NULL, paid_at TEXT); CREATE TABLE IF NOT EXISTS tickets (code TEXT PRIMARY KEY, reference TEXT NOT NULL REFERENCES orders(reference), used INTEGER NOT NULL DEFAULT 0, used_at TEXT); INSERT OR IGNORE INTO settings(key,value) VALUES ('male_price','2000'),('female_price','1000'),('male_capacity','500'),('female_capacity','500'),('sales_open','true');`)
	return err
}
func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/orders", a.orders)
	mux.HandleFunc("/api/orders/", a.orderByReference)
	mux.HandleFunc("/api/payments/moniepoint/webhook", a.webhook)
	mux.HandleFunc("/api/admin/login", a.login)
	mux.HandleFunc("/api/admin/logout", a.logout)
	mux.HandleFunc("/api/admin/me", a.adminOnly(a.me))
	mux.HandleFunc("/api/admin/dashboard", a.adminOnly(a.dashboard))
	mux.HandleFunc("/api/admin/orders.csv", a.adminOnly(a.csv))
	mux.HandleFunc("/api/admin/orders/", a.adminOnly(a.adminOrder))
	mux.HandleFunc("/api/admin/settings", a.adminOnly(a.settings))
	mux.HandleFunc("/api/admin/scan", a.adminOnly(a.scan))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { jsonOut(w, 200, map[string]string{"status": "ok"}) })
	mux.Handle("/", http.FileServer(http.Dir(".")))
	return a.cors(a.security(mux))
}
func (a *app) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", a.cfg.cors)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Idempotency-Key,X-Moniepoint-Signature")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *app) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self' https://fonts.googleapis.com https://fonts.gstatic.com; img-src 'self' data:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; script-src 'self' 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}
func (a *app) orders(w http.ResponseWriter, r *http.Request) {
	if !a.allow(r, 20, time.Minute) {
		jsonOut(w, 429, map[string]string{"error": "Too many requests"})
		return
	}
	if r.Method != "POST" {
		jsonOut(w, 405, nil)
		return
	}
	var in orderInput
	if json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&in) != nil {
		jsonOut(w, 400, map[string]string{"error": "Invalid request"})
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Phone = regexp.MustCompile(`[\s-]`).ReplaceAllString(in.Phone, "")
	in.Email = strings.TrimSpace(strings.ToLower(in.Email))
	if in.TicketType != "male" && in.TicketType != "female" || in.Quantity < 1 || in.Quantity > 10 || len(in.Name) < 2 || !regexp.MustCompile(`^\+?(234|0)\d{10}$`).MatchString(in.Phone) || !regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`).MatchString(in.Email) {
		jsonOut(w, 400, map[string]string{"error": "Enter valid order details"})
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if len(idem) < 8 {
		jsonOut(w, 400, map[string]string{"error": "Idempotency-Key is required"})
		return
	}
	var existing order
	err := a.db.QueryRow(`SELECT reference,status,ticket_type,quantity,total,name,phone,email,created_at FROM orders WHERE idempotency_key=?`, idem).Scan(&existing.Reference, &existing.Status, &existing.TicketType, &existing.Quantity, &existing.Total, &existing.Name, &existing.Phone, &existing.Email, &existing.CreatedAt)
	if err == nil {
		jsonOut(w, 200, existing)
		return
	}
	var salesOpen string
	if err := a.db.QueryRow(`SELECT value FROM settings WHERE key='sales_open'`).Scan(&salesOpen); err != nil || salesOpen != "true" {
		jsonOut(w, 409, map[string]string{"error": "Sales are currently closed"})
		return
	}
	price, capacity, err := a.priceAndCapacity(in.TicketType)
	if err != nil {
		jsonOut(w, 500, map[string]string{"error": "Could not load prices"})
		return
	}
	var reserved int
	a.db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM orders WHERE ticket_type=? AND status IN ('pending','paid')`, in.TicketType).Scan(&reserved)
	if reserved+in.Quantity > capacity {
		jsonOut(w, 409, map[string]string{"error": "Tickets are sold out"})
		return
	}
	ref := newReference()
	_, err = a.db.Exec(`INSERT INTO orders(reference,idempotency_key,status,ticket_type,quantity,total,name,phone,email,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, ref, idem, "pending", in.TicketType, in.Quantity, price*in.Quantity, in.Name, in.Phone, in.Email, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		jsonOut(w, 409, map[string]string{"error": "Order already exists"})
		return
	}
	jsonOut(w, 201, order{Reference: ref, Status: "pending", TicketType: in.TicketType, Quantity: in.Quantity, Total: price * in.Quantity, Name: in.Name, Phone: in.Phone, Email: in.Email})
}
func (a *app) priceAndCapacity(kind string) (int, int, error) {
	var p, c int
	err := a.db.QueryRow(`SELECT CAST((SELECT value FROM settings WHERE key=?) AS INTEGER),CAST((SELECT value FROM settings WHERE key=?) AS INTEGER)`, kind+"_price", kind+"_capacity").Scan(&p, &c)
	return p, c, err
}
func (a *app) orderByReference(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonOut(w, 405, nil)
		return
	}
	ref := strings.TrimPrefix(r.URL.Path, "/api/orders/")
	var o order
	err := a.db.QueryRow(`SELECT reference,status,ticket_type,quantity,total,name,phone,email,created_at FROM orders WHERE reference=?`, ref).Scan(&o.Reference, &o.Status, &o.TicketType, &o.Quantity, &o.Total, &o.Name, &o.Phone, &o.Email, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		jsonOut(w, 404, map[string]string{"error": "Order not found"})
		return
	}
	if err != nil {
		jsonOut(w, 500, nil)
		return
	}
	if o.Status == "paid" {
		o.Tickets = a.tickets(ref)
	}
	jsonOut(w, 200, o)
}
func (a *app) tickets(ref string) []ticket {
	rows, _ := a.db.Query(`SELECT code,used FROM tickets WHERE reference=? ORDER BY code`, ref)
	if rows == nil {
		return nil
	}
	defer rows.Close()
	out := []ticket{}
	for rows.Next() {
		var t ticket
		rows.Scan(&t.Code, &t.Used)
		t.QRPng = qr(t.Code)
		out = append(out, t)
	}
	return out
}
func qr(code string) string {
	b, err := qrcode.Encode(code, qrcode.Medium, 256)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}
func (a *app) webhook(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	mac := hmac.New(sha256.New, []byte(a.cfg.webhookSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if a.cfg.webhookSecret == "" || !hmac.Equal([]byte(strings.ToLower(r.Header.Get("X-Moniepoint-Signature"))), []byte(expected)) {
		jsonOut(w, 401, map[string]string{"error": "Invalid signature"})
		return
	}
	var event struct {
		Reference string `json:"reference"`
		Status    string `json:"status"`
	}
	if json.Unmarshal(body, &event) != nil || strings.ToLower(event.Status) != "paid" {
		jsonOut(w, 400, map[string]string{"error": "Invalid payment event"})
		return
	}
	if err := a.markPaid(event.Reference); err != nil {
		jsonOut(w, 500, map[string]string{"error": "Could not issue tickets"})
		return
	}
	jsonOut(w, 200, map[string]string{"status": "ok"})
}
func (a *app) markPaid(ref string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var qty int
	if err = tx.QueryRow(`SELECT status,quantity FROM orders WHERE reference=?`, ref).Scan(&status, &qty); err != nil {
		return err
	}
	if status == "paid" {
		return tx.Commit()
	}
	if _, err = tx.Exec(`UPDATE orders SET status='paid',paid_at=? WHERE reference=?`, time.Now().UTC().Format(time.RFC3339), ref); err != nil {
		return err
	}
	for i := 0; i < qty; i++ {
		if _, err = tx.Exec(`INSERT INTO tickets(code,reference) VALUES(?,?)`, newTicketCode(), ref); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (a *app) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || !a.allow(r, 5, 15*time.Minute) {
		jsonOut(w, 429, map[string]string{"error": "Login unavailable"})
		return
	}
	var v struct{ Username, Password string }
	json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&v)
	if v.Username != a.cfg.adminUser || bcrypt.CompareHashAndPassword([]byte(a.cfg.adminPassword), []byte(v.Password)) != nil {
		jsonOut(w, 401, map[string]string{"error": "Invalid credentials"})
		return
	}
	token := sign(v.Username+"|"+strconv.FormatInt(time.Now().Add(8*time.Hour).Unix(), 10), a.cfg.sessionSecret)
	http.SetCookie(w, &http.Cookie{Name: "vf3_session", Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 28800})
	jsonOut(w, 200, map[string]string{"status": "ok"})
}
func (a *app) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "vf3_session", MaxAge: -1, Path: "/"})
	jsonOut(w, 200, map[string]string{"status": "ok"})
}
func sign(value, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString([]byte(value)) + "." + hex.EncodeToString(mac.Sum(nil))
}
func validToken(v, secret string) bool {
	p := strings.Split(v, ".")
	if len(p) != 2 {
		return false
	}
	raw, e := base64.RawURLEncoding.DecodeString(p[0])
	if e != nil {
		return false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 2 {
		return false
	}
	until, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil || time.Now().Unix() > until {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	return hmac.Equal([]byte(p[1]), []byte(hex.EncodeToString(mac.Sum(nil))))
}
func (a *app) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("vf3_session")
		if e != nil || !validToken(c.Value, a.cfg.sessionSecret) {
			jsonOut(w, 401, map[string]string{"error": "Unauthorised"})
			return
		}
		next(w, r)
	}
}
func (a *app) me(w http.ResponseWriter, _ *http.Request) {
	jsonOut(w, 200, map[string]bool{"authenticated": true})
}
func (a *app) dashboard(w http.ResponseWriter, _ *http.Request) {
	var paid, pending, revenue int
	a.db.QueryRow(`SELECT COUNT(*) FROM orders WHERE status='paid'`).Scan(&paid)
	a.db.QueryRow(`SELECT COUNT(*) FROM orders WHERE status='pending'`).Scan(&pending)
	a.db.QueryRow(`SELECT COALESCE(SUM(total),0) FROM orders WHERE status='paid'`).Scan(&revenue)
	rows, _ := a.db.Query(`SELECT reference,status,ticket_type,quantity,total,name,phone,email,created_at FROM orders ORDER BY created_at DESC LIMIT 500`)
	defer rows.Close()
	orders := []order{}
	for rows.Next() {
		var o order
		rows.Scan(&o.Reference, &o.Status, &o.TicketType, &o.Quantity, &o.Total, &o.Name, &o.Phone, &o.Email, &o.CreatedAt)
		orders = append(orders, o)
	}
	jsonOut(w, 200, map[string]interface{}{"paid_orders": paid, "pending_orders": pending, "revenue": revenue, "orders": orders})
}
func (a *app) csv(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=orders.csv")
	out := csv.NewWriter(w)
	out.Write([]string{"reference", "status", "type", "quantity", "total", "name", "phone", "email", "created_at"})
	rows, _ := a.db.Query(`SELECT reference,status,ticket_type,quantity,total,name,phone,email,created_at FROM orders ORDER BY created_at DESC`)
	defer rows.Close()
	for rows.Next() {
		var o order
		rows.Scan(&o.Reference, &o.Status, &o.TicketType, &o.Quantity, &o.Total, &o.Name, &o.Phone, &o.Email, &o.CreatedAt)
		out.Write([]string{o.Reference, o.Status, o.TicketType, strconv.Itoa(o.Quantity), strconv.Itoa(o.Total), o.Name, o.Phone, o.Email, o.CreatedAt})
	}
	out.Flush()
}
func (a *app) adminOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/mark-paid") {
		jsonOut(w, 405, nil)
		return
	}
	ref := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/admin/orders/"), "/mark-paid")
	if err := a.markPaid(ref); err != nil {
		jsonOut(w, 400, map[string]string{"error": "Could not mark order paid"})
		return
	}
	jsonOut(w, 200, map[string]string{"status": "paid"})
}
func (a *app) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		rows, _ := a.db.Query(`SELECT key,value FROM settings`)
		defer rows.Close()
		out := map[string]string{}
		for rows.Next() {
			var k, v string
			rows.Scan(&k, &v)
			out[k] = v
		}
		jsonOut(w, 200, out)
		return
	}
	if r.Method != "PUT" {
		jsonOut(w, 405, nil)
		return
	}
	var values map[string]string
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&values) != nil {
		jsonOut(w, 400, nil)
		return
	}
	for _, k := range []string{"male_price", "female_price", "male_capacity", "female_capacity", "sales_open"} {
		if v, ok := values[k]; ok {
			if _, err := strconv.Atoi(v); k != "sales_open" && err != nil {
				jsonOut(w, 400, map[string]string{"error": "Invalid setting"})
				return
			}
			a.db.Exec(`UPDATE settings SET value=? WHERE key=?`, v, k)
		}
	}
	jsonOut(w, 200, map[string]string{"status": "ok"})
}
func (a *app) scan(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonOut(w, 405, nil)
		return
	}
	var v struct {
		Code string `json:"code"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&v)
	tx, _ := a.db.Begin()
	defer tx.Rollback()
	var used int
	var ref string
	err := tx.QueryRow(`SELECT used,reference FROM tickets WHERE code=?`, strings.TrimSpace(v.Code)).Scan(&used, &ref)
	if errors.Is(err, sql.ErrNoRows) {
		jsonOut(w, 404, map[string]string{"error": "Ticket not found"})
		return
	}
	if used == 1 {
		jsonOut(w, 409, map[string]string{"error": "Ticket already used"})
		return
	}
	tx.Exec(`UPDATE tickets SET used=1,used_at=? WHERE code=?`, time.Now().UTC().Format(time.RFC3339), v.Code)
	tx.Commit()
	jsonOut(w, 200, map[string]string{"status": "valid", "reference": ref})
}
func (a *app) allow(r *http.Request, max int, window time.Duration) bool {
	ip := strings.Split(r.RemoteAddr, ":")[0]
	now := time.Now()
	a.limiter.mu.Lock()
	defer a.limiter.mu.Unlock()
	hits := a.limiter.hits[ip]
	kept := hits[:0]
	for _, t := range hits {
		if now.Sub(t) < window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= max {
		a.limiter.hits[ip] = kept
		return false
	}
	a.limiter.hits[ip] = append(kept, now)
	return true
}
func newReference() string  { return "VF3-" + newRandom(12) }
func newTicketCode() string { return "VF3-" + newRandom(10) }
func newRandom(n int) string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}
func jsonOut(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

var _ = fmt.Sprintf
