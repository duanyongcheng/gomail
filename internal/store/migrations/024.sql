-- Keep the legacy pending_mail_ops table unchanged. Sequence numbers survive
-- timestamp ties, retries, database reopen and VACUUM.
CREATE TABLE mail_op_sequence (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    op_id TEXT NOT NULL REFERENCES pending_mail_ops(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('queued','succeeded')),
    UNIQUE(op_id,kind)
);
INSERT INTO mail_op_sequence(op_id,kind)
    SELECT id,'queued' FROM pending_mail_ops ORDER BY rowid;
