package user

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/textileio/go-tableland/internal/tableland"
	"github.com/textileio/go-tableland/pkg/parsing"
)

// UserStore provides access to the db store.
type UserStore struct {
	pool    *pgxpool.Pool
	chainID tableland.ChainID
}

// New creates a new UserStore.
func New(pool *pgxpool.Pool, chainID tableland.ChainID) *UserStore {
	return &UserStore{
		pool:    pool,
		chainID: chainID,
	}
}

// Read executes a read statement on the db.
func (db *UserStore) Read(ctx context.Context, rq parsing.SugaredReadStmt) (interface{}, error) {
	var ret interface{}
	f := func(tx pgx.Tx) error {
		desugared, err := rq.GetDesugaredQuery()
		if err != nil {
			return fmt.Errorf("get desugared query: %s", err)
		}
		ret, err = execReadQuery(ctx, tx, desugared)
		if err != nil {
			return fmt.Errorf("parsing result to json: %s", err)
		}
		return nil
	}
	if err := db.pool.BeginFunc(ctx, f); err != nil {
		return nil, fmt.Errorf("running nested txn: %s", err)
	}
	return ret, nil
}

func execReadQuery(ctx context.Context, tx pgx.Tx, q string) (interface{}, error) {
	rows, err := tx.Query(ctx, q, pgx.QuerySimpleProtocol(true))
	if err != nil {
		return nil, fmt.Errorf("executing query: %s", err)
	}
	defer rows.Close()
	return rowsToJSON(rows)
}
