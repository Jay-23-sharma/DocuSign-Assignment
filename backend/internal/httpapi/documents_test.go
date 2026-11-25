package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"signing-app/backend/internal/auth"
	"signing-app/backend/internal/documents"
)

type docFixture struct {
	t       *testing.T
	pool    *pgxpool.Pool
	store   documents.Store
	router  http.Handler
	cookies *auth.SenderCookies
	pdf     []byte
}

func fixture(t *testing.T) *docFixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to a migrated test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	pdf, err := os.ReadFile("testdata/three-pages.pdf")
	if err != nil {
		t.Fatal(err)
	}
	store, err := documents.NewStore(pool, t.TempDir(), fmt.Sprint(len(pdf)))
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := auth.NewSenderCookies(base64.StdEncoding.EncodeToString(make([]byte, 32)), false)
	if err != nil {
		t.Fatal(err)
	}
	return &docFixture{t, pool, store, Handler(pool, nil, cookies, store), cookies, pdf}
}

func (f *docFixture) sender() (int64, *http.Cookie) {
	ctx := context.Background()
	email := rand.Text() + "@example.test"
	var id int64
	err := f.pool.QueryRow(ctx, `INSERT INTO users(name,email,password_hash,email_verified_at)
 VALUES('Upload fixture',$1,'fixture',now()) RETURNING id`, email).Scan(&id)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() {
		for _, q := range []string{"DELETE FROM signing_email_codes WHERE session_id IN (SELECT ss.id FROM signing_sessions ss JOIN signing_links l ON l.id=ss.signing_link_id JOIN recipients r ON r.id=l.recipient_id JOIN documents d ON d.id=r.document_id WHERE d.owner_id=$1)", "DELETE FROM signing_sessions WHERE signing_link_id IN (SELECT l.id FROM signing_links l JOIN recipients r ON r.id=l.recipient_id JOIN documents d ON d.id=r.document_id WHERE d.owner_id=$1)", "DELETE FROM document_events WHERE document_id IN (SELECT id FROM documents WHERE owner_id=$1)", "DELETE FROM signing_links WHERE recipient_id IN (SELECT r.id FROM recipients r JOIN documents d ON d.id=r.document_id WHERE d.owner_id=$1)", "DELETE FROM document_fields WHERE document_id IN (SELECT id FROM documents WHERE owner_id=$1)", "DELETE FROM recipients WHERE document_id IN (SELECT id FROM documents WHERE owner_id=$1)", "DELETE FROM document_files WHERE document_id IN (SELECT id FROM documents WHERE owner_id=$1)", "DELETE FROM documents WHERE owner_id=$1", "DELETE FROM users WHERE id=$1"} {
			if _, err := f.pool.Exec(ctx, q, id); err != nil {
				f.t.Error(err)
			}
		}
	})
	c, err := f.cookies.Issue(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return id, c
}

type part struct {
	name, filename string
	data           []byte
}

func (f *docFixture) upload(c *http.Cookie, parts ...part) *httptest.ResponseRecorder {
	f.t.Helper()
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	for _, p := range parts {
		if p.filename == "" {
			if err := m.WriteField(p.name, string(p.data)); err != nil {
				f.t.Fatal(err)
			}
			continue
		}
		w, err := m.CreateFormFile(p.name, p.filename)
		if err != nil {
			f.t.Fatal(err)
		}
		if _, err = w.Write(p.data); err != nil {
			f.t.Fatal(err)
		}
	}
	m.Close()
	r := httptest.NewRequest("POST", "/api/documents", &body)
	r.Header.Set("Content-Type", m.FormDataContentType())
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}

func TestUploadBoundaryAndCleanup(t *testing.T) {
	f := fixture(t)
	owner, c := f.sender()
	encrypted, err := os.ReadFile("testdata/protected.pdf")
	if err != nil {
		t.Fatal(err)
	}
	// Exercise encryption rejection independently of the smaller boundary limit.
	larger := f.store
	larger.MaxBytes = int64(len(encrypted) + len(f.pdf))
	f.router = Handler(f.pool, nil, f.cookies, larger)
	good := part{"file", "three.PDF", f.pdf}
	for _, tc := range []struct {
		name   string
		parts  []part
		status int
	}{
		{"missing", nil, 400}, {"empty", []part{{"file", "bad.pdf", nil}}, 400},
		{"malformed", []part{{"file", "bad.pdf", []byte("%PDF-invalid")}}, 400},
		{"encrypted", []part{{"file", "encrypted.pdf", encrypted}}, 400},
		{"duplicate file", []part{good, good}, 400},
		{"duplicate title", []part{good, {"title", "", nil}, {"title", "", nil}}, 400},
		{"oversized title", []part{good, {"title", "", make([]byte, titleBytes+1)}}, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.upload(c, tc.parts...)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
		})
	}
	f.router = Handler(f.pool, nil, f.cookies, f.store)
	if w := f.upload(c, part{"file", "big.pdf", append(bytes.Clone(f.pdf), ' ')}); w.Code != 413 {
		t.Fatal(w.Code)
	}
	var count int
	ctx := context.Background()
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM documents WHERE owner_id=$1", owner).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected upload left a draft", err)
	}
	entries, err := os.ReadDir(f.store.Dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected upload left files", err)
	}
	w := f.upload(c, good, part{"title", "", []byte("  ")})
	if w.Code != 201 {
		t.Fatalf("exact size upload: %d %s", w.Code, w.Body)
	}
	var draft documents.Draft
	if err := json.Unmarshal(w.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Title != "three" || draft.Status != "draft" {
		t.Fatal(draft)
	}
	var dbOwner, size int64
	var pages, files, recipients int
	var digest []byte
	var key, filename string
	err = f.pool.QueryRow(ctx, `SELECT d.owner_id,f.size_bytes,f.page_count,f.sha256,f.storage_key,f.original_filename,
 (SELECT count(*) FROM document_files WHERE document_id=d.id),(SELECT count(*) FROM recipients WHERE document_id=d.id)
 FROM documents d JOIN document_files f ON f.document_id=d.id WHERE d.id=$1 AND f.kind='original' AND d.status='draft'`, draft.ID).Scan(&dbOwner, &size, &pages, &digest, &key, &filename, &files, &recipients)
	sum := sha256.Sum256(f.pdf)
	if err != nil || dbOwner != owner || size != int64(len(f.pdf)) || pages != 3 || files != 1 || recipients != 0 || filename != "three.PDF" || !bytes.Equal(digest, sum[:]) {
		t.Fatal("incorrect persisted metadata", err)
	}
	saved, err := os.ReadFile(filepath.Join(f.store.Dir, key))
	if err != nil || !bytes.Equal(saved, f.pdf) {
		t.Fatal("stored bytes mismatch", err)
	}
}

func TestDocumentOwnershipAndLiteralSearch(t *testing.T) {
	f := fixture(t)
	owner, a := f.sender()
	_, b := f.sender()
	add := func(title string) documents.Draft {
		w := f.upload(a, part{"title", "", []byte(title)}, part{"file", "fixture.pdf", f.pdf})
		var draft documents.Draft
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &draft) != nil {
			t.Fatal("upload failed", w.Code)
		}
		return draft
	}
	first := add("One 50%_share")
	second := add("Two 50XXshare")
	get := func(url string, c *http.Cookie, byteRange string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", url, nil)
		if c != nil {
			r.AddCookie(c)
		}
		if byteRange != "" {
			r.Header.Set("Range", byteRange)
		}
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		return w
	}
	checkList := func(query string, c *http.Cookie, ids ...int64) {
		t.Helper()
		w := get("/api/documents"+query, c, "")
		var list []documents.Summary
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || list == nil || len(list) != len(ids) {
			t.Fatalf("unexpected list %s: %s", query, w.Body)
		}
		for i, id := range ids {
			if list[i].ID != id || list[i].CreatedAt.IsZero() {
				t.Fatal(query, list)
			}
		}
	}
	checkList("", a, second.ID, first.ID)
	if _, err := f.pool.Exec(context.Background(), "UPDATE documents SET created_at=now() WHERE owner_id=$1", owner); err != nil {
		t.Fatal(err)
	}
	checkList("?status=draft", a, second.ID, first.ID)
	checkList("?q=SHaRe", a, second.ID, first.ID)
	checkList("?q=%25_", a, first.ID)
	checkList("?q=_&status=draft", a, first.ID)
	checkList("?q=share&status=sent", a)
	checkList("", b)
	url := fmt.Sprintf("/api/documents/%d", first.ID)
	w := get(url, a, "")
	var detail documents.Detail
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.Original == nil || detail.Original.Pages != 3 || detail.Original.Size != int64(len(f.pdf)) {
		t.Fatal("missing metadata")
	}
	fileURL := url + "/files/original"
	w = get(fileURL, a, "bytes=0-4")
	if w.Code != 206 || w.Body.String() != "%PDF-" || w.Header().Get("Content-Type") != "application/pdf" {
		t.Fatal("range failed")
	}
	w = get(fileURL, a, "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), f.pdf) {
		t.Fatal("full read failed")
	}
	for _, suffix := range []string{"", "/files/original"} {
		foreign := get(url+suffix, b, "")
		missing := get("/api/documents/9223372036854775807"+suffix, b, "")
		if foreign.Code != 404 || missing.Code != 404 || foreign.Body.String() != missing.Body.String() {
			t.Fatal("ownership leak")
		}
	}
	for _, url := range []string{"/api/documents", url, fileURL} {
		if get(url, nil, "").Code != 401 {
			t.Fatal("anonymous read accepted")
		}
	}
	if f.upload(nil).Code != 401 {
		t.Fatal("anonymous upload accepted")
	}
	var emptyID int64
	if err := f.pool.QueryRow(context.Background(), "INSERT INTO documents(owner_id,title) VALUES($1,'empty') RETURNING id", owner).Scan(&emptyID); err != nil {
		t.Fatal(err)
	}
	emptyURL := fmt.Sprintf("/api/documents/%d", emptyID)
	w = get(emptyURL, a, "")
	detail = documents.Detail{}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.Original != nil || get(emptyURL+"/files/original", a, "").Code != 404 {
		t.Fatal("fileless draft handling")
	}
}
