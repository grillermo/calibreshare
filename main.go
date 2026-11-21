// CalibreShare: friends enter their Kindle email and pick books from a
// publicly hosted Calibre library; the chosen file is emailed through Mailgun.
package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

//go:embed views/*.html
var viewsFS embed.FS

//go:embed static
var staticFS embed.FS

type Config struct {
	Port         string
	LibraryURL   string
	DBPath       string
	AppName      string
	DefaultEmail string
	Secret       string
	MailgunKey   string
	MailgunBase  string
	MailgunDom   string
}

func configFromEnv() Config {
	get := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	return Config{
		Port:         get("PORT", "3000"),
		LibraryURL:   os.Getenv("CALIBRE_FOLDER_BASE_URL"),
		DBPath:       get("METADATA_DB", "metadata.db"),
		AppName:      os.Getenv("APPLICATION_NAME"),
		DefaultEmail: os.Getenv("DEFAULT_EMAIL"),
		Secret:       os.Getenv("SECRET_KEY_BASE"),
		MailgunKey:   os.Getenv("MAILGUN_API_KEY"),
		MailgunBase:  get("MAILGUN_API_BASE", "https://api.mailgun.net/v3"),
		MailgunDom:   os.Getenv("MAILGUN_DOMAIN"),
	}
}

// loadDotenv sets variables from a .env file without overriding the environment.
func loadDotenv(name string) {
	f, err := os.Open(name)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
}

type App struct {
	cfg    Config
	lib    *Library
	mailer interface{ SendBookAsync(url, name, to string) }
	tmpl   map[string]*template.Template
}

func NewApp(cfg Config, lib *Library, mailer interface{ SendBookAsync(url, name, to string) }) *App {
	a := &App{cfg: cfg, lib: lib, mailer: mailer, tmpl: map[string]*template.Template{}}
	funcs := template.FuncMap{
		"languageName": languageInSpanish,
		"titleize":     titleize,
		"withBook":     func(p page, b *Book) page { p.Book = b; return p },
		"safe":         func(s string) template.HTML { return template.HTML(s) }, // Calibre comments are HTML
	}
	for _, page := range []string{"index", "books", "book", "send_book", "sources"} {
		a.tmpl[page] = template.Must(template.New("layout.html").Funcs(funcs).
			ParseFS(viewsFS, "views/layout.html", "views/_book.html", "views/"+page+".html"))
	}
	return a
}

func (a *App) Routes() http.Handler {
	static, _ := fs.Sub(staticFS, "static")
	files := http.FileServerFS(static)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("POST /{$}", a.index)
	mux.HandleFunc("POST /register_email", a.registerEmail)
	mux.HandleFunc("GET /libros", a.books)
	mux.HandleFunc("GET /libro/{id}", a.book)
	mux.HandleFunc("POST /send_book", a.sendBook)
	mux.HandleFunc("GET /send_book", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/libros", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /clear_cache", a.clearCache)
	mux.HandleFunc("GET /fuentes", a.sources)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		files.ServeHTTP(w, r)
	})
	return logRequests(mux)
}

// --- session: the registered email in an HMAC-signed cookie ---

const sessionCookie = "email"

func (a *App) sign(v string) string {
	mac := hmac.New(sha256.New, []byte(a.cfg.Secret))
	mac.Write([]byte(v))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) email(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	raw, sig, ok := strings.Cut(c.Value, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(a.sign(raw))) {
		return ""
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return ""
	}
	return string(b)
}

func (a *App) setEmail(w http.ResponseWriter, email string) {
	c := &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode}
	if email == "" {
		c.MaxAge = -1
	} else {
		raw := base64.RawURLEncoding.EncodeToString([]byte(email))
		c.Value = raw + "." + a.sign(raw)
		c.Expires = time.Now().AddDate(1, 0, 0)
	}
	http.SetCookie(w, c)
}

// requireEmail mirrors the Rails before_actions: accept ?email= to preregister,
// otherwise send unregistered visitors to the instructions page.
func (a *App) requireEmail(w http.ResponseWriter, r *http.Request) (string, bool) {
	email := a.email(r)
	if email == "" {
		if e := r.URL.Query().Get("email"); e != "" {
			a.setEmail(w, e)
			return e, true
		}
		http.Redirect(w, r, "/", http.StatusFound)
		return "", false
	}
	return email, true
}

// --- handlers ---

type page struct {
	AppName      string
	DefaultEmail string
	IsRoot       bool
	Email        string
	Languages    []Language
	Language     string
	Books        []*Book
	Book         *Book
}

func (a *App) page(r *http.Request, email string) page {
	return page{
		AppName:      a.cfg.AppName,
		DefaultEmail: a.cfg.DefaultEmail,
		IsRoot:       r.URL.Path == "/",
		Email:        email,
		Language:     r.URL.Query().Get("language"),
	}
}

func (a *App) render(w http.ResponseWriter, name string, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl[name].Execute(w, p); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err)
	http.Error(w, "Algo salió mal", http.StatusInternalServerError)
}

func booksURL(lang string, bookID int64) string {
	u := "/libros?language=" + url.QueryEscape(lang)
	if bookID != 0 {
		u += "#book" + strconv.FormatInt(bookID, 10)
	}
	return u
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	a.render(w, "index", a.page(r, a.email(r)))
}

func (a *App) registerEmail(w http.ResponseWriter, r *http.Request) {
	a.setEmail(w, strings.TrimSpace(r.FormValue("email")))
	http.Redirect(w, r, booksURL("spa", 0), http.StatusFound)
}

func (a *App) books(w http.ResponseWriter, r *http.Request) {
	email, ok := a.requireEmail(w, r)
	if !ok {
		return
	}
	p := a.page(r, email)
	var err error
	if p.Books, err = a.lib.Books(p.Language); err != nil {
		serverError(w, err)
		return
	}
	if p.Languages, err = a.lib.Languages(); err != nil {
		serverError(w, err)
		return
	}
	a.render(w, "books", p)
}

func (a *App) findBook(w http.ResponseWriter, r *http.Request, raw string) *Book {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return nil
	}
	b, err := a.lib.Book(id)
	if err != nil {
		serverError(w, err)
		return nil
	}
	if b == nil {
		http.NotFound(w, r)
	}
	return b
}

func (a *App) book(w http.ResponseWriter, r *http.Request) {
	email, ok := a.requireEmail(w, r)
	if !ok {
		return
	}
	b := a.findBook(w, r, r.PathValue("id"))
	if b == nil {
		return
	}
	p := a.page(r, email)
	p.Book = b
	a.render(w, "book", p)
}

func (a *App) sendBook(w http.ResponseWriter, r *http.Request) {
	email := a.email(r)
	if email == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	b := a.findBook(w, r, r.FormValue("book_id"))
	if b == nil {
		return
	}
	if b.BookFile() == "" {
		http.Error(w, "Este libro no tiene archivos", http.StatusUnprocessableEntity)
		return
	}
	a.mailer.SendBookAsync(b.BookURL(), b.BookFileName(), email)
	p := a.page(r, email)
	p.Book = b
	a.render(w, "send_book", p)
}

func (a *App) clearCache(w http.ResponseWriter, r *http.Request) {
	if err := a.lib.Refresh(); err != nil {
		serverError(w, err)
		return
	}
	a.setEmail(w, "")
	http.Redirect(w, r, booksURL("spa", 0), http.StatusFound)
}

func (a *App) sources(w http.ResponseWriter, r *http.Request) {
	a.render(w, "sources", a.page(r, a.email(r)))
}

// --- helpers ---

func languageInSpanish(code string) string {
	switch code {
	case "eng":
		return "Inglés"
	case "spa":
		return "Español"
	case "fra":
		return "Francés"
	}
	return code
}

// titleize matches Rails' String#titleize for simple names: "grille_books" -> "Grille Books".
func titleize(s string) string {
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(s))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
	}
	return strings.Join(words, " ")
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		h.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Microsecond))
	})
}

func main() {
	loadDotenv(".env")
	cfg := configFromEnv()
	if cfg.LibraryURL == "" {
		log.Fatal("CALIBRE_FOLDER_BASE_URL is required")
	}
	if cfg.Secret == "" {
		log.Fatal("SECRET_KEY_BASE is required (it signs the session cookie)")
	}

	lib, err := NewLibrary(cfg.LibraryURL, cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := lib.Refresh(); err != nil {
		log.Printf("could not download metadata.db (%v); using local copy", err)
		if err := lib.Open(); err != nil {
			log.Fatalf("no usable %s: %v", cfg.DBPath, err)
		}
	}
	defer lib.Close()

	mailer := &Mailer{
		APIBase: strings.TrimSuffix(cfg.MailgunBase, "/"),
		APIKey:  cfg.MailgunKey,
		Domain:  cfg.MailgunDom,
		From:    "Grille envios <" + cfg.DefaultEmail + ">",
		AppName: cfg.AppName,
		Client:  &http.Client{Timeout: 10 * time.Minute},
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           NewApp(cfg, lib, mailer).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
