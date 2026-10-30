package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationSQL embed.FS

type migrationFunc func(context.Context, *sql.Tx) error

var migrations = []migrationFunc{
	1:  sqlMigration("001.sql"),
	2:  migrateV2,
	3:  sqlMigration("003_00.sql"),
	4:  sqlMigration("004_00.sql"),
	5:  sqlMigration("005_00.sql"),
	6:  sqlMigration("006_00.sql"),
	7:  sqlMigration("007_00.sql"),
	8:  sqlMigration("008_00.sql"),
	9:  sqlMigration("009_00.sql"),
	10: sqlMigration("010_00.sql"),
	11: migrateV11,
	12: migrateV12,
	13: migrateV13,
	14: migrateV14,
	15: ifTables(sqlMigration("015_00.sql"), "messages"),
	16: migrateV16,
	17: sqlMigration("017_00.sql"),
	18: ifTables(sqlMigration("018_00.sql"), "accounts"),
	19: sqlMigration("019_00.sql"),
	20: migrateV20,
	21: sqlMigration("021.sql"),
	22: sqlMigration("022.sql"),
	23: sqlMigration("023.sql"),
	24: sqlMigration("024.sql"),
	25: sqlMigration("025.sql"),
	26: sqlMigration("026.sql"),
	27: migrateThreads,
	28: sqlMigration("028.sql"),
	29: sqlMigration("029.sql"),
	30: sqlMigration("030.sql"),
	31: sqlMigration("031.sql"),
}

// migrate holds one physical connection throughout the upgrade. V12 must turn
// FK enforcement off outside its transaction, then validate before committing
// and restore enforcement even on failure.
func migrate(ctx context.Context, db *sql.DB, target int) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version < 0 || version > target || target >= len(migrations) {
		return fmt.Errorf("unsupported schema version %d (target %d)", version, target)
	}
	if version == 0 {
		var count int
		if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("unversioned database is not an empty profile; use a new Pebble-Go profile")
		}
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		return err
	}
	for next := version + 1; next <= target; next++ {
		if err := runMigration(ctx, conn, next); err != nil {
			return fmt.Errorf("migration v%d: %w", next, err)
		}
	}
	return nil
}

func runMigration(ctx context.Context, conn *sql.Conn, version int) (err error) {
	if version == 12 {
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
			return err
		}
		defer func() {
			// Cancellation must not leave a pooled connection with FKs off.
			_, restoreErr := conn.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")
			err = errors.Join(err, restoreErr)
		}()
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := migrations[version](ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version="+strconv.Itoa(version)); err != nil {
		return err
	}
	return tx.Commit()
}

func sqlMigration(names ...string) migrationFunc {
	return func(ctx context.Context, tx *sql.Tx) error {
		for _, name := range names {
			data, err := migrationSQL.ReadFile("migrations/" + name)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, string(data)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		return nil
	}
}

func ifTables(apply migrationFunc, names ...string) migrationFunc {
	return func(ctx context.Context, tx *sql.Tx) error {
		for _, name := range names {
			exists, err := tableExists(ctx, tx, name)
			if err != nil || !exists {
				return err
			}
		}
		return apply(ctx, tx)
	}
}

func tableExists(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)", table).Scan(&exists)
	return exists, err
}

func columnExists(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	// table names are internal literals; the column name remains a bound value.
	var exists bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name=?)", table, column).Scan(&exists)
	return exists, err
}

func migrateV2(ctx context.Context, tx *sql.Tx) error {
	has, err := columnExists(ctx, tx, "attachments", "content_id")
	if err != nil || has {
		return err
	}
	return sqlMigration("002_00.sql")(ctx, tx)
}

var accountColorPresets = [...]string{
	"#0ea5e9", "#22c55e", "#f59e0b", "#8b5cf6", "#f43f5e", "#14b8a6",
	"#6366f1", "#f97316", "#06b6d4", "#ec4899", "#84cc16", "#3b82f6",
}

func validColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, b := range []byte(value[1:]) {
		if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')) {
			return false
		}
	}
	return true
}

func derivedColor(seed string) string {
	var hash uint32
	for _, b := range []byte(seed) {
		hash = hash*31 + uint32(b)
	}
	return accountColorPresets[hash%uint32(len(accountColorPresets))]
}

func migrateV11(ctx context.Context, tx *sql.Tx) error {
	has, err := columnExists(ctx, tx, "accounts", "color")
	if err != nil {
		return err
	}
	if !has {
		if err := sqlMigration("011_00.sql")(ctx, tx); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,color FROM accounts ORDER BY created_at ASC,id ASC")
	if err != nil {
		return err
	}
	type account struct {
		id    string
		color sql.NullString
	}
	var accounts []account
	used := map[string]bool{}
	for rows.Next() {
		var a account
		if err := rows.Scan(&a.id, &a.color); err != nil {
			_ = rows.Close()
			return err
		}
		accounts = append(accounts, a)
		if validColor(a.color.String) {
			used[strings.ToLower(a.color.String)] = true
		}
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	for _, a := range accounts {
		if validColor(a.color.String) {
			continue
		}
		selected := derivedColor(a.id)
		for _, candidate := range accountColorPresets {
			if !used[candidate] {
				selected = candidate
				break
			}
		}
		used[selected] = true
		if _, err := tx.ExecContext(ctx, "UPDATE accounts SET color=? WHERE id=?", selected, a.id); err != nil {
			return err
		}
	}
	return nil
}

func migrateV12(ctx context.Context, tx *sql.Tx) error {
	var definition string
	if err := tx.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name='accounts'").Scan(&definition); err != nil {
		return err
	}
	if !strings.Contains(definition, "'pop3'") {
		for index, column := range []string{"auth_data", "sync_state"} {
			has, err := columnExists(ctx, tx, "accounts", column)
			if err != nil {
				return err
			}
			if !has {
				if err := sqlMigration(fmt.Sprintf("012_%02d.sql", index))(ctx, tx); err != nil {
					return err
				}
			}
		}
		if err := sqlMigration("012_02.sql")(ctx, tx); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("foreign key violation while rebuilding accounts")
	}
	return rows.Err()
}

func migrateV13(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []string{"id", "body_text", "body_html_raw"} {
		has, err := columnExists(ctx, tx, "messages", column)
		if err != nil || !has {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,body_text,body_html_raw FROM messages")
	if err != nil {
		return err
	}
	type snippet struct{ id, value string }
	var values []snippet
	for rows.Next() {
		var id, text, html string
		if err := rows.Scan(&id, &text, &html); err != nil {
			_ = rows.Close()
			return err
		}
		values = append(values, snippet{id, buildSnippet(text, html)})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, item := range values {
		if _, err := tx.ExecContext(ctx, "UPDATE messages SET snippet=? WHERE id=?", item.value, item.id); err != nil {
			return err
		}
	}
	return nil
}

func migrateV14(ctx context.Context, tx *sql.Tx) error {
	return ifTables(func(ctx context.Context, tx *sql.Tx) error {
		if err := sqlMigration("014_00.sql")(ctx, tx); err != nil {
			return err
		}
		hasAccounts, err := tableExists(ctx, tx, "accounts")
		if err != nil {
			return err
		}
		selection := "014_02.sql" // legacy minimal fixtures have no provider table
		if hasAccounts {
			selection = "014_01.sql"
		}
		if err := sqlMigration(selection)(ctx, tx); err != nil {
			return err
		}
		for index, table := range []string{"message_folders", "attachments", "message_labels", "kanban_cards", "snoozed_messages", "search_pending", "pending_mail_ops"} {
			if err := ifTables(sqlMigration(fmt.Sprintf("014_%02d.sql", index+3)), table)(ctx, tx); err != nil {
				return err
			}
		}
		return sqlMigration("014_10.sql")(ctx, tx)
	}, "messages")(ctx, tx)
}

func migrateV16(ctx context.Context, tx *sql.Tx) error {
	return ifTables(func(ctx context.Context, tx *sql.Tx) error {
		if err := sqlMigration("016_00.sql")(ctx, tx); err != nil {
			return err
		}
		hasAccounts, err := tableExists(ctx, tx, "accounts")
		if err != nil {
			return err
		}
		hasFolders, err := tableExists(ctx, tx, "message_folders")
		if err != nil {
			return err
		}
		if hasAccounts && hasFolders {
			if err := sqlMigration("016_01.sql")(ctx, tx); err != nil {
				return err
			}
			for index, table := range []string{"message_folders", "attachments", "message_labels", "kanban_cards", "snoozed_messages", "search_pending", "pending_mail_ops"} {
				if err := ifTables(sqlMigration(fmt.Sprintf("016_%02d.sql", index+2)), table)(ctx, tx); err != nil {
					return err
				}
			}
			if err := sqlMigration("016_09.sql", "016_10.sql")(ctx, tx); err != nil {
				return err
			}
		} else if err := sqlMigration("016_11.sql")(ctx, tx); err != nil {
			return err
		}
		return sqlMigration("016_12.sql")(ctx, tx)
	}, "messages")(ctx, tx)
}

func migrateV20(ctx context.Context, tx *sql.Tx) error {
	return ifTables(func(ctx context.Context, tx *sql.Tx) error {
		if err := sqlMigration("020_00.sql")(ctx, tx); err != nil {
			return err
		}
		hasCreated, err := columnExists(ctx, tx, "accounts", "created_at")
		if err != nil {
			return err
		}
		order := "id ASC"
		if hasCreated {
			order = "created_at ASC, id ASC"
		}
		rows, err := tx.QueryContext(ctx, "SELECT id FROM accounts ORDER BY "+order)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for position, id := range ids {
			if _, err := tx.ExecContext(ctx, "UPDATE accounts SET sort_order=? WHERE id=?", position, id); err != nil {
				return err
			}
		}
		return nil
	}, "accounts")(ctx, tx)
}
