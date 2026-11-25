package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"signing-app/backend/internal/documents"
	"strings"
	"testing"
)

func (f *docFixture) call(method, url, body string, c *http.Cookie, status int) []byte {
	f.t.Helper()
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != status {
		f.t.Fatalf("%s %s: want %d, got %d %s", method, url, status, w.Code, w.Body)
	}
	return w.Body.Bytes()
}
func (f *docFixture) draft(c *http.Cookie) int64 {
	w := f.upload(c, part{"file", "test.pdf", f.pdf})
	var d documents.Draft
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &d) != nil {
		f.t.Fatal("draft creation failed")
	}
	return d.ID
}
func (f *docFixture) recipient(url, name string, c *http.Cookie) documents.Recipient {
	var v documents.Recipient
	data := f.call("POST", url+"/recipients", fmt.Sprintf(`{"name":"%s","email":"%s@example.test"}`, name, name), c, 201)
	if err := json.Unmarshal(data, &v); err != nil {
		f.t.Fatal(err)
	}
	return v
}
func TestDraftRecipients(t *testing.T) {
	f := fixture(t)
	_, c := f.sender()
	id := f.draft(c)
	url := fmt.Sprintf("/api/documents/%d", id)
	ravi := f.recipient(url, "Ravi", c)
	asha := f.recipient(url, "Asha", c)
	route := fmt.Sprintf("%s/recipients/%d", url, ravi.ID)
	f.call("PATCH", route, `{"name":"Ravi Kumar"}`, c, 200)
	var n int
	err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM recipients WHERE document_id=$1 AND status='pending' AND ((id=$2 AND name='Ravi Kumar') OR (id=$3 AND name='Asha'))", id, ravi.ID, asha.ID).Scan(&n)
	if err != nil || n != 2 || ravi.ID == asha.ID {
		t.Fatal("recipient identity", err, n)
	}
}

func TestDraftLayout(t *testing.T) {
	f := fixture(t)
	_, owner := f.sender()
	_, other := f.sender()
	id := f.draft(owner)
	url := fmt.Sprintf("/api/documents/%d", id)
	otherURL := fmt.Sprintf("/api/documents/%d", f.draft(other))
	ravi := f.recipient(url, "Ravi", owner)
	asha := f.recipient(url, "Asha", owner)
	foreign := f.recipient(otherURL, "Other", other)
	field := func(rid int64) string {
		return fmt.Sprintf(`{"recipient_id":%d,"field_type":"signature","page_number":2,"required":false,"x":0.8,"y":0.9,"width":0.2,"height":0.1}`, rid)
	}
	put := func(body string, c *http.Cookie, status int) { f.call("PUT", url+"/fields", body, c, status) }
	layout := `{"fields":[` + field(ravi.ID) + "," + field(asha.ID) + "]}"
	put(layout, owner, 204)
	put(layout, owner, 204)
	get := func() documents.Detail {
		var d documents.Detail
		if err := json.Unmarshal(f.call("GET", url, "", owner, 200), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	d := get()
	if len(d.Fields) != 2 || d.Fields[0].RecipientID != ravi.ID || d.Fields[1].RecipientID != asha.ID || d.Fields[0].X != 0.8 || d.Fields[0].Required || d.Fields[0].Page != 2 {
		t.Fatal("saved layout mismatch", d.Fields)
	}
	for _, invalid := range []string{"{}", `{"fields":null}`, `{"fields":{}}`, strings.Replace(layout, "0.2", "0.21", 1), strings.Replace(layout, `"required":false,`, "", 1), strings.Replace(layout, `"page_number":2`, `"page_number":4`, 1), strings.Replace(layout, "signature", "image", 1)} {
		put(invalid, owner, 400)
	}
	put(`{"fields":[`+field(ravi.ID)+","+field(foreign.ID)+"]}", owner, 404)
	for _, method := range []string{"POST", "PATCH", "DELETE", "PUT"} {
		path := url + "/recipients"
		body := `{"name":"Test","email":"test@example.test"}`
		if method == "PATCH" || method == "DELETE" {
			path += fmt.Sprintf("/%d", ravi.ID)
		}
		if method == "PUT" {
			path = url + "/fields"
			body = layout
		}
		f.call(method, path, body, nil, 401)
		f.call(method, path, body, other, 404)
	}
	if d = get(); len(d.Fields) != 2 {
		t.Fatal("previous layout lost")
	}
	var answered int
	err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM document_fields WHERE document_id=$1 AND
 coalesce(text_value,checked_value::text,signature_method,signature_storage_key,submitted_at::text) IS NOT NULL`, id).Scan(&answered)
	if err != nil || answered != 0 {
		t.Fatal("field answers", err)
	}
	f.call("PATCH", fmt.Sprintf("%s/recipients/%d", url, ravi.ID), `{"name":"Ravi Kumar"}`, owner, 200)
	f.call("DELETE", fmt.Sprintf("%s/recipients/%d", url, foreign.ID), "", owner, 404)
	f.call("DELETE", fmt.Sprintf("%s/recipients/%d?savedFields=0", url, ravi.ID), "", owner, 409)
	if len(get().Fields) != 2 {
		t.Fatal("deletion count changed without confirmation")
	}
	f.call("DELETE", fmt.Sprintf("%s/recipients/%d?savedFields=1", url, ravi.ID), "", owner, 204)
	put(layout, owner, 404)
	if d = get(); len(d.Recipients) != 1 || len(d.Fields) != 1 || d.Fields[0].RecipientID != asha.ID {
		t.Fatal("recipient removal or stale save corrupted layout")
	}
	put(`{"fields":[]}`, owner, 204)
	if d = get(); len(d.Fields) != 0 {
		t.Fatal("empty layout not cleared")
	}
	if _, err = f.pool.Exec(t.Context(), "UPDATE documents SET status='sent' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	put(`{"fields":[]}`, owner, 409)
	f.call("POST", url+"/recipients", `{"name":"Test","email":"test@example.test"}`, owner, 409)
	for _, method := range []string{"PATCH", "DELETE"} {
		f.call(method, fmt.Sprintf("%s/recipients/%d", url, asha.ID), `{"name":"Test"}`, owner, 409)
	}
}
