package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"signing-app/backend/internal/documents"
	"signing-app/backend/internal/signing"
	"testing"
)

type guestClient struct {
	d      *releaseDraft
	cookie *http.Cookie
	id     int64
}

func (g guestClient) request(method, path string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/signing/"+path, bytes.NewReader(body))
	if g.cookie != nil {
		r.AddCookie(g.cookie)
	}
	r.Header.Set("X-Signing-Session", fmt.Sprint(g.id))
	w := httptest.NewRecorder()
	g.d.router.ServeHTTP(w, r)
	return w
}
func (g guestClient) check(path string, input any, status int) []byte {
	g.d.t.Helper()
	b, _ := json.Marshal(input)
	w := g.request("POST", path, b)
	if w.Code != status {
		g.d.t.Fatalf("%s: got %d want %d", path, w.Code, status)
	}
	return w.Body.Bytes()
}
func exchange(d *releaseDraft, link documents.Invitation) guestClient {
	g := guestClient{d: d}
	b, _ := json.Marshal(map[string]string{"token": link.Token})
	w := g.request("POST", "sessions", b)
	if w.Code != 200 {
		d.t.Fatalf("exchange: %d", w.Code)
	}
	var session struct {
		ID int64 `json:"session_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &session)
	g.id = session.ID
	g.cookie = w.Result().Cookies()[0]
	return g
}
func guestFixture(t *testing.T) (*releaseDraft, []documents.Invitation) {
	d := releaseFixture(t)
	d.layout(d.ravi.ID, d.asha.ID)
	links, e := d.store.Release(t.Context(), d.owner, d.id)
	if e != nil {
		t.Fatal(e)
	}
	return d, links
}
func TestGuestExchange(t *testing.T) {
	d, links := guestFixture(t)
	a, b := exchange(d, links[0]), exchange(d, links[1])
	if a.id == b.id || a.cookie.Value == b.cookie.Value || !a.cookie.HttpOnly || a.cookie.Name != guestCookie {
		t.Fatal("invalid guest cookies")
	}
	var recipients, views int
	e := d.pool.QueryRow(t.Context(), `SELECT count(distinct r.id),count(r.first_viewed_at) FROM signing_sessions s JOIN signing_links l ON s.signing_link_id=l.id JOIN recipients r ON r.id=l.recipient_id WHERE r.document_id=$1`, d.id).Scan(&recipients, &views)
	if e != nil || recipients != 2 || views != 0 {
		t.Fatal("incorrect guest scope or premature view", e)
	}
}

func TestGuestCodeAndView(t *testing.T) {
	d, l := guestFixture(t)
	r, a := exchange(d, l[0]), exchange(d, l[1])
	stale := r
	stale.cookie = a.cookie
	stale.check("session/email-code", nil, 403)
	for _, path := range []string{"document", "files/original"} {
		if r.request("GET", path, nil).Code != 403 {
			t.Fatal("premature access")
		}
	}
	r.check("session/email-code", nil, 204)
	exchange(d, l[0]).check("session/email-code", nil, 429)
	r.check("session/verify-code", map[string]string{"code": "invalid"}, 422)
	var tries int
	if e := d.pool.QueryRow(t.Context(), "SELECT attempt_count FROM signing_email_codes WHERE session_id=$1", r.id).Scan(&tries); e != nil || tries != 1 {
		t.Fatal("guess did not persist", e)
	}
	r.check("session/verify-code", map[string]string{"code": d.mail.codes[d.ravi.Email]}, 204)
	var doc struct{ Fields []documents.Field }
	if e := json.Unmarshal(r.request("GET", "document", nil).Body.Bytes(), &doc); e != nil || len(doc.Fields) != 1 || doc.Fields[0].RecipientID != d.ravi.ID {
		t.Fatal("incorrect fields", e)
	}
	for range 2 {
		req := httptest.NewRequest("GET", "/api/signing/files/original", nil)
		req.AddCookie(r.cookie)
		req.Header.Set("X-Signing-Session", fmt.Sprint(r.id))
		req.Header.Set("Range", "bytes=0-3")
		w := httptest.NewRecorder()
		d.router.ServeHTTP(w, req)
		if w.Code != 206 || w.Body.String() != "%PDF" {
			t.Fatal("range request failed")
		}
	}
	var views int
	if e := d.pool.QueryRow(t.Context(), "SELECT count(*) FROM document_events WHERE document_id=$1 AND event_type='viewed'", d.id).Scan(&views); e != nil || views != 1 {
		t.Fatal("incorrect view count", e)
	}
	d.call("POST", d.resend(d.ravi.ID), "", d.cookie, 204)
	if r.request("GET", "document", nil).Code != 403 {
		t.Fatal("revoked guest could access document")
	}
}

func verified(d *releaseDraft, l documents.Invitation) guestClient {
	g := exchange(d, l)
	g.check("session/email-code", nil, 204)
	g.check("session/verify-code", map[string]string{"code": d.mail.codes[l.Email]}, 204)
	return g
}
func (g guestClient) answer(name string) map[string]any {
	var doc struct{ Fields []struct{ ID int64 } }
	json.Unmarshal(g.request("GET", "document", nil).Body.Bytes(), &doc)
	return map[string]any{"answers": []any{map[string]any{"id": doc.Fields[0].ID, "text": name, "method": "typed"}}}
}
func TestGuestTypedSigning(t *testing.T) {
	d, l := guestFixture(t)
	r, a := verified(d, l[0]), verified(d, l[1])
	ra, aa := r.answer("Ravi"), a.answer("Asha")
	r.check("submit", aa, 422)
	r.check("submit", ra, 200)
	r.check("decline", map[string]bool{"confirm": true}, 409)
	if status := d.detail().Status; status == "signed" || status == "completed" {
		t.Fatal("early completion")
	}
	a.check("submit", aa, 200)
	a.check("submit", ra, 200)
	var n int
	if e := d.pool.QueryRow(t.Context(), `SELECT count(*) FROM document_fields WHERE document_id=$1 AND signature_method='typed' AND text_value IN ('Ravi','Asha') AND submitted_at IS NOT NULL`, d.id).Scan(&n); e != nil || n != 2 || d.detail().Status != "completed" {
		t.Fatal("incorrect stored signatures", e)
	}
}

func TestGuestCodeLimits(t *testing.T) {
	d, l := guestFixture(t)
	r := exchange(d, l[0])
	// The issuance lock spans all sessions opened from one invitation.
	other := exchange(d, l[0])
	out := make(chan int, 2)
	for _, g := range []guestClient{r, other} {
		go func() { out <- g.request("POST", "session/email-code", nil).Code }()
	}
	a, b := <-out, <-out
	if a+b != 204+429 || a == b {
		t.Fatalf("concurrent issue %d %d", a, b)
	}
	age := func() {
		_, e := d.pool.Exec(t.Context(), `UPDATE signing_email_codes SET expires_at=expires_at-interval '65 seconds' WHERE session_id IN (SELECT id FROM signing_sessions WHERE signing_link_id=$1)`, l[0].LinkID)
		if e != nil {
			t.Fatal(e)
		}
	}
	for range 3 {
		age()
		g := exchange(d, l[0])
		g.check("session/email-code", nil, 204)
		for range 5 {
			g.check("session/verify-code", map[string]string{"code": "wrong"}, 422)
		}
		status := g.request("POST", "session/verify-code", []byte(`{"code":"000000"}`)).Code
		if status != 422 && status != 429 {
			t.Fatal("exhausted code accepted")
		}
	}
	age()
	exchange(d, l[0]).check("session/email-code", nil, 429)
	aGuest := exchange(d, l[1])
	aGuest.check("session/email-code", nil, 204)
	_, e := d.pool.Exec(t.Context(), "UPDATE signing_email_codes SET expires_at=now()-interval '1 second' WHERE session_id=$1", aGuest.id)
	if e != nil {
		t.Fatal(e)
	}
	aGuest.check("session/verify-code", map[string]string{"code": d.mail.codes[l[1].Email]}, 422)
	aGuest.check("session/email-code", nil, 204)
	for range 3 {
		_, e = d.pool.Exec(t.Context(), "UPDATE signing_email_codes SET expires_at=expires_at-interval '61 seconds' WHERE session_id=$1", aGuest.id)
		if e != nil {
			t.Fatal(e)
		}
		aGuest.check("session/email-code", nil, 204)
	}
	_, e = d.pool.Exec(t.Context(), "UPDATE signing_email_codes SET expires_at=expires_at-interval '61 seconds' WHERE session_id=$1", aGuest.id)
	if e != nil {
		t.Fatal(e)
	}
	exchange(d, l[1]).check("session/email-code", nil, 429)
}
func (g guestClient) assertStatus(method, path string, status int) {
	g.d.t.Helper()
	if w := g.request(method, path, nil); w.Code != status {
		g.d.t.Fatalf("%s got %d want %d", path, w.Code, status)
	}
}
func TestGuestExpiry(t *testing.T) {
	d, l := guestFixture(t)
	r := verified(d, l[0])
	a := verified(d, l[1])
	expire := func(q string, id int64) {
		if _, e := d.pool.Exec(t.Context(), q, id); e != nil {
			t.Fatal(e)
		}
	}
	expire("UPDATE signing_sessions SET expires_at=now() WHERE id=$1", r.id)
	r.assertStatus("GET", "document", 403)
	expire("UPDATE signing_links SET expires_at=now() WHERE id=$1", l[0].LinkID)
	r.check("sessions", map[string]string{"token": l[0].Token}, 403)
	expire("UPDATE documents SET expires_at=now() WHERE id=$1", d.id)
	a.check("submit", map[string]any{"answers": []any{}}, 403)
	a.assertStatus("GET", "files/original", 403)
}
func TestGuestDecisionRace(t *testing.T) {
	for _, scenario := range []string{"both sign", "same recipient signs and declines", "decline after another signs"} {
		t.Run(scenario, func(t *testing.T) {
			d, l := guestFixture(t)
			r, a := verified(d, l[0]), verified(d, l[1])
			ra, aa := r.answer("Ravi"), a.answer("Asha")
			state := func() (status string, signed, declined, events, answers int, unfinished bool) {
				e := d.pool.QueryRow(t.Context(), `SELECT status,completed_at IS NULL,(SELECT count(signed_at) FROM recipients WHERE document_id=$1),(SELECT count(declined_at) FROM recipients WHERE document_id=$1),(SELECT count(*) FROM document_events WHERE document_id=$1 AND event_type IN('signed','declined')),(SELECT count(submitted_at) FROM document_fields WHERE document_id=$1) FROM documents WHERE id=$1`, d.id).Scan(&status, &unfinished, &signed, &declined, &events, &answers)
				if e != nil {
					t.Fatal(e)
				}
				return
			}
			if scenario == "decline after another signs" {
				r.check("submit", ra, 200)
				a.check("decline", map[string]bool{"confirm": true}, 200)
				a.check("submit", aa, 409)
				status, s, n, ev, ans, u := state()
				if status != "declined" || s != 1 || n != 1 || ev != 2 || ans != 1 || !u {
					t.Fatal("decline lost prior signature")
				}
				return
			}
			var op string
			var second any
			client := a
			if scenario == "both sign" {
				op = "submit"
				second = aa
			} else {
				op = "decline"
				second = map[string]bool{"confirm": true}
				client = r
			}
			start := make(chan struct{})
			out := make(chan int, 2)
			for _, f := range []func() int{func() int { body, _ := json.Marshal(ra); return r.request("POST", "submit", body).Code }, func() int { body, _ := json.Marshal(second); return client.request("POST", op, body).Code }} {
				go func() { <-start; out <- f() }()
			}
			close(start)
			x, y := <-out, <-out
			status, s, n, ev, ans, u := state()
			if scenario == "both sign" {
				if x != 200 || y != 200 || s != 2 || ev != 2 || ans != 2 || n != 0 || u || status != "completed" {
					t.Fatal("parallel signing inconsistent")
				}
				a.check("submit", aa, 200)
				r.check("submit", ra, 200)
				_, _, _, ev, _, _ = state()
				if ev != 2 {
					t.Fatal("duplicate replay event")
				}
			} else if x+y != 609 || s+n != 1 || ev != 1 || ans != s || !u || (n == 1 && status != "declined") {
				t.Fatal("decision race inconsistent")
			}
		})
	}
}

func TestGuestFieldAndImageValidation(t *testing.T) {
	d := releaseFixture(t)
	fields := []documents.Field{}
	for _, typ := range []string{"signature", "checkbox", "text", "email", "date", "name", "initials"} {
		fields = append(fields, documents.Field{RecipientID: d.ravi.ID, Type: typ, Page: 1, Width: .1, Height: .1, Required: true})
	}
	fields = append(fields, documents.Field{RecipientID: d.asha.ID, Type: "signature", Page: 1, Width: .1, Height: .1, Required: true})
	layout, _ := json.Marshal(map[string]any{"fields": fields})
	d.call("PUT", d.url+"/fields", string(layout), d.cookie, 204)
	links, e := d.store.Release(t.Context(), d.owner, d.id)
	if e != nil {
		t.Fatal(e)
	}
	ravi, asha := verified(d, links[0]), verified(d, links[1])
	var doc signing.Document
	if e = json.Unmarshal(ravi.request("GET", "document", nil).Body.Bytes(), &doc); e != nil {
		t.Fatal(e)
	}
	ids := map[string]int64{}
	for _, f := range doc.Fields {
		ids[f.Type] = f.ID
	}
	var pngFile bytes.Buffer
	if e = png.Encode(&pngFile, image.NewRGBA(image.Rect(0, 0, 32, 32))); e != nil {
		t.Fatal(e)
	}
	path := "signatures?method=uploaded"
	for _, tc := range []struct {
		data   []byte
		status int
	}{{[]byte("fake.png"), 422}, {pngFile.Bytes()[:40], 422}, {make([]byte, signing.MaxImageBytes+1), 413}} {
		if w := ravi.request("POST", path, tc.data); w.Code != tc.status {
			t.Fatalf("image got %d want %d", w.Code, tc.status)
		}
	}
	var wide bytes.Buffer
	png.Encode(&wide, image.NewRGBA(image.Rect(0, 0, 4097, 1)))
	if ravi.request("POST", path, wide.Bytes()).Code != 422 {
		t.Fatal("oversized dimensions accepted")
	}
	upload := func(g guestClient) string {
		w := g.request("POST", path, pngFile.Bytes())
		var body struct{ Reference string }
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Reference == "" {
			t.Fatal("image upload failed")
		}
		return body.Reference
	}
	foreign, own := upload(asha), upload(ravi)
	sig := map[string]any{"id": ids["signature"], "method": "uploaded", "reference": own}
	checkbox := map[string]any{"id": ids["checkbox"], "checked": true}
	date := map[string]any{"id": ids["date"], "text": "2030-07-15"}
	address := map[string]any{"id": ids["email"], "text": "ravi@example.test"}
	answers := []any{sig, checkbox, date, address}
	for _, k := range []string{"text", "name", "initials"} {
		answers = append(answers, map[string]any{"id": ids[k], "text": "R"})
	}
	submit := func(a []any, status int) { ravi.check("submit", map[string]any{"answers": a}, status) }
	submit(answers[:5], 422)
	submit(append(answers, sig), 422)
	checkbox["checked"] = false
	submit(answers, 422)
	checkbox["checked"] = true
	date["text"] = "2030-02-30"
	submit(answers, 422)
	date["text"] = "2030-02-12"
	address["text"] = "bad"
	submit(answers, 422)
	address["text"] = "r@example.test"
	for _, ref := range []string{foreign, "../../original.pdf"} {
		sig["reference"] = ref
		submit(answers, 422)
	}
	sig["reference"] = own
	var n int
	if e = d.pool.QueryRow(t.Context(), "SELECT count(submitted_at) FROM document_fields WHERE document_id=$1", d.id).Scan(&n); e != nil || n != 0 {
		t.Fatal("partial answers", e)
	}
	submit(answers, 200)
	var key string
	if e = d.pool.QueryRow(t.Context(), "SELECT signature_storage_key FROM document_fields WHERE id=$1 AND signature_method='uploaded' AND text_value IS NULL", ids["signature"]).Scan(&key); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenInRoot(d.store.Dir, key)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if _, e = png.Decode(f); e != nil {
		t.Fatal("stored image unreadable")
	}
}

func TestOutstandingRecipientsRaceSignAndDecline(t *testing.T) {
	d, l := guestFixture(t)
	r, a := verified(d, l[0]), verified(d, l[1])
	data, _ := json.Marshal(r.answer("Ravi"))
	gate := make(chan struct{})
	signResult := make(chan int, 1)
	declineResult := make(chan int, 1)
	go func() { <-gate; signResult <- r.request("POST", "submit", data).Code }()
	go func() { <-gate; declineResult <- a.request("POST", "decline", []byte(`{"confirm":true}`)).Code }()
	close(gate)
	sign, decline := <-signResult, <-declineResult
	if decline != 200 || (sign != 200 && sign != 409) {
		t.Fatalf("sign %d, decline %d", sign, decline)
	}
	expected := 0
	if sign == 200 {
		expected = 1
	}
	var signedEvents, declineEvents, answers int
	e := d.pool.QueryRow(t.Context(), `SELECT count(*) FILTER(WHERE event_type='signed'),count(*) FILTER(WHERE event_type='declined'),(SELECT count(submitted_at) FROM document_fields WHERE document_id=$1) FROM document_events WHERE document_id=$1`, d.id).Scan(&signedEvents, &declineEvents, &answers)
	if e != nil || signedEvents != expected || answers != expected || declineEvents != 1 || d.detail().Status != "declined" {
		t.Fatal("sign/decline did not serialize", e)
	}
}
func TestGuestConcurrentAttemptsAndReplacement(t *testing.T) {
	d, l := guestFixture(t)
	r := exchange(d, l[0])
	r.check("session/email-code", nil, 204)
	out := make(chan int, 8)
	for range 8 {
		go func() { out <- r.request("POST", "session/verify-code", []byte(`{"code":"invalid"}`)).Code }()
	}
	for range 8 {
		if <-out != 422 {
			t.Fatal("invalid attempt accepted")
		}
	}
	var tries int
	if e := d.pool.QueryRow(t.Context(), "SELECT attempt_count FROM signing_email_codes WHERE session_id=$1", r.id).Scan(&tries); e != nil || tries != 5 {
		t.Fatal("concurrent attempts evaded limit", e)
	}
	if _, e := d.pool.Exec(t.Context(), "UPDATE signing_email_codes SET expires_at=expires_at-interval '61 seconds' WHERE session_id=$1", r.id); e != nil {
		t.Fatal(e)
	}
	fresh := exchange(d, l[0])
	fresh.check("session/email-code", nil, 204)
	var active int
	if e := d.pool.QueryRow(t.Context(), "SELECT count(*) FROM signing_email_codes WHERE session_id=$1 AND used_at IS NULL", r.id).Scan(&active); e != nil || active != 0 {
		t.Fatal("replacement left old code active", e)
	}
	fresh.check("session/verify-code", map[string]string{"code": d.mail.codes[l[0].Email]}, 204)
}
