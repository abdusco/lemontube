package internal

// language=sql
const schema = `
CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    url TEXT NOT NULL,
    title TEXT NOT NULL,
    download_type TEXT NOT NULL CHECK (download_type IN ('video', 'audio')),
    options JSON NOT NULL DEFAULT '{}' CHECK (json_valid(options)),
    video_info JSON CHECK (video_info IS NULL OR json_valid(video_info)),
    outputs JSON NOT NULL DEFAULT '[]' CHECK (json_valid(outputs)),
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'finished', 'failed', 'canceled')),
    message TEXT,
    error TEXT,
    percent REAL,
    speed REAL,
    eta REAL,
    delete_requested_at DATETIME,
    delete_outputs BOOLEAN NOT NULL DEFAULT FALSE CHECK (delete_outputs IN (FALSE, TRUE)),
    cleanup_error TEXT,
    cleanup_attempts INTEGER NOT NULL DEFAULT 0,
    cleanup_after DATETIME,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    started_at DATETIME,
    finished_at DATETIME
);

CREATE INDEX jobs_queue_idx ON jobs(status, created_at, id);
CREATE INDEX jobs_finished_idx ON jobs(status, finished_at);
CREATE INDEX jobs_cleanup_idx ON jobs(delete_requested_at, cleanup_after);

CREATE TABLE job_logs (
    id INTEGER PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    message TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX job_logs_job_idx ON job_logs(job_id, id);

CREATE TABLE cookies (
    domain TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
`
