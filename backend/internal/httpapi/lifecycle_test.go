package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVoidCompetesWithLastSignature(t *testing.T) {
	d, l := guestFixture(t)
	r, a := verified(d, l[0]), verified(d, l[1])
	answer, _ := json.Marshal(a.answer("Asha"))
	r.check("submit", r.answer("Ravi"), 200)
	_, other := d.sender()
	d.call("GET", d.url+"/history", "", other, 404)
	d.call("POST", d.url+"/void", "", other, 404)
	d.call("POST", d.url+"/void", "", nil, 401)
	gate := make(chan struct{})
	v, s := make(chan int, 1), make(chan int, 1)
	go func() {
		<-gate
		req := httptest.NewRequest("POST", d.url+"/void", nil)
		req.AddCookie(d.cookie)
		w := httptest.NewRecorder()
		d.router.ServeHTTP(w, req)
		v <- w.Code
	}()
	go func() { <-gate; s <- a.request("POST", "submit", answer).Code }()
	close(gate)
	vc, sc := <-v, <-s
	var status string
	var events, signed, answers, files int
	e := d.pool.QueryRow(t.Context(), `SELECT status,(SELECT count(*) FROM document_events WHERE document_id=$1 AND event_type='voided' AND actor_user_id=$2),(SELECT count(*) FROM document_events WHERE document_id=$1 AND event_type='signed'),(SELECT count(submitted_at) FROM document_fields WHERE document_id=$1),(SELECT count(*) FROM document_files WHERE document_id=$1 AND kind='completed') FROM documents WHERE id=$1`, d.id, d.owner).Scan(&status, &events, &signed, &answers, &files)
	if e != nil {
		t.Fatal(e)
	}
	switch status {
	case "voided":
		if vc != 204 || sc != 409 || events != 1 || signed != 1 || answers != 1 || files != 0 {
			t.Fatal("void race inconsistent")
		}
		d.call("POST", d.url+"/void", "", d.cookie, 204)
	case "completed":
		if vc != 409 || sc != 200 || events != 0 || signed != 2 || answers != 2 || files != 1 {
			t.Fatal("sign race inconsistent")
		}
	default:
		t.Fatal("unexpected outcome", status)
	}
	first := d.call("GET", d.url+"/history", "", d.cookie, 200)
	second := d.call("GET", d.url+"/history", "", d.cookie, 200)
	if string(first) != string(second) || strings.Count(string(first), `"type":"voided"`) != events {
		t.Fatal("unstable history")
	}
}
func TestBlockedDecisionCommitsExpiry(t *testing.T) {
	d, l := guestFixture(t)
	g := verified(d, l[0])
	answer, _ := json.Marshal(g.answer("Ravi"))
	ctx := t.Context()
	tx, e := d.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var blocker int
	if e = tx.QueryRow(ctx, "SELECT pg_backend_pid() FROM documents WHERE id=$1 FOR UPDATE", d.id).Scan(&blocker); e != nil {
		t.Fatal(e)
	}
	result := make(chan int, 1)
	go func() { result <- g.request("POST", "submit", answer).Code }()
	// Advance the fixture deadline to just after the waiting decision transaction started.
	// PostgreSQL's transaction timestamp would still classify that decision as on time.
	var waitingSince time.Time
	end := time.Now().Add(5 * time.Second)
	for {
		if e = d.pool.QueryRow(ctx, `SELECT xact_start FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND datname=current_database() LIMIT 1`, blocker).Scan(&waitingSince); e == nil {
			break
		}
		if time.Now().After(end) {
			t.Fatal("decision did not wait on lock", e)
		}
		time.Sleep(time.Millisecond * 5)
	}
	if _, e = tx.Exec(ctx, "UPDATE documents SET expires_at=$2::timestamptz+interval '1 microsecond' WHERE id=$1", d.id, waitingSince); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if <-result != 403 {
		t.Fatal("late decision accepted")
	}
	var status string
	var events, answers int
	e = d.pool.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM document_events WHERE document_id=$1 AND event_type='expired'),(SELECT count(submitted_at) FROM document_fields WHERE document_id=$1) FROM documents WHERE id=$1`, d.id).Scan(&status, &events, &answers)
	if e != nil || status != "expired" || events != 1 || answers != 0 {
		t.Fatal("expired decision not committed", status, events, answers, e)
	}
}
func TestConcurrentExpiryAndPastDraft(t *testing.T) {
	d, _ := guestFixture(t)
	draft := d.draft(d.cookie)
	if _, e := d.pool.Exec(t.Context(), "UPDATE documents SET expires_at=clock_timestamp()-interval '1 day' WHERE id IN ($1,$2)", d.id, draft); e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 4)
	for range 4 {
		go func() { results <- d.store.ExpireOverdue(t.Context()) }()
	}
	for range 4 {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
	var expired, draftStatus string
	var events int
	e := d.pool.QueryRow(t.Context(), `SELECT (SELECT status FROM documents WHERE id=$1),(SELECT status FROM documents WHERE id=$2),(SELECT count(*) FROM document_events WHERE document_id IN ($1,$2) AND event_type='expired')`, d.id, draft).Scan(&expired, &draftStatus, &events)
	if e != nil || expired != "expired" || draftStatus != "draft" || events != 1 {
		t.Fatal("sweep affected incorrect rows", e)
	}
}

func TestVoidAfterDeadlinePreservesPriorSignature(t *testing.T) {
	d, l := guestFixture(t)
	r, a := verified(d, l[0]), verified(d, l[1])
	ra := r.answer("Ravi")
	aa := a.answer("Asha")
	r.check("submit", ra, 200)
	if _, err := d.pool.Exec(t.Context(), "UPDATE documents SET expires_at=clock_timestamp() WHERE id=$1", d.id); err != nil {
		t.Fatal(err)
	}
	d.call("POST", d.url+"/void", "", d.cookie, 409)
	a.check("submit", aa, 403)
	var status string
	var accepted, expired, voids int
	e := d.pool.QueryRow(t.Context(), `SELECT status,(SELECT count(*) FROM document_fields WHERE document_id=$1 AND text_value='Ravi' AND submitted_at IS NOT NULL),
 (SELECT count(*) FROM document_events WHERE document_id=$1 AND event_type='expired'),(SELECT count(*) FROM document_events WHERE document_id=$1 AND event_type='voided') FROM documents WHERE id=$1`, d.id).Scan(&status, &accepted, &expired, &voids)
	if e != nil || status != "expired" || accepted != 1 || expired != 1 || voids != 0 {
		t.Fatal("overdue void changed accepted history", e)
	}
}
func TestClosureReasonRequiresCurrentVerifiedSession(t *testing.T) {
	d, l := guestFixture(t)
	valid := verified(d, l[0])
	unverified := exchange(d, l[0])
	revoked := verified(d, l[1])
	d.call("POST", d.resend(d.asha.ID), "", d.cookie, 204)
	d.call("POST", d.url+"/void", "", d.cookie, 204)
	for _, g := range []guestClient{revoked, unverified, {d: d}} {
		w := g.request("GET", "session", nil)
		if w.Code == 200 || strings.Contains(w.Body.String(), "voided") {
			t.Fatal("closure reason disclosed")
		}
	}
	w := valid.request("GET", "session", nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "voided") {
		t.Fatal("missing verified closure reason")
	}
}
