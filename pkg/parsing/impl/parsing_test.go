package impl_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/textileio/go-tableland/pkg/parsing"
	postgresparser "github.com/textileio/go-tableland/pkg/parsing/impl"
)

func TestRunSQL(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name            string
		query           string
		expectedErrType interface{}
		queryType       parsing.QueryType
	}

	writeQueryTests := []testCase{
		// Malformed query.
		{name: "malformed insert", query: "insert into foo valuez (1, 1)", expectedErrType: ptr2ErrInvalidSyntax()},
		{name: "malformed update", query: "update foo sez a=1, b=2", expectedErrType: ptr2ErrInvalidSyntax()},
		{name: "malformed delete", query: "delete fromz foo where a=2", expectedErrType: ptr2ErrInvalidSyntax()},

		// Valid insert and updates.
		{name: "valid insert", query: "insert into foo values ('hello', 1, 2)", expectedErrType: nil},
		{name: "valid simple update", query: "update foo set a=1 where b='hello'", expectedErrType: nil},
		{name: "valid delete", query: "delete from foo where a=2", expectedErrType: nil},
		{name: "valid custom func call", query: "insert into foo values (myfunc(1))", expectedErrType: nil},

		// Single-statement check.
		{name: "single statement fail", query: "update foo set a=1; update foo set b=1;", expectedErrType: ptr2ErrNoSingleStatement()},
		{name: "no statements", query: "", expectedErrType: ptr2ErrNoSingleStatement()},

		// Check not allowed top-statements.
		{name: "create", query: "create table foo (bar int)", expectedErrType: ptr2ErrNoTopLevelUpdateInsertDelete()},
		{name: "drop", query: "drop table foo", expectedErrType: ptr2ErrNoTopLevelUpdateInsertDelete()},

		// Disallow JOINs and sub-queries
		{name: "insert subquery", query: "insert into foo select * from bar", expectedErrType: ptr2ErrJoinOrSubquery()},
		{name: "update join", query: "update foo set a=1 from bar", expectedErrType: ptr2ErrJoinOrSubquery()},
		{name: "update where subquery", query: "update foo set a=1 where a=(select a from bar limit 1) and b=1", expectedErrType: ptr2ErrJoinOrSubquery()},
		{name: "delete where subquery", query: "delete from foo where a=(select a from bar limit 1)", expectedErrType: ptr2ErrJoinOrSubquery()},

		// Disallow RETURNING clauses
		{name: "update returning", query: "update foo set a=a+1 returning a", expectedErrType: ptr2ErrReturningClause()},
		{name: "insert returning", query: "insert into foo values (1, 'bar') returning a", expectedErrType: ptr2ErrReturningClause()},
		{name: "delete returning", query: "delete from foo where a=1 returning b", expectedErrType: ptr2ErrReturningClause()},

		// Check no system-tables references.
		{name: "update system table", query: "update system_tables set a=1", expectedErrType: ptr2ErrSystemTableReferencing()},
		{name: "insert system table", query: "insert into system_tables values ('foo')", expectedErrType: ptr2ErrSystemTableReferencing()},
		{name: "delete system table", query: "delete from system_tables", expectedErrType: ptr2ErrSystemTableReferencing()},

		// Check non-deterministic functions.
		{name: "insert current_timestamp lower", query: "insert into foo values (current_timestamp, 'lolz')", expectedErrType: ptr2ErrNonDeterministicFunction()},
		{name: "insert current_timestamp case-insensitive", query: "insert into foo values (current_TiMeSTamP, 'lolz')", expectedErrType: ptr2ErrNonDeterministicFunction()},
		{name: "update set current_timestamp", query: "update foo set a=current_timestamp, b=2", expectedErrType: ptr2ErrNonDeterministicFunction()},
		{name: "update where current_timestamp", query: "update foo set a=1 where b=current_timestamp", expectedErrType: ptr2ErrNonDeterministicFunction()},
		{name: "delete where current_timestamp", query: "delete from foo where a=current_timestamp", expectedErrType: ptr2ErrNonDeterministicFunction()},
	}
	for i := range writeQueryTests {
		writeQueryTests[i].queryType = parsing.WriteQuery
	}

	readQueryTests := []testCase{
		// Malformed query.
		{name: "malformed query", query: "shelect * from foo", expectedErrType: ptr2ErrInvalidSyntax()},

		// Valid read-queries.
		{name: "valid all", query: "select * from foo", expectedErrType: nil},
		{name: "valid defined rows", query: "select row1, row2 from foo", expectedErrType: nil},
		{name: "valid join with subquery", query: "select * from foo inner join bar on a=b inner join (select * from zoo) z on a=b", expectedErrType: nil},

		// Single-statement check.
		{name: "single statement fail", query: "select * from foo; select * from bar", expectedErrType: ptr2ErrNoSingleStatement()},
		{name: "no statements", query: "", expectedErrType: ptr2ErrNoSingleStatement()},

		// Check no FROM SHARE/UPDATE
		{name: "for share", query: "select * from foo for share", expectedErrType: ptr2ErrNoForUpdateOrShare()},
		{name: "for update", query: "select * from foo for update", expectedErrType: ptr2ErrNoForUpdateOrShare()},

		// Check no system-tables references.
		{name: "reference system table", query: "select * from system_tables", expectedErrType: ptr2ErrSystemTableReferencing()},
		{name: "reference system table with inner join", query: "select * from foo inner join system_tables on a=b", expectedErrType: ptr2ErrSystemTableReferencing()},
		{name: "reference system table in nested FROM SELECT", query: "select * from foo inner join (select * from system_tables) j on a=b", expectedErrType: ptr2ErrSystemTableReferencing()},
	}
	for i := range readQueryTests {
		readQueryTests[i].queryType = parsing.ReadQuery
	}

	tests := append(readQueryTests, writeQueryTests...)

	for _, it := range tests {
		t.Run(fmt.Sprintf("%s/%s", it.queryType, it.name), func(tc testCase) func(t *testing.T) {
			return func(t *testing.T) {
				t.Parallel()
				parser := postgresparser.New("system_")
				qt, err := parser.ValidateRunSQL(tc.query)
				if tc.expectedErrType == nil {
					require.NoError(t, err)
					require.Equal(t, tc.queryType, qt)
					return
				}
				require.ErrorAs(t, err, tc.expectedErrType)
			}
		}(it))
	}
}

func TestCreateTable(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name            string
		query           string
		expectedErrType interface{}
	}
	tests := []testCase{
		// Malformed query.
		{name: "malformed query", query: "create tablez foo (foo int)", expectedErrType: ptr2ErrInvalidSyntax()},

		// Single-statement check.
		{name: "two creates", query: "create table foo (a int); create table bar (a int);", expectedErrType: ptr2ErrNoSingleStatement()},
		{name: "no statements", query: "", expectedErrType: ptr2ErrNoSingleStatement()},

		// Check top-statement is only CREATE.
		{name: "select", query: "select * from foo", expectedErrType: ptr2ErrNoTopLevelCreate()},
		{name: "update", query: "update foo set bar=1", expectedErrType: ptr2ErrNoTopLevelCreate()},
		{name: "insert", query: "insert into foo values (1)", expectedErrType: ptr2ErrNoTopLevelCreate()},
		{name: "drop", query: "drop table foo", expectedErrType: ptr2ErrNoTopLevelCreate()},
		{name: "delete", query: "delete from foo", expectedErrType: ptr2ErrNoTopLevelCreate()},

		// Valid table with all accepted types.
		{
			name: "valid all",
			query: `create table foo (
				   zint  int,
				   zint2 int2,
				   zint4 int4,
				   zint8 int8,
				   zbigint bigint,
				   zsmallint smallint,

				   ztext text,
				   zvarchar varchar(10),
				   zbpchar bpchar,
				   zdate date,

				   zbool bool,

				   zfloat4 float4,
				   zfloat8 float8,

				   znumeric numeric,

				   ztimestamp timestamp,
				   ztimestamptz timestamptz,
				   zuuid uuid
			       )`,
			expectedErrType: nil,
		},

		// Tables with invalid columns.
		{name: "xml column", query: "create table foo (foo xml)", expectedErrType: ptr2ErrInvalidColumnType()},
		{name: "money column", query: "create table foo (foo money)", expectedErrType: ptr2ErrInvalidColumnType()},
		{name: "polygon column", query: "create table foo (foo polygon)", expectedErrType: ptr2ErrInvalidColumnType()},
	}

	for _, it := range tests {
		t.Run(it.name, func(tc testCase) func(t *testing.T) {
			return func(t *testing.T) {
				t.Parallel()
				parser := postgresparser.New("system_")
				err := parser.ValidateCreateTable(tc.query)
				if tc.expectedErrType == nil {
					require.NoError(t, err)
					return
				}
				require.ErrorAs(t, err, tc.expectedErrType)
			}
		}(it))
	}
}

// Helpers to have a pointer to pointer for generic test-case running.
func ptr2ErrInvalidSyntax() **parsing.ErrInvalidSyntax {
	var e *parsing.ErrInvalidSyntax
	return &e
}
func ptr2ErrNoSingleStatement() **parsing.ErrNoSingleStatement {
	var e *parsing.ErrNoSingleStatement
	return &e
}
func ptr2ErrNoTopLevelSelect() **parsing.ErrNoTopLevelSelect {
	var e *parsing.ErrNoTopLevelSelect
	return &e
}
func ptr2ErrNoForUpdateOrShare() **parsing.ErrNoForUpdateOrShare {
	var e *parsing.ErrNoForUpdateOrShare
	return &e
}
func ptr2ErrSystemTableReferencing() **parsing.ErrSystemTableReferencing {
	var e *parsing.ErrSystemTableReferencing
	return &e
}
func ptr2ErrNoTopLevelUpdateInsertDelete() **parsing.ErrNoTopLevelUpdateInsertDelete {
	var e *parsing.ErrNoTopLevelUpdateInsertDelete
	return &e
}
func ptr2ErrReturningClause() **parsing.ErrReturningClause {
	var e *parsing.ErrReturningClause
	return &e
}
func ptr2ErrNonDeterministicFunction() **parsing.ErrNonDeterministicFunction {
	var e *parsing.ErrNonDeterministicFunction
	return &e
}
func ptr2ErrJoinOrSubquery() **parsing.ErrJoinOrSubquery {
	var e *parsing.ErrJoinOrSubquery
	return &e
}
func ptr2ErrNoTopLevelCreate() **parsing.ErrNoTopLevelCreate {
	var e *parsing.ErrNoTopLevelCreate
	return &e
}
func ptr2ErrInvalidColumnType() **parsing.ErrInvalidColumnType {
	var e *parsing.ErrInvalidColumnType
	return &e
}
