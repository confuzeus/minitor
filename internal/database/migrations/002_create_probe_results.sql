CREATE TABLE probe_results (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id  INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    status      TEXT    NOT NULL,
    status_code INTEGER,
    latency_ms  INTEGER,
    error_msg   TEXT,
    timestamp   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_probe_results_monitor_time
    ON probe_results (monitor_id, timestamp DESC);
