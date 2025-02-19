package tableland

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/textileio/go-tableland/pkg/tables"
)

// ACL is the API for access control rules check.
type ACL interface {
	// CheckPrivileges checks if an address can execute a specific operation on a table.
	CheckPrivileges(context.Context, *sql.Tx, common.Address, tables.TableID, Operation) (bool, error)
}

// Privilege maps to SQL privilege and is the thing needed to execute an operation.
type Privilege struct {
	Abbreviation string
	Bitfield     int
}

var (
	// PrivInsert allows insert operations to be executed. The abbreviation is "a".
	PrivInsert = Privilege{
		Abbreviation: "a",
		Bitfield:     0b001,
	}

	// PrivUpdate allows updated operations to be executed. The abbreviation is "w".
	PrivUpdate = Privilege{
		Abbreviation: "w",
		Bitfield:     0b010,
	}

	// PrivDelete allows delete operations to be executed. The abbreviation is "d".
	PrivDelete = Privilege{
		Abbreviation: "d",
		Bitfield:     0b100,
	}
)

// NewPrivilegeFromSQLString converts a SQL privilege string into a Privilege.
func NewPrivilegeFromSQLString(s string) (Privilege, error) {
	switch s {
	case "insert":
		return PrivInsert, nil
	case "update":
		return PrivUpdate, nil
	case "delete":
		return PrivDelete, nil
	}

	return Privilege{}, fmt.Errorf("unsupported string=%s", s)
}

// ToSQLString returns the SQL string representation of a Privilege.
func (p Privilege) ToSQLString() string {
	switch p {
	case PrivInsert:
		return "insert"
	case PrivUpdate:
		return "update"
	case PrivDelete:
		return "delete"
	default:
		return "nil"
	}
}

// Operation represents the kind of operation that can by executed in Tableland.
type Operation int

const (
	// OpSelect is represents a SELECT query.
	OpSelect Operation = iota
	// OpInsert is represents a INSERT query.
	OpInsert
	// OpUpdate is represents a UPDATE query.
	OpUpdate
	// OpDelete is represents a DELETE query.
	OpDelete
	// OpGrant is represents a GRANT query.
	OpGrant
	// OpRevoke is represents a REVOKE query.
	OpRevoke
	// OpCreate is represents a CREATE query.
	OpCreate
)

// String returns the string representation of the operation.
func (op Operation) String() string {
	switch op {
	case OpSelect:
		return "OpSelect"
	case OpInsert:
		return "OpInsert"
	case OpUpdate:
		return "OpUpdate"
	case OpDelete:
		return "OpDelete"
	case OpGrant:
		return "OpGrant"
	case OpRevoke:
		return "OpRevoke"
	case OpCreate:
		return "OpCreate"
	}

	return ""
}

var operationPrivilegeMap map[Operation]Privilege

func init() {
	// This map gives the privilege that is needed for each operation.
	// If an operation is not in the map, it means it doesn't need any privilege.
	operationPrivilegeMap = map[Operation]Privilege{
		OpInsert: PrivInsert,
		OpDelete: PrivDelete,
		OpUpdate: PrivUpdate,
	}
}

// Privileges represents a list of privileges.
type Privileges []Privilege

// CanExecute checks if the list of privileges can execute a given operation.
// In case the operation cannot be executed, it returns the privilege that
// would allow the execution.
func (p Privileges) CanExecute(operation Operation) (bool, Privilege) {
	privilegeNeededForOperation, ok := operationPrivilegeMap[operation]
	if !ok {
		return true, Privilege{}
	}
	for _, privilege := range p {
		if privilege == privilegeNeededForOperation {
			return true, Privilege{}
		}
	}
	return false, privilegeNeededForOperation
}

// Policy represents the kinds of restrictions that can be imposed on a statement execution.
type Policy interface {
	// IsInsertAllowed rejects insert statement execution.
	IsInsertAllowed() bool

	// IsUpdateAllowed rejects update statement execution.
	IsUpdateAllowed() bool

	// IsDeleteAllowed rejects delete statement execution.
	IsDeleteAllowed() bool

	// WhereClause is SQL where clauses that restricts update and delete execution.
	WhereClause() string

	// UpdatableColumns imposes restrictions on what columns can be updated.
	// Empty means all columns are allowed.
	UpdatableColumns() []string

	// WithCheck is a SQL where clause that restricts the execution of incoming writes.
	WithCheck() string
}
