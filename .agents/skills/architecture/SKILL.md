---
name: architecture
description: "Project architecture document. Use when you need to understand the system architecture, components, or data flow when implementing or updating features."
---

## 1. Executive Summary

Minitor is a self-hosted, single-binary monitoring tool for solo SaaS founders. It performs scheduled HTTP and ping health checks against configured endpoints, stores results in SQLite, and sends email alerts via SMTP when monitors go down or recover. The entire application ships as a statically compiled Go binary with zero runtime dependencies beyond the binary itself and a writable directory for the database.

---

## 2. Core Design Decisions

### Single Binary, Zero External Dependencies

The Go binary embeds all HTML templates, CSS, and JavaScript using `go:embed`. SQLite runs in-process via `modernc.org/sqlite`, a pure Go driver requiring no CGO. This eliminates the need for users to install a database server, configure connection strings, or manage separate processes. Cross-compilation targets every platform Go supports without toolchain complications.

### Timer-Based Probe Scheduler

A background goroutine manager maintains a map of monitor ID to `time.Ticker`, firing probes on each monitor's configured interval. No message queues, no Redis, no distributed coordination. On startup, the scheduler reconstructs its state from the `monitors` table. If the process is down when a probe was due, it fires immediately on restart. This is acceptable because Minitor is a best-effort monitoring tool, not a paging system for critical infrastructure.

### SQLite with WAL Mode

SQLite handles concurrent reads from the web dashboard and writes from the probe engine without issue when WAL mode is enabled. For a single-user tool monitoring up to a few hundred endpoints, this will never become a bottleneck. The database is a single file in a user-specified directory. Backups are a file copy.

### Environment-Driven Configuration

All operational settings come from environment variables. There is no config file, no settings UI, and no settings stored in the database. This keeps configuration where operators expect it — alongside the process definition in a systemd unit or Docker environment block. Configuration changes require a restart, which is acceptable for a tool where a 2-second gap has no meaningful impact.

### Conditional Authentication

If the `ADMIN_PASSWORD` environment variable is set, all routes except `/login`, `/api/status`, and static assets require authentication via a signed HTTP-only session cookie. If unset, the entire application is open. This preserves the zero-config path for users running on localhost or behind a VPN while providing meaningful security for users with public-facing instances.

---

## 3. Technology Stack

| Layer                  | Choice                             | Rationale                                                                                                                                           |
| ---------------------- | ---------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| Language               | Go 1.22+                           | Single binary compilation, excellent concurrency primitives, stdlib HTTP server suitable for production, large ecosystem                            |
| Router                 | `chi`                              | Adds route groups, middleware chaining, and path parameters to `net/http` without becoming a framework                                              |
| Database               | SQLite via `modernc.org/sqlite`    | Pure Go, no CGO, embeddable, zero operational overhead                                                                                              |
| Migrations             | `golang-migrate` with embedded SQL | Lightweight, file-based migrations embedded into the binary                                                                                         |
| Templates              | `html/template` (stdlib)           | Context-aware escaping prevents XSS, sufficient for server-rendered HTML                                                                            |
| CSS                    | TailwindCSS 4                      | Utility-first, purges unused styles at build time, output embedded                                                                                  |
| Frontend interactivity | HTMX + Alpine.js                   | HTMX handles AJAX partial updates and polling; Alpine handles client-side state for dropdowns and modals. Both are small, dependency-free libraries |
| Logging                | `log/slog` (stdlib)                | Structured JSON logging, level support, zero dependencies since Go 1.21                                                                             |
| HTTP probing           | `net/http` (stdlib)                | Configurable timeouts, redirect following, TLS metadata inspection                                                                                  |
| ICMP ping              | `go-ping/ping`                     | Handles raw socket complexity; graceful fallback to TCP dial when ICMP is unavailable                                                               |
| SMTP                   | `net/smtp` (stdlib)                | Sends plain text email without external libraries                                                                                                   |
| CLI                    | `flag` (stdlib)                    | Handles `--version`, `--help`, and a `--migrate` command for manual migration runs                                                                  |

---

## 4. Project Structure

```
minitor/
├── main.go                    # Entry point: load settings, run migrations, start scheduler, start server
├── go.mod
├── go.sum
├── tailwind.config.js
├── package.json               # Only for Tailwind CLI; not needed at runtime
├── Dockerfile
├── docker-compose.yml         # Optional, for users who prefer containers
├── README.md
│
├── cmd/
│   └── minitor/
│       └── main.go            # Alternative: cobra-style entry if multiple subcommands emerge
│
├── internal/
│   ├── settings/
│   │   └── settings.go        # Env var parsing, validation, defaults
│   │
│   ├── database/
│   │   ├── db.go              # Open/create SQLite, enable WAL, return *sql.DB
│   │   └── migrations/
│   │       └── *.sql          # Embedded SQL migration files
│   │
│   ├── models/
│   │   ├── monitor.go         # Monitor struct + DB methods (Create, List, GetByID, Update, Delete)
│   │   ├── probe_result.go    # ProbeResult struct + Insert, GetByMonitorID, CleanupOld
│   │   ├── alert_recipient.go # AlertRecipient struct + CRUD
│   │   └── monitor_alert.go   # MonitorAlert struct (links monitors to recipients)
│   │
│   ├── probe/
│   │   ├── scheduler.go       # Ticker manager: Start, Stop, AddMonitor, RemoveMonitor
│   │   ├── http_probe.go      # HTTP probe logic
│   │   └── ping_probe.go      # ICMP/TCP fallback probe logic
│   │
│   ├── alerter/
│   │   └── alerter.go         # State machine: detect transitions, build email, send via SMTP
│   │
│   ├── auth/
│   │   ├── middleware.go      # AuthRequired middleware
│   │   └── session.go         # Create, validate, clear session cookies
│   │
│   ├── handlers/
│   │   ├── dashboard.go       # GET /
│   │   ├── monitors.go        # CRUD handlers for monitors
│   │   ├── alerts.go          # Alert recipient management
│   │   ├── auth.go            # Login, logout handlers
│   │   └── api.go             # JSON endpoints for HTMX polling and stats
│   │
│   └── templates/
│       ├── base.html          # Base layout with nav
│       ├── dashboard.html
│       ├── monitor_form.html
│       ├── monitor_detail.html
│       ├── monitor_list.html
│       ├── alerts.html
│       ├── settings_info.html # Read-only display of env config
│       └── login.html
│
├── static/
│   ├── css/
│   │   └── tailwind.css       # Source file; compiled to dist/ before embedding
│   └── dist/
│       └── tailwind.min.css   # Compiled output, embedded via go:embed
│
└── embed.go                   # //go:embed directives for templates and static/dist
```

All Go code lives under `internal/` to prevent external import. The `embed.go` file at the root holds `//go:embed` directives pointing to `templates/` and `static/dist/`.

---

## 5. System Components & Data Flow

### Startup Sequence

1. Parse environment variables into `Settings` struct. Validate required combinations (SMTP settings must all be present if any are set).
2. Open SQLite database at `$DATA_DIR/minitor.db`. Create directory if it doesn't exist. Enable WAL mode. Run embedded migrations.
3. If `SECRET_KEY` is not set, generate a random 32-byte key. Log a warning that sessions will expire on restart.
4. Load all enabled monitors from the database.
5. Initialize the probe scheduler. For each monitor, create a ticker that fires at `interval_secs`. The first probe fires after a short jitter (0–10 seconds) to avoid thundering herd on restart.
6. Initialize the alerter with a reference to SMTP settings and a function to query recent probe results.
7. Start HTTP server on `:$PORT`.

### Probe Execution Flow

1. Scheduler ticker fires for monitor M.
2. Scheduler spawns a goroutine to execute the probe (non-blocking; a slow probe must not delay others).
3. Probe function (HTTP or ping) runs with the configured timeout. It records: success/failure, response time in milliseconds, HTTP status code (if HTTP), error message (if any), TLS certificate expiry (if HTTPS).
4. Result is inserted into `probe_results`.
5. Alerter is notified with the monitor ID and the new result.
6. Alerter queries the last N results for that monitor (where N = `consecutive_failures` threshold). It determines if state has transitioned (up→down or down→up).
7. If a transition is detected, the alerter queries `monitor_alerts` for recipients with matching notification preferences, then sends an email to each.

### Dashboard Rendering Flow

1. User requests `GET /`.
2. Handler queries all monitors with their latest probe result (single query with a subquery or join).
3. Template renders status cards: name, type, target, current status (green/red dot), last response time, last checked timestamp.
4. HTMX polling: the dashboard includes a `hx-get="/api/monitors" hx-trigger="every 30s"` attribute. Every 30 seconds, the browser requests JSON monitor statuses and swaps the card content without a full page reload.
5. If auth is enabled and the session cookie is expired or missing, the HTMX request gets a 401, and a small script redirects to `/login`.

### Alert Email Flow

The email is plain text with no HTML formatting. It includes: monitor name, target URL or host, what happened (went down / recovered), timestamp of the failing/succeeding probe, error message if down, and a note that this is from Minitor. The `SMTP_FROM` address is used as the sender. Subject line: `[Minitor] {{.MonitorName}} is DOWN` or `[Minitor] {{.MonitorName}} RECOVERED`.

---

## 6. Data Model

### Entity Relationships

- A **Monitor** has many **ProbeResults** (one per probe execution).
- A **Monitor** has many **MonitorAlerts** (linking to recipients).
- An **AlertRecipient** has many **MonitorAlerts** (can receive alerts for multiple monitors).
- MonitorAlerts is a join table with additional config: whether to alert on down, on recovery, and how many consecutive failures before triggering.

### Database Schema

Four tables: `monitors`, `probe_results`, `alert_recipients`, `monitor_alerts`. All primary keys are UUIDs generated in application code. Timestamps are ISO 8601 text strings for SQLite compatibility. All foreign keys have `ON DELETE CASCADE` so deleting a monitor cleans up its results and alert links.

The `probe_results` table has a composite index on `(monitor_id, timestamp DESC)` for efficient retrieval of recent results per monitor. A retention cleanup query runs daily, deleting results older than `RETENTION_DAYS`.

### Migrations Strategy

Migrations are numbered SQL files embedded in the binary. On startup, `golang-migrate` checks a schema version table and applies any pending migrations. For MVP, migrations run automatically. A `--migrate` flag is available for users who want to run them manually (useful in restricted environments).

---

## 7. API Design

### Routing Architecture

The application is primarily server-rendered HTML. A small set of JSON endpoints supports HTMX partial updates. The router is split into two groups: public routes (login, logout, health check) and protected routes (everything else). The auth middleware checks for `ADMIN_PASSWORD` being set and skips entirely if it's empty.

### Route Table

Public routes require no authentication regardless of configuration. Protected routes require a valid session cookie when `ADMIN_PASSWORD` is set.

| Method | Path                      | Auth      | Purpose                                                 |
| ------ | ------------------------- | --------- | ------------------------------------------------------- |
| GET    | `/login`                  | Public    | Login form                                              |
| POST   | `/login`                  | Public    | Process login, set session cookie                       |
| POST   | `/logout`                 | Public    | Clear session, redirect to login                        |
| GET    | `/api/status`             | Public    | Health check, returns 200 `{"status":"ok"}`             |
| GET    | `/`                       | Protected | Dashboard with all monitor status cards                 |
| GET    | `/monitors`               | Protected | Monitor list                                            |
| GET    | `/monitors/new`           | Protected | Create monitor form                                     |
| POST   | `/monitors`               | Protected | Create monitor                                          |
| GET    | `/monitors/:id`           | Protected | Monitor detail with recent results                      |
| GET    | `/monitors/:id/edit`      | Protected | Edit monitor form                                       |
| PUT    | `/monitors/:id`           | Protected | Update monitor                                          |
| DELETE | `/monitors/:id`           | Protected | Delete monitor and cascade                              |
| GET    | `/alerts`                 | Protected | Alert recipient list                                    |
| POST   | `/alerts`                 | Protected | Add recipient                                           |
| DELETE | `/alerts/:id`             | Protected | Remove recipient                                        |
| GET    | `/api/monitors`           | Protected | JSON array of monitors with latest status               |
| GET    | `/api/monitors/:id/stats` | Protected | JSON stats: uptime %, avg response time, recent results |
| GET    | `/api/settings`           | Protected | JSON of current config (password redacted)              |

### Authentication Mechanism

The login form accepts a password. The server compares it to `ADMIN_PASSWORD` using constant-time comparison. On match, it creates an HTTP-only cookie named `monitool_session` containing a base64-encoded payload (fixed string `admin` plus expiry Unix timestamp) concatenated with an HMAC-SHA256 signature. The signing key is `SECRET_KEY` (or a randomly generated key valid only for the current process lifetime).

The auth middleware extracts and validates the cookie on each request: decode payload, check expiry, recompute HMAC, compare signatures with constant-time comparison. Failed validation returns 401 for API routes and redirects to `/login` for page routes.

No brute force protection is implemented in MVP. Users who expose their instance publicly should place it behind a reverse proxy that can add rate limiting.

---

## 8. Probe Engine

### Scheduler

The scheduler maintains an in-memory map of `monitorID -> *time.Ticker`. A dedicated goroutine handles start/stop/add/remove operations via channels to avoid concurrent map access. When a monitor is created via the UI, the handler inserts into the database and sends an `AddMonitor` message to the scheduler channel. When deleted, a `RemoveMonitor` message stops the ticker.

On startup, the scheduler loads all enabled monitors and creates tickers. The first probe for each monitor fires after a random jitter of 0–10 seconds to spread the load.

### HTTP Probe

Uses `net/http.Client` with a timeout equal to the monitor's configured `timeout_secs`. Follows redirects if configured. Checks that the response status code matches the expected status. Records the round-trip time in milliseconds. If the target uses HTTPS, inspects the TLS connection state for certificate expiry and records it. Connection errors, timeouts, and unexpected status codes all count as failures.

### Ping Probe

Uses `go-ping` to send ICMP echo requests. If ICMP fails (common in containerized environments without `CAP_NET_RAW`), it falls back to a TCP dial on port 80 (or a configurable port) with the configured timeout. The fallback is logged clearly so users understand why "ping" is succeeding via TCP.

### Concurrency Model

Each probe runs in its own goroutine. The scheduler does not wait for a probe to complete before the next tick. If a probe takes longer than its interval (e.g., 30-second probe on a 10-second interval), the next tick simply starts another goroutine. This is acceptable for a tool of this scale; probe timeouts keep this from being a resource leak.

---

## 9. Alert Engine

### State Detection

The alerter is stateless. When notified of a new probe result, it queries the last `consecutive_failures` results for that monitor. If all are failures and the previous batch contained at least one success (or this is the first batch), the monitor is considered newly down. If all are successes and the previous batch contained at least one failure, the monitor is considered recovered. All other cases produce no alert.

This approach inherently handles the consecutive failure threshold without maintaining in-memory state, at the cost of an extra database query per probe. Given the scale (hundreds of probes per minute at most), this query cost is negligible.

### Email Sending

For each alert that should fire, the alerter looks up recipients via the `monitor_alerts` join table, filtering by notification preference (on_down or on_recovery matching the current transition). It constructs a plain text email and sends it using `net/smtp`. SMTP connection is opened per email batch, not per email — if three recipients need the same alert, one connection sends all three.

If SMTP sending fails, the error is logged at ERROR level. No retry logic is implemented in MVP. The next probe cycle will re-evaluate state, and if the condition persists, a new alert will fire (not a duplicate — the state machine won't re-trigger).

---

## 10. Frontend Architecture

### Template System

All HTML is rendered server-side using `html/template`. A base layout (`base.html`) defines the document structure, navigation, and slots for content. Page templates embed into the base layout. HTMX attributes are applied directly in templates for partial updates.

The navigation bar shows: "Minitor" branding on the left, and links to Dashboard, Monitors, Alerts, Settings on the right. If auth is enabled, a logout button is present. If SMTP is not configured, a subtle yellow banner appears: "Email alerts disabled. Set SMTP_HOST to enable."

### Interactivity Model

HTMX handles all server communication for partial updates. The dashboard polls `/api/monitors` every 30 seconds and swaps status card content. Forms submit via standard HTTP POST/PUT (not AJAX) to keep behavior predictable and debuggable. Alpine.js handles purely client-side interactions: the settings page password visibility toggle, confirmation modals before deleting a monitor, dropdown menus if needed later.

### Build Process

TailwindCSS is compiled at build time via its standalone CLI. The input file (`static/css/tailwind.css`) contains `@tailwind base/components/utilities` directives. The output is minified and written to `static/dist/tailwind.min.css`, which is embedded into the binary. No JavaScript build step is needed — HTMX and Alpine are loaded from a CDN in the base template (or embedded as inline scripts to avoid the CDN dependency entirely; the libraries are small enough to inline).

---

## 11. Security

### Secrets Handling

`ADMIN_PASSWORD` and `SMTP_PASSWORD` live in environment variables. They are never written to disk, logged, or exposed via the settings API endpoint. The `/api/settings` response redacts `SMTP_PASSWORD` as `********`. The session signing key (`SECRET_KEY`) is either set via environment or generated in-memory, never persisted.

### Transport Security

Minitor does not serve HTTPS directly. Users who need encrypted access should place it behind a reverse proxy (Caddy, nginx) or a Cloudflare Tunnel. The documentation provides example Caddy configuration. The session cookie has `Secure: false` by default; if the application detects an `X-Forwarded-Proto: https` header on requests, it sets `Secure: true`. This is not automatic in MVP; users can set an env var `SECURE_COOKIES=true` to force it.

### Input Validation

All form inputs are validated server-side before database insertion. URL targets are parsed with `net/url.Parse` to ensure well-formedness. Email addresses are validated with a simple regex (presence of `@` and a domain). Intervals are bounded (minimum 10 seconds, maximum 86400 seconds). HTML templates use `html/template` which escapes output by default, preventing XSS. No user-generated HTML is ever rendered raw.

### Dependency Security

`govulncheck` runs in CI on every push. The Go toolchain's built-in vulnerability database catches known issues in direct and transitive dependencies without external services.

---

## 12. Observability

### Logging

All logs are structured JSON written to stdout via `slog`. Log levels: DEBUG (probe details, scheduler operations), INFO (startup, shutdown, monitor state changes, alert sending), WARN (probe timeouts, SMTP failures, ping fallback to TCP), ERROR (database errors, unexpected panics recovered). In production, users can pipe stdout to a file or a log aggregator of their choice.

### Health Check

`GET /api/status` returns `{"status":"ok"}` with HTTP 200 if the server is running. It performs no database query — it simply confirms the HTTP server is accepting requests. This endpoint is intentionally unauthenticated so external monitoring tools can verify Minitor itself is running.

### Metrics

A basic `/metrics` endpoint exposes Prometheus-format counters: total probes executed, probes failed, alerts sent, and monitors active. This is rendered as plain text; no Prometheus client library is required. Users who want metrics can point Prometheus at this endpoint. This is a Phase 2 feature; MVP relies on the dashboard and logs.

### Error Visibility

Probe failures are visible on the dashboard immediately (red status indicator, error message displayed). Recent results on the monitor detail page show failure reasons. There is no separate error tracking system — the database of probe results serves this function.
