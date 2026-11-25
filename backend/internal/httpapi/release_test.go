package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"signing-app/backend/internal/documents"
)

type testMailer struct {
	sync.Mutex
	attempts map[string]int
	codes    map[string]string
	fail     string
}

func (m *testMailer) Send(string, string) error { return errors.New("unexpected verification email") }
func (m *testMailer) SendInvitation(email, subject, message string, deadline time.Time, token string) error {
	m.Lock()
	defer m.Unlock()
	m.attempts[email]++
	if email == m.fail {
		return errors.New("simulated SMTP rejection")
	}
	return nil
}

type releaseDraft struct {
	*docFixture
	id, owner  int64
	url        string
	cookie     *http.Cookie
	ravi, asha documents.Recipient
	mail       *testMailer
}

func releaseFixture(t *testing.T) *releaseDraft {
	f := fixture(t)
	owner, c := f.sender()
	id := f.draft(c)
	url := fmt.Sprintf("/api/documents/%d", id)
	sender := &testMailer{attempts: map[string]int{}}
	f.router = Handler(f.pool, sender, f.cookies, f.store)
	ravi := f.recipient(url, "Ravi", c)
	asha := f.recipient(url, "Asha", c)
	f.call("PATCH", url, `{"email_subject":"Sign this PDF","email_message":"Review both pages.","expires_at":"2090-06-03T20:15:00+05:30"}`, c, 204)
	return &releaseDraft{f, id, owner, url, c, ravi, asha, sender}
}
func (d *releaseDraft) layout(ids ...int64) {
	fields := make([]documents.Field, 0)
	for _, id := range ids {
		fields = append(fields, documents.Field{RecipientID: id, Type: "signature", Page: 1, Width: .3, Height: .1, Required: true})
	}
	data, _ := json.Marshal(map[string]any{"fields": fields})
	d.call("PUT", d.url+"/fields", string(data), d.cookie, 204)
}
func (d *releaseDraft) detail() documents.Detail {
	var doc documents.Detail
	if err := json.Unmarshal(d.call("GET", d.url, "", d.cookie, 200), &doc); err != nil {
		d.t.Fatal(err)
	}
	return doc
}
func (d *releaseDraft) counts(links, events int) {
	d.t.Helper()
	var l, e int
	err := d.pool.QueryRow(d.t.Context(), `SELECT (SELECT count(*) FROM signing_links l JOIN recipients r ON r.id=l.recipient_id WHERE r.document_id=$1),
 (SELECT count(*) FROM document_events WHERE document_id=$1 AND event_type='sent' AND actor_user_id=$2 AND recipient_id IS NULL)`, d.id, d.owner).Scan(&l, &e)
	if err != nil || l != links || e != events {
		d.t.Fatalf("links %d, sent events %d, err %v", l, e, err)
	}
}
func (d *releaseDraft) resend(rid int64) string {
	return fmt.Sprintf("%s/recipients/%d/resend", d.url, rid)
}

func TestReleaseValidationAndFreeze(t *testing.T) {
	d := releaseFixture(t)
	_, other := d.sender()
	d.call("PATCH", d.url, `{"email_subject":"New subject"}`, d.cookie, 204)
	for _, bad := range []string{`{"email_subject":"bad\r\nheader"}`, `{"expires_at":"2030-01-01T00:00"}`} {
		d.call("PATCH", d.url, bad, d.cookie, 400)
	}
	doc := d.detail()
	if *doc.Subject != "New subject" || *doc.Message != "Review both pages." || doc.Deadline.UTC().Format(time.RFC3339) != "2090-06-03T14:45:00Z" {
		t.Fatal("settings patch lost data")
	}
	d.layout(d.ravi.ID)
	result := d.call("POST", d.url+"/send", "", d.cookie, 422)
	if !strings.Contains(string(result), "Asha") {
		t.Fatal("missing signer not identified")
	}
	d.layout(d.ravi.ID, d.asha.ID)
	d.call("PATCH", d.url, `{"expires_at":"2000-01-01T00:00:00Z"}`, d.cookie, 204)
	d.call("POST", d.url+"/send", "", d.cookie, 422)
	for _, c := range []*http.Cookie{nil, other} {
		status := 401
		if c != nil {
			status = 404
		}
		d.call("POST", d.url+"/send", "", c, status)
		d.call("POST", d.resend(d.asha.ID), "", c, status)
		d.call("PATCH", d.url, `{"email_subject":"x"}`, c, status)
	}
	d.counts(0, 0)
	if d.detail().Status != "draft" || len(d.mail.attempts) != 0 {
		t.Fatal("invalid release changed state")
	}
	d.call("PATCH", d.url, `{"expires_at":"2090-06-03T14:45:00Z"}`, d.cookie, 204)
	gate := make(chan struct{})
	results := make(chan int, 2)
	for range 2 {
		go func() {
			<-gate
			r := httptest.NewRequest("POST", d.url+"/send", nil)
			r.AddCookie(d.cookie)
			w := httptest.NewRecorder()
			d.router.ServeHTTP(w, r)
			results <- w.Code
		}()
	}
	close(gate)
	a, b := <-results, <-results
	if !((a == 204 && b == 409) || (a == 409 && b == 204)) {
		t.Fatalf("concurrent responses %d %d", a, b)
	}
	d.counts(2, 1)
	doc = d.detail()
	if doc.Status != "sent" || doc.SentAt == nil || d.mail.attempts[d.ravi.Email] != 1 || d.mail.attempts[d.asha.Email] != 1 {
		t.Fatal("duplicate delivery or incorrect sent state")
	}
	d.call("PATCH", d.url, `{"email_message":"changed"}`, d.cookie, 409)
	d.call("PATCH", fmt.Sprintf("%s/recipients/%d", d.url, d.ravi.ID), `{"name":"Changed"}`, d.cookie, 409)
	d.call("PUT", d.url+"/fields", `{"fields":[]}`, d.cookie, 409)
}

func TestPartialDeliveryAndResend(t *testing.T) {
	d := releaseFixture(t)
	d.layout(d.ravi.ID, d.asha.ID)
	d.mail.fail = d.asha.Email
	d.call("POST", d.url+"/send", "", d.cookie, 204)
	outcomes := func(a, b string) {
		t.Helper()
		doc := d.detail()
		if doc.Status != "sent" || doc.Recipients[0].Delivery != a || doc.Recipients[1].Delivery != b {
			t.Fatal("unexpected delivery results")
		}
	}
	outcomes("accepted", "failed")
	active := func(rid int64) int64 {
		var id int64
		if err := d.pool.QueryRow(t.Context(), "SELECT id FROM signing_links WHERE recipient_id=$1 AND revoked_at IS NULL", rid).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ravi, asha := active(d.ravi.ID), active(d.asha.ID)
	d.call("POST", d.resend(d.asha.ID), "", d.cookie, 204)
	outcomes("accepted", "failed")
	d.mail.fail = ""
	d.call("POST", d.resend(d.asha.ID), "", d.cookie, 204)
	outcomes("accepted", "accepted")
	if active(d.ravi.ID) != ravi || active(d.asha.ID) == asha || d.mail.attempts[d.ravi.Email] != 1 || d.mail.attempts[d.asha.Email] != 3 {
		t.Fatal("resend affected the wrong recipient")
	}
	var revoked int
	if err := d.pool.QueryRow(t.Context(), "SELECT count(*) FROM signing_links WHERE recipient_id=$1 AND revoked_at IS NOT NULL", d.asha.ID).Scan(&revoked); err != nil || revoked != 2 {
		t.Fatal("superseded links")
	}
	_, other := d.sender()
	otherDoc := fmt.Sprintf("/api/documents/%d", d.draft(other))
	foreign := d.recipient(otherDoc, "Other", other)
	d.call("POST", d.resend(foreign.ID), "", d.cookie, 404)
	for _, status := range []string{"signed", "declined"} {
		if _, err := d.pool.Exec(t.Context(), "UPDATE recipients SET status=$1 WHERE id=$2", status, d.asha.ID); err != nil {
			t.Fatal(err)
		}
		d.call("POST", d.resend(d.asha.ID), "", d.cookie, 409)
	}
	if _, err := d.pool.Exec(t.Context(), "UPDATE documents SET expires_at=now()-interval '1 day' WHERE id=$1", d.id); err != nil {
		t.Fatal(err)
	}
	d.call("POST", d.resend(d.ravi.ID), "", d.cookie, 409)
	if _, err := d.pool.Exec(t.Context(), "UPDATE documents SET status='voided',expires_at=now()+interval '1 day' WHERE id=$1", d.id); err != nil {
		t.Fatal(err)
	}
	d.call("POST", d.resend(d.ravi.ID), "", d.cookie, 409)
	d.counts(4, 1)
}

func TestUnconfirmedRecoveryAndLateOutcomes(t *testing.T) {
	d := releaseFixture(t)
	d.layout(d.ravi.ID, d.asha.ID)
	batch, err := d.store.Release(t.Context(), d.owner, d.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 || batch[0].Token == batch[1].Token {
		t.Fatal("personal link generation")
	}
	for _, invite := range batch {
		digest := sha256.Sum256([]byte(invite.Token))
		var matches bool
		err = d.pool.QueryRow(t.Context(), `SELECT l.token_hash=$1 AND l.expires_at=d.expires_at FROM signing_links l JOIN recipients r ON r.id=l.recipient_id
  JOIN documents d ON d.id=r.document_id WHERE l.id=$2 AND r.id=$3`, digest[:], invite.LinkID, invite.RecipientID).Scan(&matches)
		if err != nil || !matches {
			t.Fatal("digest or deadline mismatch")
		}
	}
	if d.detail().Recipients[0].Delivery != "unconfirmed" {
		t.Fatal("missing outcome should be unconfirmed")
	}
	if err = d.store.RecordDelivery(t.Context(), batch[0], true); err != nil {
		t.Fatal(err)
	}
	replacement, err := d.store.ReplaceInvitation(t.Context(), d.owner, d.id, d.ravi.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.store.RecordDelivery(t.Context(), batch[0], true); err != nil {
		t.Fatal(err)
	}
	if d.detail().Recipients[0].Delivery != "unconfirmed" {
		t.Fatal("late acceptance leaked onto replacement")
	}
	if err = d.store.RecordDelivery(t.Context(), replacement, true); err != nil {
		t.Fatal(err)
	}
	if err = d.store.RecordDelivery(t.Context(), batch[0], false); err != nil {
		t.Fatal(err)
	}
	d.call("POST", d.resend(d.asha.ID), "", d.cookie, 204)
	doc := d.detail()
	if doc.Recipients[0].Delivery != "accepted" || doc.Recipients[1].Delivery != "accepted" {
		t.Fatal("unconfirmed recovery failed")
	}
}

func TestSendInsertFailureRollsBack(t *testing.T) {
	d := releaseFixture(t)
	d.layout(d.ravi.ID, d.asha.ID)
	cfg := d.pool.Config()
	cfg.MaxConns = 1
	isolated, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer isolated.Close()
	_, err = isolated.Exec(t.Context(), `CREATE TEMP TABLE document_events (LIKE public.document_events INCLUDING ALL);
 ALTER TABLE document_events ADD CHECK (event_type <> 'sent')`)
	if err != nil {
		t.Fatal(err)
	}
	store := d.store
	store.Pool = isolated
	d.router = Handler(isolated, d.mail, d.cookies, store)
	d.call("POST", d.url+"/send", "", d.cookie, 500)
	d.counts(0, 0)
	if d.detail().Status != "draft" || len(d.mail.attempts) != 0 {
		t.Fatal("failed transaction released the draft")
	}
}

func TestOtherReleasePrerequisites(t *testing.T) {
	d := releaseFixture(t)
	d.layout(d.ravi.ID, d.asha.ID)
	d.call("PATCH", d.url, `{"email_subject":"  "}`, d.cookie, 204)
	d.call("POST", d.url+"/send", "", d.cookie, 422)
	d.call("PATCH", d.url, `{"email_subject":"Please sign"}`, d.cookie, 204)
	for _, p := range []documents.Recipient{d.ravi, d.asha} {
		d.call("DELETE", fmt.Sprintf("%s/recipients/%d", d.url, p.ID), "", d.cookie, 204)
	}
	d.call("POST", d.url+"/send", "", d.cookie, 422)
	if _, err := d.pool.Exec(t.Context(), "DELETE FROM document_files WHERE document_id=$1", d.id); err != nil {
		t.Fatal(err)
	}
	response := d.call("POST", d.url+"/send", "", d.cookie, 422)
	if !strings.Contains(string(response), "original PDF") {
		t.Fatal("missing original not detected")
	}
	d.counts(0, 0)
	if len(d.mail.attempts) != 0 {
		t.Fatal("SMTP attempted for invalid draft")
	}
}

func (m *testMailer) SendCode(email, code string) error {
	m.Lock()
	defer m.Unlock()
	if m.codes == nil {
		m.codes = map[string]string{}
	}
	m.codes[email] = code
	return nil
}
