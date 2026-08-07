CREATE TABLE monitor_alerts (
    monitor_id   INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    recipient_id INTEGER NOT NULL REFERENCES alert_recipients(id) ON DELETE CASCADE,
    PRIMARY KEY (monitor_id, recipient_id)
);
