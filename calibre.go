package main

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Library reads books from a local copy of Calibre's metadata.db, which it
// downloads from the publicly hosted library folder. Nothing is cached in
// memory: every page queries SQLite, which keeps the process small.
type Library struct {
	baseURL *url.URL
	dbPath  string

	mu sync.RWMutex
	db *sql.DB
}

type Book struct {
	ID       int64
	Title    string
	Path     string
	Authors  []string
	Comments []string
	Files    []string // paths relative to the library root, e.g. "Author/Title (1)/Title - Author.mobi"
	Langs    []string

	lib *Library
}

type Language struct {
	Code string
}

func NewLibrary(baseURL, dbPath string) (*Library, error) {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("CALIBRE_FOLDER_BASE_URL: %w", err)
	}
	return &Library{baseURL: u, dbPath: dbPath}, nil
}

// URLFor turns a library-relative path into a public URL, escaping each segment.
func (l *Library) URLFor(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	ref, err := url.Parse(strings.Join(segs, "/"))
	if err != nil {
		return ""
	}
	return l.baseURL.ResolveReference(ref).String()
}

// Refresh downloads metadata.db and swaps it in. On failure the current copy stays.
func (l *Library) Refresh() error {
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(l.URLFor("metadata.db"))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading metadata.db: %s", resp.Status)
	}

	tmp, err := os.CreateTemp(path.Dir(l.dbPath), ".metadata-*.db")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.db != nil {
		l.db.Close()
		l.db = nil
	}
	if err := os.Rename(tmp.Name(), l.dbPath); err != nil {
		return err
	}
	return l.open()
}

// Open opens the local copy without downloading.
func (l *Library) Open() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.open()
}

func (l *Library) open() error {
	db, err := sql.Open("sqlite", "file:"+l.dbPath+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return err
	}
	l.db = db
	return nil
}

func (l *Library) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.db != nil {
		l.db.Close()
		l.db = nil
	}
}

func (l *Library) conn() (*sql.DB, func(), error) {
	l.mu.RLock()
	if l.db == nil {
		l.mu.RUnlock()
		return nil, nil, fmt.Errorf("library database is not loaded")
	}
	return l.db, l.mu.RUnlock, nil
}

func (l *Library) Languages() ([]Language, error) {
	db, done, err := l.conn()
	if err != nil {
		return nil, err
	}
	defer done()
	rows, err := db.Query(`SELECT lang_code FROM languages ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Language
	for rows.Next() {
		var lg Language
		if err := rows.Scan(&lg.Code); err != nil {
			return nil, err
		}
		out = append(out, lg)
	}
	return out, rows.Err()
}

// Books lists books newest first, optionally only those in the given language.
func (l *Library) Books(lang string) ([]*Book, error) {
	db, done, err := l.conn()
	if err != nil {
		return nil, err
	}
	defer done()

	q := `SELECT id, title, path FROM books ORDER BY id DESC`
	args := []any{}
	if lang != "" {
		q = `SELECT b.id, b.title, b.path FROM books b
		     WHERE EXISTS (SELECT 1 FROM books_languages_link bl JOIN languages lg ON lg.id = bl.lang_code
		                   WHERE bl.book = b.id AND lg.lang_code = ?)
		     ORDER BY b.id DESC`
		args = append(args, lang)
	}
	books, err := l.scanBooks(db, q, args...)
	if err != nil {
		return nil, err
	}
	return books, l.loadRelations(db, books)
}

// Book returns one book, or nil when it does not exist.
func (l *Library) Book(id int64) (*Book, error) {
	db, done, err := l.conn()
	if err != nil {
		return nil, err
	}
	defer done()
	books, err := l.scanBooks(db, `SELECT id, title, path FROM books WHERE id = ?`, id)
	if err != nil || len(books) == 0 {
		return nil, err
	}
	return books[0], l.loadRelations(db, books)
}

func (l *Library) scanBooks(db *sql.DB, q string, args ...any) ([]*Book, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Book
	for rows.Next() {
		b := &Book{lib: l}
		if err := rows.Scan(&b.ID, &b.Title, &b.Path); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// loadRelations fills authors, comments, files and languages with one query each.
func (l *Library) loadRelations(db *sql.DB, books []*Book) error {
	if len(books) == 0 {
		return nil
	}
	byID := make(map[int64]*Book, len(books))
	ids := make([]string, len(books))
	for i, b := range books {
		byID[b.ID] = b
		ids[i] = fmt.Sprint(b.ID)
	}
	in := strings.Join(ids, ",") // integers from SQLite, safe to inline

	each := func(q string, fn func(b *Book, v string)) error {
		rows, err := db.Query(q)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var v string
			if err := rows.Scan(&id, &v); err != nil {
				return err
			}
			if b := byID[id]; b != nil {
				fn(b, v)
			}
		}
		return rows.Err()
	}

	if err := each(`SELECT l.book, a.name FROM books_authors_link l JOIN authors a ON a.id = l.author
	                WHERE l.book IN (`+in+`) ORDER BY l.id`,
		func(b *Book, v string) { b.Authors = append(b.Authors, v) }); err != nil {
		return err
	}
	if err := each(`SELECT book, text FROM comments WHERE book IN (`+in+`) ORDER BY id`,
		func(b *Book, v string) { b.Comments = append(b.Comments, v) }); err != nil {
		return err
	}
	if err := each(`SELECT book, name || '.' || lower(format) FROM data WHERE book IN (`+in+`) ORDER BY id`,
		func(b *Book, v string) { b.Files = append(b.Files, b.Path+"/"+v) }); err != nil {
		return err
	}
	return each(`SELECT l.book, lg.lang_code FROM books_languages_link l JOIN languages lg ON lg.id = l.lang_code
	             WHERE l.book IN (`+in+`) ORDER BY l.item_order, l.id`,
		func(b *Book, v string) { b.Langs = append(b.Langs, v) })
}

func (b *Book) ImageURL() string { return b.lib.URLFor(b.Path + "/cover.jpg") }

func (b *Book) AuthorsName() string {
	return strings.ReplaceAll(strings.Join(b.Authors, ", "), "|", "")
}

func (b *Book) CommentsText() string { return strings.Join(b.Comments, ", ") }

func (b *Book) Language() string {
	if len(b.Langs) > 0 {
		return b.Langs[0]
	}
	return "spa"
}

var filePriority = []string{"mobi", "pdf", "doc", "docx", "html", "htm", "rtf"}

func extension(p string) string {
	if i := strings.LastIndex(p, "."); i >= 0 {
		return p[i+1:]
	}
	return p
}

// BookFile picks the format to email, preferring Kindle-friendly ones.
func (b *Book) BookFile() string {
	if len(b.Files) == 0 {
		return ""
	}
	rank := func(p string) int {
		for i, e := range filePriority {
			if e == extension(p) {
				return i
			}
		}
		return len(filePriority)
	}
	files := append([]string(nil), b.Files...)
	sort.SliceStable(files, func(i, j int) bool { return rank(files[i]) < rank(files[j]) })
	return files[0]
}

func (b *Book) BookFileName() string { return path.Base(b.BookFile()) }

func (b *Book) BookURL() string {
	if f := b.BookFile(); f != "" {
		return b.lib.URLFor(f)
	}
	return ""
}

func (b *Book) PreviewURL() string {
	for _, f := range b.Files {
		if extension(f) == "pdf" {
			return b.lib.URLFor(f)
		}
	}
	return ""
}
