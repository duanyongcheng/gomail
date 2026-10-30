CREATE TABLE email_signatures (
    account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    data BLOB NOT NULL
);
