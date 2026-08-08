# Minitor Smoke Test Checklist

Manual end-to-end walkthrough to run after building a release binary. Run the
automated checks first, then verify the interactive UI steps in a browser.

Prerequisites:

- `just test` passes (automated unit + integration tests).
- `just build` produces a binary.
- A running probe target for a real endpoint (e.g. `http://example.com`, or a
  local `python3 -m http.server 9000`).

## 0. Startup

- [ ] Binary starts cleanly: `./minitor -data-dir /tmp/minitor-smoke`
- [ ] Logs show `minitor starting` and `minitor listening`
- [ ] Data directory and `<data-dir>/minitor.db` are created
- [ ] `GET /api/status` returns `{"status":"ok"}`:
      `curl -s http://localhost:8080/api/status`
- [ ] Home page loads at http://localhost:8080 without errors
- [ ] CSS is applied (custom font and coral/orange accents; not unstyled HTML)
- [ ] Ctrl-C shuts the server down gracefully with `minitor stopped`

## 1. Dashboard

- [ ] Empty state is shown when no monitors exist
- [ ] Nav shows Dashboard, Monitors, Alerts links

## 2. Create a monitor

- [ ] Navigate to Monitors → New Monitor
- [ ] Empty form submits show validation errors (name required, URL required,
      invalid interval/timeout)
- [ ] Create an HTTP monitor pointing at your probe target
      (interval 10s, timeout 5s, follow redirects off)
- [ ] Redirect lands on the Monitors list; the monitor appears
- [ ] Monitor card shows status (green/Up after first probe)
- [ ] Without JS enabled, the form still creates the monitor (POST fallback)

## 3. Monitor detail

- [ ] Click the monitor to open its detail page
- [ ] Recent probe results appear in the table
- [ ] Status column shows `up`, HTTP status code, and latency
- [ ] Detail page auto-refreshes (HTMX polling) without a full reload

## 4. Edit a monitor

- [ ] Open Edit, change the name and interval to 5s, save
- [ ] Changes persist and the detail page reflects them
- [ ] Editing to a down endpoint flips the status to down/pulsing red
- [ ] Disable the monitor; it stops producing new results
- [ ] Re-enable the monitor; probing resumes

## 5. Delete a monitor

- [ ] Delete the monitor; it disappears from the list
- [ ] Its probe results are removed (foreign-key cascade)

## 6. Alert recipients

- [ ] Navigate to Alerts
- [ ] Create a recipient with a valid email (reject `Bob <bob@x.com>` style
      and malformed addresses)
- [ ] Link the recipient to a monitor with On Down / On Recovery enabled
- [ ] Recipient card lists the assigned monitor

## 7. Alert delivery (SMTP)

- [ ] With SMTP configured, take a linked monitor down
      (point it at a dead port or `http://localhost:1`)
- [ ] A DOWN alert email arrives at the recipient
- [ ] Bring the monitor back up; a RECOVERED email arrives
- [ ] With `consecutive_failures > 1`, a single blip does not send an alert

## 8. Authentication

- [ ] Restart with `ADMIN_PASSWORD` and a fixed `SECRET_KEY` set
- [ ] Visiting `/monitors` unauthenticated redirects to `/login`
- [ ] Wrong password returns 401 "Invalid password"
- [ ] Correct password logs in and lands on the dashboard
- [ ] Protected pages render with a logged-in session
- [ ] Logout clears the session and returns to `/login`
- [ ] Visiting a protected page after logout redirects to `/login`
- [ ] `/api/status` stays public even when auth is enabled
- [ ] Restarting with the same `SECRET_KEY` keeps the session valid
- [ ] Restarting without `SECRET_KEY` invalidates the session (new random key)

## 9. Validation / config

- [ ] `./minitor -version` prints the version
- [ ] `./minitor -migrate` runs migrations and exits
- [ ] Partial `SMTP_*` config fails to start with a clear error
- [ ] Invalid `SECURE_COOKIES` / `RETENTION_DAYS` fail to start

## Notes

- The automated integration tests cover startup → monitor create → probe →
  result → alert, and the full auth lifecycle. This checklist covers the
  browser-visible behavior those tests cannot assert (CSS, HTMX polling,
  no-JS fallbacks, real SMTP delivery).
- Use a scratch `-data-dir` (e.g. `/tmp/minitor-smoke`) to avoid touching real
  data.
