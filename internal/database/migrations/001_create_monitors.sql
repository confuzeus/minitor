CREATE TABLE monitors (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    url         TEXT    NOT NULL,
    type        TEXT    NOT NULL DEFAULT 'http',
    interval    INTEGER NOT NULL DEFAULT 60,
    timeout     INTEGER NOT NULL DEFAULT 30,
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
