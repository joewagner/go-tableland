package impl

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v2"
	"github.com/textileio/go-tableland/pkg/parsing"
)

var (
	errEmptyNode          = errors.New("empty node")
	errUnexpectedNodeType = errors.New("unexpected node type")
)

// QueryValidator enforces PostgresSQL constraints for Tableland.
type QueryValidator struct {
	systemTablePrefix  string
	acceptedTypesNames []string
}

var _ parsing.SQLValidator = (*QueryValidator)(nil)

// New returns a Tableland query validator.
func New(systemTablePrefix string) *QueryValidator {
	// We create here a flattened slice of all the accepted type names from
	// the parsing.AcceptedTypes source of truth. We do this since having a
	// slice is easier and faster to do checks.
	var acceptedTypesNames []string
	for _, at := range parsing.AcceptedTypes {
		acceptedTypesNames = append(acceptedTypesNames, at.Names...)
	}

	return &QueryValidator{
		systemTablePrefix:  systemTablePrefix,
		acceptedTypesNames: acceptedTypesNames,
	}
}

// TODO(jsign): rename to "Parse..."
// ValidateCreateTable validates the provided query and returns an error
// if the CREATE statement isn't allowed. Returns nil otherwise.
func (pp *QueryValidator) ValidateCreateTable(query string) (parsing.CreateStmt, error) {
	parsed, err := pg_query.Parse(query)
	if err != nil {
		return nil, &parsing.ErrInvalidSyntax{InternalError: err}
	}

	if err := checkNonEmptyStatement(parsed); err != nil {
		return nil, fmt.Errorf("empty-statement check: %w", err)
	}

	if err := checkSingleStatement(parsed); err != nil {
		return nil, fmt.Errorf("single-statement check: %w", err)
	}

	stmt := parsed.Stmts[0].Stmt
	if err := checkTopLevelCreate(stmt); err != nil {
		return nil, fmt.Errorf("allowed top level stmt: %w", err)
	}

	colNameTypes, err := checkCreateColTypes(stmt.GetCreateStmt(), pp.acceptedTypesNames)
	if err != nil {
		return nil, fmt.Errorf("disallowed column types: %w", err)
	}

	createStmt, err := genCreateStmt(stmt, colNameTypes)
	if err != nil {
		return nil, fmt.Errorf("generating structured create statement: %s", err)
	}

	return createStmt, nil
}

// ValidateRunSQL validates the query and returns an error if isn't allowed.
// If the query validates correctly, it returns the query type and nil.
func (pp *QueryValidator) ValidateRunSQL(query string) (parsing.TableID, parsing.ReadStmt, []parsing.WriteStmt, error) {
	parsed, err := pg_query.Parse(query)
	if err != nil {
		return parsing.UndefinedQuery, nil, &parsing.ErrInvalidSyntax{InternalError: err}
	}

	if err := checkNonEmptyStatement(parsed); err != nil {
		return parsing.UndefinedQuery, nil, fmt.Errorf("empty-statement check: %w", err)
	}

	stmt := parsed.Stmts[0].Stmt

	// If we detect a read-query, do read-query validation.
	if selectStmt := stmt.GetSelectStmt(); selectStmt != nil {
		if err := checkSingleStatement(parsed); err != nil {
			return parsing.UndefinedQuery, nil, fmt.Errorf("single-statement check: %w", err)
		}

		if err := validateReadQuery(stmt); err != nil {
			return parsing.UndefinedQuery, nil, fmt.Errorf("validating read-query: %w", err)
		}
		return parsing.ReadQuery, nil, nil
	}

	// Otherwise, do a write-query validation.
	writeStmts := make([]parsing.WriteStmt, len(parsed.Stmts))
	var targetTable string
	for i := range parsed.Stmts {
		refTable, err := pp.validateWriteQuery(parsed.Stmts[i].Stmt)
		if err != nil {
			return parsing.UndefinedQuery, nil, fmt.Errorf("validating write-query: %w", err)
		}

		// 1. Check that all statements reference the same table.
		if targetTable == "" {
			targetTable = refTable
		} else if targetTable != refTable {
			return parsing.UndefinedQuery, nil, &parsing.ErrMultiTableReference{Ref1: targetTable, Ref2: refTable}
		}

		// 2. Regenerate raw-queries from parsed tree.
		parsedTree := &pg_query.ParseResult{}
		parsedTree.Stmts = []*pg_query.RawStmt{parsed.Stmts[i]}
		wq, err := pg_query.Deparse(parsedTree)
		if err != nil {
			return parsing.UndefinedQuery, nil, fmt.Errorf("deparsing statement: %s", err)
		}
		writeStmts[i] = &writeStmt{rawQuery: wq, tableName: targetTable}
	}

	return parsing.WriteQuery, writeStmts, nil
}

type writeStmt struct {
	rawQuery  string
	tableName string
}

var _ parsing.WriteStmt = (*writeStmt)(nil)

func (ws *writeStmt) GetRawQuery() string {
	return ws.rawQuery
}
func (ws *writeStmt) GetTablename() string {
	return ws.tableName
}

func (pp *QueryValidator) validateWriteQuery(stmt *pg_query.Node) (string, error) {
	if err := checkTopLevelUpdateInsertDelete(stmt); err != nil {
		return "", fmt.Errorf("allowed top level stmt: %w", err)
	}

	if err := checkNoJoinOrSubquery(stmt); err != nil {
		return "", fmt.Errorf("join or subquery check: %w", err)
	}

	if err := checkNoReturningClause(stmt); err != nil {
		return "", fmt.Errorf("no returning clause check: %w", err)
	}

	if err := checkNoSystemTablesReferencing(stmt, pp.systemTablePrefix); err != nil {
		return "", fmt.Errorf("no system-table reference: %w", err)
	}

	if err := checkNonDeterministicFunctions(stmt); err != nil {
		return "", fmt.Errorf("no non-deterministic func check: %w", err)
	}

	referencedTable, err := getReferencedTable(stmt)
	if err != nil {
		return "", fmt.Errorf("get referenced table: %w", err)
	}

	return referencedTable, nil
}

func validateReadQuery(node *pg_query.Node) error {
	selectStmt := node.GetSelectStmt()

	if err := checkNoJoinOrSubquery(selectStmt.WhereClause); err != nil {
		return fmt.Errorf("join or subquery in where: %w", err)
	}
	for _, n := range selectStmt.TargetList {
		if err := checkNoJoinOrSubquery(n); err != nil {
			return fmt.Errorf("join or subquery in cols: %w", err)
		}
	}
	for _, n := range selectStmt.FromClause {
		if err := checkNoJoinOrSubquery(n); err != nil {
			return fmt.Errorf("join or subquery in from: %w", err)
		}
	}

	if err := checkNoForUpdateOrShare(selectStmt); err != nil {
		return fmt.Errorf("no for check: %w", err)
	}

	return nil
}

func checkNonEmptyStatement(parsed *pg_query.ParseResult) error {
	if len(parsed.Stmts) == 0 {
		return &parsing.ErrEmptyStatement{}
	}
	return nil
}

func checkSingleStatement(parsed *pg_query.ParseResult) error {
	if len(parsed.Stmts) != 1 {
		return &parsing.ErrNoSingleStatement{}
	}
	return nil
}

func checkTopLevelUpdateInsertDelete(node *pg_query.Node) error {
	if node.GetUpdateStmt() == nil &&
		node.GetInsertStmt() == nil &&
		node.GetDeleteStmt() == nil {
		return &parsing.ErrNoTopLevelUpdateInsertDelete{}
	}
	return nil
}

func checkTopLevelCreate(node *pg_query.Node) error {
	if node.GetCreateStmt() == nil {
		return &parsing.ErrNoTopLevelCreate{}
	}
	return nil
}

func checkNoForUpdateOrShare(node *pg_query.SelectStmt) error {
	if node == nil {
		return errEmptyNode
	}

	if len(node.LockingClause) > 0 {
		return &parsing.ErrNoForUpdateOrShare{}
	}
	return nil
}

func checkNoReturningClause(node *pg_query.Node) error {
	if node == nil {
		return errEmptyNode
	}

	if updateStmt := node.GetUpdateStmt(); updateStmt != nil {
		if len(updateStmt.ReturningList) > 0 {
			return &parsing.ErrReturningClause{}
		}
	} else if insertStmt := node.GetInsertStmt(); insertStmt != nil {
		if len(insertStmt.ReturningList) > 0 {
			return &parsing.ErrReturningClause{}
		}
	} else if deleteStmt := node.GetDeleteStmt(); deleteStmt != nil {
		if len(deleteStmt.ReturningList) > 0 {
			return &parsing.ErrReturningClause{}
		}
	} else {
		return errUnexpectedNodeType
	}
	return nil
}

func checkNoSystemTablesReferencing(node *pg_query.Node, systemTablePrefix string) error {
	if node == nil {
		return nil
	}
	if rangeVar := node.GetRangeVar(); rangeVar != nil {
		if strings.HasPrefix(rangeVar.Relname, systemTablePrefix) {
			return &parsing.ErrSystemTableReferencing{}
		}
	} else if insertStmt := node.GetInsertStmt(); insertStmt != nil {
		if strings.HasPrefix(insertStmt.Relation.Relname, systemTablePrefix) {
			return &parsing.ErrSystemTableReferencing{}
		}
		return checkNoSystemTablesReferencing(insertStmt.SelectStmt, systemTablePrefix)
	} else if selectStmt := node.GetSelectStmt(); selectStmt != nil {
		for _, fcn := range selectStmt.FromClause {
			if err := checkNoSystemTablesReferencing(fcn, systemTablePrefix); err != nil {
				return fmt.Errorf("from clause: %w", err)
			}
		}
	} else if updateStmt := node.GetUpdateStmt(); updateStmt != nil {
		if strings.HasPrefix(updateStmt.Relation.Relname, systemTablePrefix) {
			return &parsing.ErrSystemTableReferencing{}
		}
		for _, fcn := range updateStmt.FromClause {
			if err := checkNoSystemTablesReferencing(fcn, systemTablePrefix); err != nil {
				return fmt.Errorf("from clause: %w", err)
			}
		}
	} else if deleteStmt := node.GetDeleteStmt(); deleteStmt != nil {
		if strings.HasPrefix(deleteStmt.Relation.Relname, systemTablePrefix) {
			return &parsing.ErrSystemTableReferencing{}
		}
		if err := checkNoSystemTablesReferencing(deleteStmt.WhereClause, systemTablePrefix); err != nil {
			return fmt.Errorf("where clause: %w", err)
		}
	} else if rangeSubselectStmt := node.GetRangeSubselect(); rangeSubselectStmt != nil {
		if err := checkNoSystemTablesReferencing(rangeSubselectStmt.Subquery, systemTablePrefix); err != nil {
			return fmt.Errorf("subquery: %w", err)
		}
	} else if joinExpr := node.GetJoinExpr(); joinExpr != nil {
		if err := checkNoSystemTablesReferencing(joinExpr.Larg, systemTablePrefix); err != nil {
			return fmt.Errorf("join left arg: %w", err)
		}
		if err := checkNoSystemTablesReferencing(joinExpr.Rarg, systemTablePrefix); err != nil {
			return fmt.Errorf("join right arg: %w", err)
		}
	}
	return nil
}

func getReferencedTable(node *pg_query.Node) (string, error) {
	if insertStmt := node.GetInsertStmt(); insertStmt != nil {
		return insertStmt.Relation.Relname, nil
	} else if updateStmt := node.GetUpdateStmt(); updateStmt != nil {
		return updateStmt.Relation.Relname, nil
	} else if deleteStmt := node.GetDeleteStmt(); deleteStmt != nil {
		return deleteStmt.Relation.Relname, nil
	}
	return "", fmt.Errorf("the statement isn't an insert/update/delete")
}

// checkNonDeterministicFunctions walks the query tree and disallow references to
// functions that aren't deterministic.
func checkNonDeterministicFunctions(node *pg_query.Node) error {
	if node == nil {
		return nil
	}
	if sqlValFunc := node.GetSqlvalueFunction(); sqlValFunc != nil {
		return &parsing.ErrNonDeterministicFunction{}
	} else if listStmt := node.GetList(); listStmt != nil {
		for _, item := range listStmt.Items {
			if err := checkNonDeterministicFunctions(item); err != nil {
				return fmt.Errorf("list item: %w", err)
			}
		}
	}
	if insertStmt := node.GetInsertStmt(); insertStmt != nil {
		return checkNonDeterministicFunctions(insertStmt.SelectStmt)
	} else if selectStmt := node.GetSelectStmt(); selectStmt != nil {
		for _, nl := range selectStmt.ValuesLists {
			if err := checkNonDeterministicFunctions(nl); err != nil {
				return fmt.Errorf("value list: %w", err)
			}
		}
		for _, fcn := range selectStmt.FromClause {
			if err := checkNonDeterministicFunctions(fcn); err != nil {
				return fmt.Errorf("from: %w", err)
			}
		}
	} else if updateStmt := node.GetUpdateStmt(); updateStmt != nil {
		for _, t := range updateStmt.TargetList {
			if err := checkNonDeterministicFunctions(t); err != nil {
				return fmt.Errorf("target: %w", err)
			}
		}
		for _, fcn := range updateStmt.FromClause {
			if err := checkNonDeterministicFunctions(fcn); err != nil {
				return fmt.Errorf("from clause: %w", err)
			}
		}
		if err := checkNonDeterministicFunctions(updateStmt.WhereClause); err != nil {
			return fmt.Errorf("where clause: %w", err)
		}
	} else if deleteStmt := node.GetDeleteStmt(); deleteStmt != nil {
		if err := checkNonDeterministicFunctions(deleteStmt.WhereClause); err != nil {
			return fmt.Errorf("where clause: %w", err)
		}
	} else if rangeSubselectStmt := node.GetRangeSubselect(); rangeSubselectStmt != nil {
		if err := checkNonDeterministicFunctions(rangeSubselectStmt.Subquery); err != nil {
			return fmt.Errorf("subquery: %w", err)
		}
	} else if joinExpr := node.GetJoinExpr(); joinExpr != nil {
		if err := checkNonDeterministicFunctions(joinExpr.Larg); err != nil {
			return fmt.Errorf("join left tree: %w", err)
		}
		if err := checkNonDeterministicFunctions(joinExpr.Rarg); err != nil {
			return fmt.Errorf("join right tree: %w", err)
		}
	} else if aExpr := node.GetAExpr(); aExpr != nil {
		if err := checkNonDeterministicFunctions(aExpr.Lexpr); err != nil {
			return fmt.Errorf("aexpr left: %w", err)
		}
		if err := checkNonDeterministicFunctions(aExpr.Rexpr); err != nil {
			return fmt.Errorf("aexpr right: %w", err)
		}
	} else if resTarget := node.GetResTarget(); resTarget != nil {
		if err := checkNonDeterministicFunctions(resTarget.Val); err != nil {
			return fmt.Errorf("target: %w", err)
		}
	}
	return nil
}

func checkNoJoinOrSubquery(node *pg_query.Node) error {
	if node == nil {
		return nil
	}

	if resTarget := node.GetResTarget(); resTarget != nil {
		if err := checkNoJoinOrSubquery(resTarget.Val); err != nil {
			return fmt.Errorf("column sub-query: %w", err)
		}
	} else if selectStmt := node.GetSelectStmt(); selectStmt != nil {
		if len(selectStmt.ValuesLists) == 0 {
			return &parsing.ErrJoinOrSubquery{}
		}
	} else if subSelectStmt := node.GetRangeSubselect(); subSelectStmt != nil {
		return &parsing.ErrJoinOrSubquery{}
	} else if joinExpr := node.GetJoinExpr(); joinExpr != nil {
		return &parsing.ErrJoinOrSubquery{}
	} else if insertStmt := node.GetInsertStmt(); insertStmt != nil {
		if err := checkNoJoinOrSubquery(insertStmt.SelectStmt); err != nil {
			return fmt.Errorf("insert select expr: %w", err)
		}
	} else if updateStmt := node.GetUpdateStmt(); updateStmt != nil {
		if len(updateStmt.FromClause) != 0 {
			return &parsing.ErrJoinOrSubquery{}
		}
		if err := checkNoJoinOrSubquery(updateStmt.WhereClause); err != nil {
			return fmt.Errorf("where clause: %w", err)
		}
	} else if deleteStmt := node.GetDeleteStmt(); deleteStmt != nil {
		if err := checkNoJoinOrSubquery(deleteStmt.WhereClause); err != nil {
			return fmt.Errorf("where clause: %w", err)
		}
	} else if aExpr := node.GetAExpr(); aExpr != nil {
		if err := checkNoJoinOrSubquery(aExpr.Lexpr); err != nil {
			return fmt.Errorf("aexpr left: %w", err)
		}
		if err := checkNoJoinOrSubquery(aExpr.Rexpr); err != nil {
			return fmt.Errorf("aexpr right: %w", err)
		}
	} else if subLinkExpr := node.GetSubLink(); subLinkExpr != nil {
		return &parsing.ErrJoinOrSubquery{}
	} else if boolExpr := node.GetBoolExpr(); boolExpr != nil {
		for _, arg := range boolExpr.Args {
			if err := checkNoJoinOrSubquery(arg); err != nil {
				return fmt.Errorf("bool expr: %w", err)
			}
		}
	}
	return nil
}

type colNameType struct {
	colName  string
	typeName string
}

func checkCreateColTypes(createStmt *pg_query.CreateStmt, acceptedTypesNames []string) ([]colNameType, error) {
	if createStmt == nil {
		return nil, errEmptyNode
	}

	if createStmt.OfTypename != nil {
		// This will only ever be one, otherwise its a parsing error
		for _, nameNode := range createStmt.OfTypename.Names {
			if name := nameNode.GetString_(); name == nil {
				return nil, fmt.Errorf("unexpected type name node: %v", name)
			}
		}
	}

	var colNameTypes []colNameType
	for _, col := range createStmt.TableElts {
		if colConst := col.GetConstraint(); colConst != nil {
			continue
		}
		colDef := col.GetColumnDef()
		if colDef == nil {
			return nil, errors.New("unexpected node type in column definition")
		}

		var typeName string
	AcceptedTypesFor:
		for _, nameNode := range colDef.TypeName.Names {
			name := nameNode.GetString_()
			if name == nil {
				return nil, fmt.Errorf("unexpected type name node: %v", name)
			}
			// We skip `pg_catalog` since it seems that gets included for some
			// cases of native types.
			if name.Str == "pg_catalog" {
				continue
			}

			for _, atn := range acceptedTypesNames {
				if name.Str == atn {
					typeName = atn
					// The current data type name has a match with accepted
					// types. Continue matching the rest of columns.
					break AcceptedTypesFor
				}
			}

			return nil, &parsing.ErrInvalidColumnType{ColumnType: name.Str}
		}

		colNameTypes = append(colNameTypes, colNameType{colName: colDef.Colname, typeName: typeName})
	}

	return colNameTypes, nil
}

func genCreateStmt(cNode *pg_query.Node, cols []colNameType) (*createStmt, error) {
	strCols := make([]string, len(cols))
	for i := range cols {
		strCols[i] = fmt.Sprintf("%s:%s", cols[i].colName, cols[i].typeName)
	}
	stringifiedColDef := strings.Join(strCols, ",")
	sh := sha256.New()
	sh.Write([]byte(stringifiedColDef))
	hash := sh.Sum(nil)

	return &createStmt{
		cNode:         cNode,
		structureHash: hex.EncodeToString(hash),
		namePrefix:    cNode.GetCreateStmt().Relation.Relname,
	}, nil
}

type createStmt struct {
	cNode         *pg_query.Node
	structureHash string
	namePrefix    string
}

var _ parsing.CreateStmt = (*createStmt)(nil)

func (cs *createStmt) GetRawQueryForTableID(id parsing.TableID) (string, error) {
	parsedTree := &pg_query.ParseResult{}

	cs.cNode.GetCreateStmt().Relation.Relname = "t" + fmt.Sprintf("0x%016x", id)
	parsedTree.Stmts = []*pg_query.RawStmt{&pg_query.RawStmt{Stmt: cs.cNode}}
	wq, err := pg_query.Deparse(parsedTree)
	if err != nil {
		return "", fmt.Errorf("deparsing statement: %s", err)
	}
	return wq, nil
}
func (cs *createStmt) GetStructureHash() string {
	return cs.structureHash
}
func (cs *createStmt) GetNamePrefix() string {
	return cs.namePrefix
}
