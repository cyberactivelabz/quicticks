# QuicTicks

Ticketing system for **The Vibez and Fun Party 3.0**, with a static buyer site and a Go/SQLite API.

## Local setup

1. Copy `.env.example` to `.env` and set every value. `ADMIN_PASSWORD` must be a bcrypt hash. Generate one interactively with:

	`go run ./cmd/hashpassword`

2. Install dependencies and run the API from the repository root:

	`go mod tidy`

	`set -a; . ./.env; set +a; go run ./server`

3. Open `http://localhost:8080`. The API serves `index.html`, `admin.html`, and `scan.html` from the same origin. The production frontend can also be hosted on GitHub Pages by setting `window.API_BASE_URL` in the pages to the deployed API origin and setting `CORS_ORIGIN` on the API.

## Payments

Orders are always created as `pending`; the server calculates the total from the SQLite settings table. The buyer is instructed to pay the configured Moniepoint account:

`8075340904` · `Moniepoint` · `Faruq Olabanji Adigun`

Configure Moniepoint to POST its signed payment event to `/api/payments/moniepoint/webhook` with `reference` and `status: paid` in the JSON body and the HMAC-SHA256 hex digest in `X-Moniepoint-Signature`. The webhook is idempotent: a paid order receives tickets only once. Pending bank-transfer orders can also be marked paid from the admin dashboard.

The API does not trust client totals, issues QR PNGs server-side, validates each ticket once at `/scan.html`, and sends security headers. Set `RESEND_API_KEY` and `EMAIL_FROM` to a verified sender to send an HTML receipt with QR codes through Resend after manual payment confirmation. Gmail SMTP remains available as a fallback. Never commit provider credentials.

## Admin and deployment

- `admin.html`: authenticated dashboard, revenue summary, order list, CSV export, and manual payment confirmation.
- `scan.html`: authenticated one-time ticket scanner.
- `Dockerfile`: builds a static Go container suitable for Render or Fly.io.
- `DATABASE_PATH`: use a persistent disk in production; SQLite is not suitable for ephemeral deployment storage.

