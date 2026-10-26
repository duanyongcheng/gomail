CREATE TABLE IF NOT EXISTS sync_failures (
account_id TEXT NOT NULL,
folder_id TEXT NOT NULL,
remote_id TEXT NOT NULL,
provider TEXT NOT NULL,
reason TEXT NOT NULL,
attempts INTEGER NOT NULL DEFAULT 1,
updated_at INTEGER NOT NULL,
PRIMARY KEY (account_id, folder_id, remote_id),
FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE CASCADE,
FOREIGN KEY(folder_id) REFERENCES folders(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_sync_failures_folder
ON sync_failures(account_id, folder_id);
