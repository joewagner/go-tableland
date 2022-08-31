package tableland

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/textileio/go-tableland/pkg/tables"
)

// TxnReceipt is a Tableland event processing receipt.
type TxnReceipt struct {
	ChainID     ChainID `json:"chain_id"`
	TxnHash     string  `json:"txn_hash"`
	BlockNumber int64   `json:"block_number"`

	TableID       *string `json:"table_id,omitempty"`
	Error         string  `json:"error"`
	ErrorEventIdx int     `json:"error_event_idx"`
}

// Tableland defines the interface of Tableland.
type Tableland interface {
	RunReadQuery(ctx context.Context, stmt string) (interface{}, error)
	ValidateCreateTable(ctx context.Context, chainID ChainID, stmt string) (string, error)
	ValidateWriteQuery(ctx context.Context, chainID ChainID, stmt string) (tables.TableID, error)
	RelayWriteQuery(
		ctx context.Context,
		chainID ChainID,
		caller common.Address,
		stmt string,
	) (tables.Transaction, error)
	GetReceipt(ctx context.Context, chainID ChainID, txnHash string) (bool, *TxnReceipt, error)
	SetController(
		ctx context.Context,
		chainID ChainID,
		caller common.Address,
		controller common.Address,
		tableID tables.TableID,
	) (tables.Transaction, error)
}

// ChainID is a supported EVM chain identifier.
type ChainID int64

// Table represents a database table.
type Table struct {
	id      tables.TableID
	prefix  string
	chainID ChainID
}

// ChainID returns table's chain id.
func (t Table) ChainID() ChainID {
	return t.chainID
}

// NewTableFromName creates a Table from its name.
func NewTableFromName(name string) (Table, error) {
	parts := strings.Split(name, "_")

	if len(parts) < 2 {
		return Table{}, errors.New("table name has invalid format")
	}

	tableID, err := tables.NewTableID(parts[len(parts)-1])
	if err != nil {
		return Table{}, fmt.Errorf("new table id: %s", err)
	}

	i, err := strconv.ParseInt(parts[len(parts)-2], 10, 64)
	if err != nil {
		return Table{}, fmt.Errorf("parse chain id: %s", err)
	}

	return Table{
		id:      tableID,
		prefix:  strings.Join(parts[:len(parts)-2], "_"),
		chainID: ChainID(i),
	}, nil
}

// EVMEvent is a Tableland on-chain event produced by the Registry SC.
type EVMEvent struct {
	Address     common.Address
	Topics      []byte
	Data        []byte
	BlockNumber uint64
	TxHash      common.Hash
	TxIndex     uint
	BlockHash   common.Hash
	Index       uint

	// Enhanced fields
	ChainID   ChainID
	EventJSON []byte
	EventType string
}

// EVMBlockInfo contains information about an EVM block.
type EVMBlockInfo struct {
	ChainID     ChainID
	BlockNumber int64
	Timestamp   time.Time
}
