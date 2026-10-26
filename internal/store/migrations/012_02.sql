CREATE TABLE accounts_new (
            id TEXT PRIMARY KEY,
            email TEXT NOT NULL,
            display_name TEXT NOT NULL DEFAULT '',
            color TEXT,
            provider TEXT NOT NULL CHECK(provider IN ('imap', 'pop3', 'gmail', 'outlook')),
            auth_data BLOB,
            sync_state TEXT,
            created_at INTEGER NOT NULL,
            updated_at INTEGER NOT NULL
        );
        INSERT INTO accounts_new (id, email, display_name, color, provider, auth_data, sync_state, created_at, updated_at)
            SELECT id, email, display_name, color, provider, auth_data, sync_state, created_at, updated_at
            FROM accounts;
        DROP TABLE accounts;
        ALTER TABLE accounts_new RENAME TO accounts;
