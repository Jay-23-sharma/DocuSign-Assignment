package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"signing-app/backend/internal/documents"
)

func TestDashboardFolders(t *testing.T) {
	d := releaseFixture(t)
	user, cookie := d.sender()
	_, stranger := d.sender()
	draft := d.draft(cookie)
	if _, err := d.pool.Exec(t.Context(), "UPDATE recipients SET email=upper((SELECT email FROM users WHERE id=$1)) WHERE id=$2", user, d.ravi.ID); err != nil {
		t.Fatal(err)
	}
	check := func(c *http.Cookie, query string, ids ...int64) {
		t.Helper()
		var rows []documents.ListedDocument
		if err := json.Unmarshal(d.call("GET", "/api/documents?"+query, "", c, 200), &rows); err != nil {
			t.Fatal(err)
		}
		expected := map[int64]bool{}
		for _, id := range ids {
			expected[id] = true
		}
		for _, row := range rows {
			if !expected[row.ID] {
				t.Fatalf("unexpected document in %s", query)
			}
			delete(expected, row.ID)
		}
		if len(expected) > 0 {
			t.Fatalf("missing document in %s", query)
		}
	}
	check(cookie, "view=all", draft)
	check(cookie, "view=received")
	d.layout(d.ravi.ID, d.asha.ID)
	if _, err := d.store.Release(t.Context(), d.owner, d.id); err != nil {
		t.Fatal(err)
	}
	check(cookie, "view=all", draft, d.id)
	check(cookie, "view=sent")
	check(cookie, "view=received", d.id)
	check(cookie, "view=all&status=draft", draft)
	check(cookie, "view=received&q=absent-title")
	check(d.cookie, "view=sent", d.id)
	check(stranger, "view=all")
	d.call("GET", d.url, "", cookie, 404)
}
