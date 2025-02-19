package tests

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Sqlite3URI returns a URI to spinup an in-memory Sqlite database.
func Sqlite3URI(t *testing.T) string {
	dbURI := "file::" + uuid.NewString() + ":?mode=memory&cache=shared&_foreign_keys=on"
	db, err := sql.Open("sqlite3", dbURI)
	require.NoError(t, err)
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
		_ = db.Close()
	})

	return dbURI
}
