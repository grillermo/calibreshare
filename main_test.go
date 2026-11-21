package main

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixtureDB writes a metadata.db with the subset of Calibre's schema the app reads.
func fixtureDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE books (id INTEGER PRIMARY KEY, title TEXT, path TEXT, has_cover BOOL)`,
		`CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE books_authors_link (id INTEGER PRIMARY KEY, book INTEGER, author INTEGER)`,
		`CREATE TABLE comments (id INTEGER PRIMARY KEY, book INTEGER, text TEXT)`,
		`CREATE TABLE data (id INTEGER PRIMARY KEY, book INTEGER, format TEXT, name TEXT)`,
		`CREATE TABLE languages (id INTEGER PRIMARY KEY, lang_code TEXT)`,
		`CREATE TABLE books_languages_link (id INTEGER PRIMARY KEY, book INTEGER, lang_code INTEGER, item_order INTEGER DEFAULT 0)`,
		`INSERT INTO books VALUES (1, 'The Forerunner', 'Khalil Gibran/The Forerunner (1)', 1)`,
		`INSERT INTO books VALUES (2, 'Cien años', 'Gabriel Garcia/Cien años (2)', 1)`,
		`INSERT INTO authors VALUES (1, 'Khalil Gibran'), (2, 'Gabriel|Garcia')`,
		`INSERT INTO books_authors_link VALUES (1, 1, 1), (2, 2, 2)`,
		`INSERT INTO comments VALUES (1, 1, '<p>Parables &amp; poems</p>')`,
		`INSERT INTO data VALUES (1, 1, 'EPUB', 'The Forerunner - Khalil Gibran'),
		                         (2, 1, 'PDF', 'The Forerunner - Khalil Gibran'),
		                         (3, 1, 'MOBI', 'The Forerunner - Khalil Gibran'),
		                         (4, 2, 'EPUB', 'Cien años - Gabriel Garcia')`,
		`INSERT INTO languages VALUES (1, 'eng'), (2, 'spa')`,
		`INSERT INTO books_languages_link (id, book, lang_code) VALUES (1, 1, 1), (2, 2, 2)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

type sentMail struct {
	form       url.Values
	fileName   string
	attachment string
}

type harness struct {
	app    *httptest.Server
	client *http.Client
	mails  chan sentMail
	lib    *Library
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	srcDB := filepath.Join(dir, "remote-metadata.db")
	fixtureDB(t, srcDB)

	// The publicly hosted Calibre library folder.
	library := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/books/metadata.db":
			http.ServeFile(w, r, srcDB)
		case "/books/Khalil Gibran/The Forerunner (1)/The Forerunner - Khalil Gibran.mobi":
			io.WriteString(w, "MOBI-BYTES")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(library.Close)

	mails := make(chan sentMail, 4)
	mailgun := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, _ := r.BasicAuth(); user != "api" || pass != "key-123" || r.URL.Path != "/v3/mg.example.com/messages" {
			http.Error(w, "bad request", http.StatusUnauthorized)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		f, h, err := r.FormFile("attachment")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		body, _ := io.ReadAll(f)
		mails <- sentMail{form: url.Values(r.MultipartForm.Value), fileName: h.Filename, attachment: string(body)}
		io.WriteString(w, `{"message":"Queued"}`)
	}))
	t.Cleanup(mailgun.Close)

	cfg := Config{
		LibraryURL:   library.URL + "/books",
		DBPath:       filepath.Join(dir, "metadata.db"),
		AppName:      "grille_books",
		DefaultEmail: "books@example.com",
		Secret:       "s3cret",
	}
	lib, err := NewLibrary(cfg.LibraryURL, cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.Refresh(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lib.Close)

	mailer := &Mailer{APIBase: mailgun.URL + "/v3", APIKey: "key-123", Domain: "mg.example.com",
		From: "Grille envios <books@example.com>", AppName: cfg.AppName, Client: http.DefaultClient}
	app := httptest.NewServer(NewApp(cfg, lib, mailer).Routes())
	t.Cleanup(app.Close)

	jar, _ := cookiejar.New(nil)
	return &harness{app: app, mails: mails, lib: lib, client: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (h *harness) do(t *testing.T, method, path string, form url.Values) (int, http.Header, string) {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, h.app.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

func mustContain(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("body missing %q", w)
		}
	}
}

func TestIndexShowsSenderEmail(t *testing.T) {
	h := newHarness(t)
	code, _, body := h.do(t, "GET", "/", nil)
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "books@example.com", "<title>grille_books</title>", `action="/register_email"`)
}

func TestBooksRequireRegisteredEmail(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/libros", "/libro/1"} {
		code, hdr, _ := h.do(t, "GET", p, nil)
		if code != http.StatusFound || hdr.Get("Location") != "/" {
			t.Errorf("%s: got %d %q, want redirect to /", p, code, hdr.Get("Location"))
		}
	}
}

func TestRegisterAndBrowse(t *testing.T) {
	h := newHarness(t)
	code, hdr, _ := h.do(t, "POST", "/register_email", url.Values{"email": {"friend@kindle.com"}})
	if code != http.StatusFound || hdr.Get("Location") != "/libros?language=spa" {
		t.Fatalf("register: %d %q", code, hdr.Get("Location"))
	}

	_, _, body := h.do(t, "GET", "/libros", nil)
	mustContain(t, body,
		`id="book1"`, `id="book2"`, "The Forerunner", "GabrielGarcia",
		"<p>Parables &amp; poems</p>", // Calibre comment HTML is rendered, not escaped
		"vista previa(pdf)", "enviar a friend@kindle.com",
		`<a href="/">Grille Books</a>`, ">Inglés<", ">Español<",
		h.lib.baseURL.String()+"Khalil%20Gibran/The%20Forerunner%20%281%29/cover.jpg")
	if strings.Index(body, `id="book2"`) > strings.Index(body, `id="book1"`) {
		t.Error("books should be newest first")
	}

	_, _, body = h.do(t, "GET", "/libros?language=eng", nil)
	mustContain(t, body, `id="book1"`, `<h2 class="language">Inglés</h2>`)
	if strings.Contains(body, `id="book2"`) {
		t.Error("language filter should hide the Spanish book")
	}

	_, _, body = h.do(t, "GET", "/libro/1", nil)
	mustContain(t, body, "The Forerunner", "/libros?language=eng#book1")

	if code, _, _ := h.do(t, "GET", "/libro/999", nil); code != 404 {
		t.Errorf("missing book: %d", code)
	}
}

func TestPreregisterViaQueryParam(t *testing.T) {
	h := newHarness(t)
	code, _, body := h.do(t, "GET", "/libros?email=q@kindle.com", nil)
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "enviar a q@kindle.com")
	_, _, body = h.do(t, "GET", "/libro/2", nil)
	mustContain(t, body, "enviar a q@kindle.com")
}

func TestForgedCookieIsIgnored(t *testing.T) {
	h := newHarness(t)
	u, _ := url.Parse(h.app.URL)
	h.client.Jar.SetCookies(u, []*http.Cookie{{Name: "email", Value: "ZXZpbEBleC5jb20.bad"}})
	if code, _, _ := h.do(t, "GET", "/libros", nil); code != http.StatusFound {
		t.Errorf("forged cookie accepted: %d", code)
	}
}

func TestSendBookEmailsPreferredFormat(t *testing.T) {
	h := newHarness(t)
	h.do(t, "POST", "/register_email", url.Values{"email": {"friend@kindle.com"}})
	code, _, body := h.do(t, "POST", "/send_book", url.Values{"book_id": {"1"}})
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	mustContain(t, body, "Enviando...", "Libro: The Forerunner", "Email: friend@kindle.com")

	select {
	case m := <-h.mails:
		if m.fileName != "The Forerunner - Khalil Gibran.mobi" || m.attachment != "MOBI-BYTES" {
			t.Errorf("attachment %q = %q", m.fileName, m.attachment)
		}
		if m.form.Get("to") != "friend@kindle.com" || m.form.Get("from") != "Grille envios <books@example.com>" ||
			m.form.Get("subject") != "The Forerunner - Khalil Gibran.mobi" || m.form.Get("text") != "Tu libro desde grille_books" {
			t.Errorf("fields: %v", m.form)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no mail sent")
	}

	if code, hdr, _ := h.do(t, "GET", "/send_book", nil); code != 301 || hdr.Get("Location") != "/libros" {
		t.Errorf("GET /send_book: %d %q", code, hdr.Get("Location"))
	}
}

func TestClearCacheRefreshesAndLogsOut(t *testing.T) {
	h := newHarness(t)
	h.do(t, "POST", "/register_email", url.Values{"email": {"friend@kindle.com"}})
	before, _ := os.Stat(h.lib.dbPath)
	code, hdr, _ := h.do(t, "GET", "/clear_cache", nil)
	if code != http.StatusFound || hdr.Get("Location") != "/libros?language=spa" {
		t.Fatalf("clear_cache: %d %q", code, hdr.Get("Location"))
	}
	after, _ := os.Stat(h.lib.dbPath)
	if os.SameFile(before, after) {
		t.Error("metadata.db was not replaced")
	}
	if code, _, _ := h.do(t, "GET", "/libros", nil); code != http.StatusFound {
		t.Error("session should be cleared")
	}
	books, err := h.lib.Books("")
	if err != nil || len(books) != 2 {
		t.Errorf("after refresh: %v, %d books", err, len(books))
	}
}

func TestStaticAndSourcesPages(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/application.css", "/kindle-email.jpg", "/robots.txt"} {
		if code, _, _ := h.do(t, "GET", p, nil); code != 200 {
			t.Errorf("%s: %d", p, code)
		}
	}
	_, _, body := h.do(t, "GET", "/fuentes", nil)
	mustContain(t, body, "Library Genesis", "Standard Ebooks")
}

func TestConcurrentReadsDuringRefresh(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%5 == 0 {
				if err := h.lib.Refresh(); err != nil {
					t.Error(err)
				}
				return
			}
			if _, err := h.lib.Books(""); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
}
