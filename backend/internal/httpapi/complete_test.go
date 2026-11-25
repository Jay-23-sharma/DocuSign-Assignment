package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCompletedPDFRecoveryAndIntegrity(t *testing.T) {
	d, links := guestFixture(t)
	_, other := d.sender()
	finish := d.url + "/complete"
	download := d.url + "/files/completed"
	d.call("POST", finish, "", d.cookie, 409)
	d.call("POST", finish, "", other, 404)
	d.call("GET", download, "", nil, 401)
	d.call("GET", download, "", other, 404)
	r, a := verified(d, links[0]), verified(d, links[1])
	ra, aa := r.answer("Ravi"), a.answer("Asha")
	r.check("submit", ra, 200)
	d.call("GET", download, "", d.cookie, 404)
	t.Run("renderer unavailable", func(t *testing.T) {
		t.Setenv("PDF_RENDER_PYTHON", filepath.Join(t.TempDir(), "missing"))
		a.check("submit", aa, 200)
		doc := d.detail()
		if doc.Status != "signed" || doc.CompletedAt != nil || doc.CompletedSHA256 != nil {
			t.Fatal("signatures or completion state lost")
		}
		d.call("POST", finish, "", d.cookie, 500)
	})
	// All decisions were accepted on time. Expiry must leave the signed request renderable.
	if _, e := d.pool.Exec(t.Context(), "UPDATE documents SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", d.id); e != nil {
		t.Fatal(e)
	}
	if e := d.store.ExpireOverdue(t.Context()); e != nil {
		t.Fatal(e)
	}
	if doc := d.detail(); doc.Status != "signed" {
		t.Fatal("sweep closed a fully signed request")
	}
	dir := filepath.Join(d.store.Dir, fmt.Sprint(d.id))
	originalPath := filepath.Join(dir, "original.pdf")
	original, e := os.ReadFile(originalPath)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(originalPath, []byte("modified"), 0600); e != nil {
		t.Fatal(e)
	}
	d.call("POST", finish, "", d.cookie, 500)
	if e = os.WriteFile(originalPath, original, 0600); e != nil {
		t.Fatal(e)
	}
	out := make(chan int, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			r := httptest.NewRequest("POST", finish, nil)
			r.AddCookie(d.cookie)
			w := httptest.NewRecorder()
			d.router.ServeHTTP(w, r)
			out <- w.Code
		}()
	}
	close(start)
	for range 2 {
		if code := <-out; code != 204 {
			t.Fatalf("retry returned %d", code)
		}
	}
	a.check("submit", aa, 403)
	doc := d.detail()
	if doc.Status != "completed" || doc.CompletedAt == nil || doc.CompletedSHA256 == nil {
		t.Fatal("missing completion metadata")
	}
	b := d.call("GET", download+"?download=1", "", d.cookie, 200)
	sum := sha256.Sum256(b)
	if !bytes.HasPrefix(b, []byte("%PDF-")) || hex.EncodeToString(sum[:]) != *doc.CompletedSHA256 || bytes.Equal(b, original) {
		t.Fatal("incorrect artifact")
	}
	var artifacts, events, pages int
	var size int64
	e = d.pool.QueryRow(t.Context(), `SELECT count(*),max(size_bytes),max(page_count),(SELECT count(*) FROM document_events WHERE document_id=$1 AND event_type='completed') FROM document_files WHERE document_id=$1 AND kind='completed'`, d.id).Scan(&artifacts, &size, &pages, &events)
	if e != nil || artifacts != 1 || events != 1 || size != int64(len(b)) || pages != 3 {
		t.Fatal("incorrect audit or file metadata", e)
	}
	req := httptest.NewRequest("GET", download, nil)
	req.AddCookie(d.cookie)
	req.Header.Set("Range", "bytes=0-3")
	w := httptest.NewRecorder()
	d.router.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent || w.Body.String() != "%PDF" {
		t.Fatal("completed range failed")
	}
	if e = os.WriteFile(filepath.Join(dir, "completed.pdf"), []byte("modified"), 0600); e != nil {
		t.Fatal(e)
	}
	d.call("GET", download, "", d.cookie, 500)
}
