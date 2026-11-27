package documents

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Summary struct {
	Draft
	SignedCount    int       `json:"signed_count"`
	RecipientCount int       `json:"recipient_count"`
	CreatedAt      time.Time `json:"created_at"`
}
type Detail struct {
	SendSettings
	CompletedAt     *time.Time `json:"completed_at"`
	CompletedSHA256 *string    `json:"completed_sha256"`
	SentAt          *time.Time `json:"sent_at"`
	Summary
	Fields     []Field     `json:"fields"`
	Recipients []Recipient `json:"recipients"`
	Original   *Original   `json:"original"`
}

type ListedDocument struct {
	Summary
	Owned  bool   `json:"owned"`
	Sender string `json:"sender"`
}

func (s Store) List(ctx context.Context, userID int64, email, view, query, status string) ([]ListedDocument, error) {
	rows, err := s.Pool.Query(ctx, `SELECT d.id,d.title,d.status,d.created_at,counts.signed,counts.total,d.owner_id=$1,u.name
 FROM documents d JOIN users u ON u.id=d.owner_id
 CROSS JOIN LATERAL (SELECT count(*) FILTER (WHERE r.status='signed') AS signed,count(*) AS total,
 bool_or(lower(r.email)=lower($2)) AS addressed FROM recipients r WHERE r.document_id=d.id) counts
 WHERE ((d.owner_id=$1 AND ($3 IN ('owned','all') OR ($3='sent' AND d.sent_at IS NOT NULL)))
 OR ($3 IN ('all','received') AND d.sent_at IS NOT NULL AND counts.addressed))
 AND strpos(lower(d.title),lower($4))>0 AND ($5='' OR d.status=$5)
 ORDER BY d.created_at DESC,d.id DESC`, userID, email, view, query, status)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ListedDocument, error) {
		var d ListedDocument
		err := row.Scan(&d.ID, &d.Title, &d.Status, &d.CreatedAt, &d.SignedCount, &d.RecipientCount, &d.Owned, &d.Sender)
		return d, err
	})
}

func (s Store) Detail(ctx context.Context, owner, id int64) (Detail, error) {
	var d Detail
	err := s.Pool.QueryRow(ctx, `SELECT d.id,d.title,d.status,d.created_at,d.email_subject,d.email_message,d.expires_at,d.sent_at,d.completed_at,
 (SELECT encode(sha256,'hex') FROM document_files WHERE document_id=d.id AND kind='completed'),
 CASE WHEN f.id IS NOT NULL THEN json_build_object('filename',f.original_filename,'size_bytes',f.size_bytes,'page_count',f.page_count) END,
 coalesce((SELECT json_agg(json_build_object('id',r.id,'name',r.name,'email',r.email,'status',r.status,
   'delivery_status',CASE WHEN d.sent_at IS NOT NULL THEN coalesce((SELECT CASE e.event_type
     WHEN 'invitation_accepted' THEN 'accepted' WHEN 'invitation_failed' THEN 'failed' ELSE 'unconfirmed' END
     FROM document_events e WHERE e.document_id=d.id AND e.recipient_id=r.id
       AND e.event_type IN ('invitation_issued','invitation_accepted','invitation_failed') ORDER BY e.id DESC LIMIT 1),'unconfirmed') END) ORDER BY r.id) FROM recipients r WHERE r.document_id=d.id),'[]'),
 coalesce((SELECT json_agg(json_build_object(
   'recipient_id',f.recipient_id,'field_type',f.field_type,'page_number',f.page_number,
   'x',f.x,'y',f.y,'width',f.width,'height',f.height,'required',f.required) ORDER BY f.id)
   FROM document_fields f WHERE f.document_id=d.id),'[]')
 FROM documents d LEFT JOIN document_files f ON f.document_id=d.id AND f.kind='original'
 WHERE d.owner_id=$1 AND d.id=$2`, owner, id).Scan(&d.ID, &d.Title, &d.Status, &d.CreatedAt, &d.Subject, &d.Message, &d.Deadline, &d.SentAt, &d.CompletedAt, &d.CompletedSHA256, &d.Original, &d.Recipients, &d.Fields)
	d.RecipientCount = len(d.Recipients)
	for _, r := range d.Recipients {
		if r.Status == "signed" {
			d.SignedCount++
		}
	}
	return d, err
}

func (s Store) OriginalKey(ctx context.Context, owner, id int64) (key, filename string, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT f.storage_key,f.original_filename
 FROM documents d JOIN document_files f ON f.document_id=d.id AND f.kind='original'
 WHERE d.owner_id=$1 AND d.id=$2`, owner, id).Scan(&key, &filename)
	return
}

type HistoryEvent struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	At        time.Time `json:"occurred_at"`
	Sender    *string   `json:"sender"`
	Recipient *string   `json:"recipient"`
}

func (s Store) History(ctx context.Context, owner, id int64) ([]HistoryEvent, error) {
	var events []HistoryEvent
	err := s.Pool.QueryRow(ctx, `SELECT coalesce((SELECT json_agg(json_build_object(
 'id',e.id,'type',e.event_type,'occurred_at',e.occurred_at,'sender',u.name,'recipient',r.name) ORDER BY e.id)
 FROM document_events e LEFT JOIN users u ON u.id=e.actor_user_id
 LEFT JOIN recipients r ON r.document_id=e.document_id AND r.id=e.recipient_id
 WHERE e.document_id=d.id),'[]') FROM documents d WHERE d.id=$1 AND d.owner_id=$2`, id, owner).Scan(&events)
	return events, err
}
