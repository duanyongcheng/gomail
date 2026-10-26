CREATE TABLE IF NOT EXISTS folder_sync_state (
account_id TEXT NOT NULL,
folder_id TEXT NOT NULL,
state TEXT NOT NULL,
updated_at INTEGER NOT NULL,
PRIMARY KEY (account_id, folder_id),
FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE CASCADE,
FOREIGN KEY(folder_id) REFERENCES folders(id) ON DELETE CASCADE
);
