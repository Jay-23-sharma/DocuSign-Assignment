# LocalSign

LocalSign lets a sender upload and prepare a PDF and collect signatures from multiple recipients in any order. The sender dashboard shows signing progress. Open a document to see its timeline, its original, and its completed PDF when available.

## Start the local app

Use Docker Compose, Go with the toolchain specified in `backend/go.mod`, Node.js and npm. The local frontend was tested with Node 24. On Ubuntu the renderer and PDF test tools can be installed with:

```sh
sudo apt install python3-gi-cairo python3-cairo gir1.2-poppler-0.18 gir1.2-pango-1.0 poppler-utils
```

The renderer was tested with Python 3.12.3, PyGObject 3.48.2, Poppler 24.02.0, Cairo 1.18.0, and Pango 1.52.1. It uses `python3` by default. To choose an interpreter, set `PDF_RENDER_PYTHON=/usr/bin/python3` in the root `.env`, or supply the absolute path to your own interpreter with the same bindings.

**Terminal 1** — From the project root, start PostgreSQL and Mailpit:

```sh
cd ~/Documents/LocalSign/signing-app
docker compose up -d --wait db mailpit
docker compose ps
```

On first setup, copy `.env.example` to a private `.env` and fill in the configuration. Reuse the existing `.env` on subsequent starts. Set `DATABASE_URL` to the project's PostgreSQL connection (host setup here: `127.0.0.1:5433`, database `signing_app`). Generate the stable `SENDER_COOKIE_KEY` once with `openssl rand -base64 32`. The local mail settings are `SMTP_ADDR=127.0.0.1:1025` and `FRONTEND_BASE_URL=http://127.0.0.1:5173`.

The existing database already has the four migrations. **For a fresh empty database only**, from the project root, apply their forward sections in one transaction:

```sh
for migration in backend/migrations/00*.sql; do
  sed '/^-- +goose Down/,$d' "$migration"
done | docker compose exec -T db psql -X -1 -v ON_ERROR_STOP=1 -U signing_app -d signing_app -f -
```

Start the API in terminal 1:

```sh
set -a
. ./.env
set +a
cd backend
go run ./cmd/api
```

**Terminal 2** — Install dependencies on first setup or after a lockfile change, then start React:

```sh
cd ~/Documents/LocalSign/signing-app/frontend
npm ci
npm run dev
```

Open the [sender app](http://127.0.0.1:5173) and the [Mailpit inbox](http://127.0.0.1:8025). Check backend connectivity with `curl -fsS http://127.0.0.1:5173/api/health`. Mailpit captures the local verification, invitation, and email-code messages.

## Demonstrate the four outcomes

1. Register Maya at the sender app, verify her email using Mailpit, and log in. Upload a PDF, open **Prepare**, and add Ravi and Asha with distinct email addresses.
2. Place a **required signature** for each recipient. Open **Review and send**, set the subject, message, and future deadline, then choose **Send invitations**.
3. Open each personal invitation from Mailpit in a separate browser profile. Choose **Open document**, read the six-digit code in that recipient's inbox, and verify it.
4. Use a fresh draft for each ending:

| Ending | Action | What Maya sees after refresh |
|---|---|---|
| Completed | Ravi chooses **Finish and sign**, then Asha does the same. | **1 of 2** advances to **2 of 2**. Open **Completed** to view history, preview either PDF, or download the final file. |
| Declined | Ravi signs. Asha chooses **Decline**, then **Confirm decline**. | **Declined**, Ravi's accepted signature, and both decisions in history. |
| Voided | Leave Ravi's verified page open. In the sender document screen, choose **Void request** and **Confirm void**. Try signing from Ravi's old tab. | **Voided**, a history entry naming Maya, and no new signature. |
| Expired | Use a deadline a few minutes ahead. Leave a recipient page open past the deadline and try to sign, or leave the request untouched. | **Expired** and one system expiration event. The API checks immediately on startup and every five seconds while running. |

If PDF generation fails after all decisions were accepted, the document stays **Signed**. Maya can choose **Generate completed PDF** even after the deadline. The completed file's SHA-256 is displayed on its page; compare a downloaded copy with `sha256sum path/to/completed-document.pdf`.

## Restart and verify persistence

Stop the API and Vite with Ctrl-C in their terminals. From the project root run `docker compose restart db mailpit`, then repeat the terminal startup commands above (existing frontend dependencies can be reused). Sign in again and reopen the documents.

The Compose `postgres_data` volume holds accounts, document states, signing decisions, file hashes, and history. Originals, signature images, and completed PDFs are private files under `storage/documents` by default. Keep that directory and the database volume together across restarts. If you configure `DOCUMENT_STORAGE_DIR`, use the same directory on each start. The default relative path assumes the API runs from `backend/`.

## Tests

From `backend/` with the root `.env` exported as in terminal 1, set `TEST_DATABASE_URL` to a separately migrated local test database before running the database suite. Lifecycle tests exercise the expiration worker throughout that test database.

```sh
go vet ./...
go build ./...
go test -count=1 ./...
```

From `frontend/` run `npm test` and `npm run build`.
