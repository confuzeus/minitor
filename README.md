# Minitor

Minitor is a self-hosted, single-binary monitoring tool for solo SaaS founders. It performs scheduled HTTP and ping health checks against configured endpoints, stores results in SQLite, and sends email alerts via SMTP when monitors go down or recover. The entire application ships as a statically compiled Go binary with zero runtime dependencies beyond the binary itself and a writable directory for the database.
