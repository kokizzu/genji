package statement

import (
	"github.com/cockroachdb/errors"
	"github.com/genjidb/genji/internal/database"
	errs "github.com/genjidb/genji/internal/errors"
	"github.com/genjidb/genji/internal/stream"
	"github.com/genjidb/genji/internal/stream/index"
	"github.com/genjidb/genji/internal/stream/table"
)

// AlterTableRenameStmt is a DSL that allows creating a full ALTER TABLE query.
type AlterTableRenameStmt struct {
	TableName    string
	NewTableName string
}

// IsReadOnly always returns false. It implements the Statement interface.
func (stmt AlterTableRenameStmt) IsReadOnly() bool {
	return false
}

// Run runs the ALTER TABLE statement in the given transaction.
// It implements the Statement interface.
func (stmt AlterTableRenameStmt) Run(ctx *Context) (Result, error) {
	var res Result

	if stmt.TableName == "" {
		return res, errors.New("missing table name")
	}

	if stmt.NewTableName == "" {
		return res, errors.New("missing new table name")
	}

	if stmt.TableName == stmt.NewTableName {
		return res, errs.AlreadyExistsError{Name: stmt.NewTableName}
	}

	err := ctx.Tx.CatalogWriter().RenameTable(ctx.Tx, stmt.TableName, stmt.NewTableName)
	return res, err
}

type AlterTableAddFieldStmt struct {
	TableName        string
	FieldConstraint  *database.FieldConstraint
	TableConstraints database.TableConstraints
}

// IsReadOnly always returns false. It implements the Statement interface.
func (stmt *AlterTableAddFieldStmt) IsReadOnly() bool {
	return false
}

// Run runs the ALTER TABLE ADD FIELD statement in the given transaction.
// It implements the Statement interface.
// The statement rebuilds the table.
func (stmt *AlterTableAddFieldStmt) Run(ctx *Context) (Result, error) {
	var err error

	// get the table before adding the field constraint
	// and assign the table to the table.Scan operator
	// so that it can decode the records properly
	scan := table.Scan(stmt.TableName)
	scan.Table, err = ctx.Tx.Catalog.GetTable(ctx.Tx, stmt.TableName)
	if err != nil {
		return Result{}, errors.Wrap(err, "failed to get table")
	}

	// get the current list of indexes
	indexNames := ctx.Tx.Catalog.ListIndexes(stmt.TableName)

	// add the field constraint to the table
	err = ctx.Tx.CatalogWriter().AddFieldConstraint(
		ctx.Tx,
		stmt.TableName,
		stmt.FieldConstraint,
		stmt.TableConstraints)
	if err != nil {
		return Result{}, err
	}

	// create a unique index for every unique constraint
	pkAdded := false
	var newIdxs []*database.IndexInfo
	for _, tc := range stmt.TableConstraints {
		if tc.Unique {
			idx, err := ctx.Tx.CatalogWriter().CreateIndex(ctx.Tx, &database.IndexInfo{
				Paths:  tc.Paths,
				Unique: true,
				Owner: database.Owner{
					TableName: stmt.TableName,
					Paths:     tc.Paths,
				},
			})
			if err != nil {
				return Result{}, err
			}

			newIdxs = append(newIdxs, idx)
		}

		if tc.PrimaryKey {
			pkAdded = true
		}
	}

	// create the stream:
	// on one side, scan the table with the old schema
	// on the other side, insert the records into the same table with the new schema
	s := stream.New(scan)

	// if a primary key was added, we need to delete the old records
	// and old indexes, and insert the new records and indexes
	if pkAdded {
		// delete the old records from the indexes
		for _, indexName := range indexNames {
			s = s.Pipe(index.Delete(indexName))
		}
		// delete the old records from the table
		s = s.Pipe(table.Delete(stmt.TableName))

		// validate the record against the new schema
		s = s.Pipe(table.Validate(stmt.TableName))

		// insert the record with the new primary key
		s = s.Pipe(table.Insert(stmt.TableName))

		// insert the record into the all the indexes
		indexNames = ctx.Tx.Catalog.ListIndexes(stmt.TableName)
		for _, indexName := range indexNames {
			info, err := ctx.Tx.Catalog.GetIndexInfo(indexName)
			if err != nil {
				return Result{}, err
			}
			if info.Unique {
				s = s.Pipe(index.Validate(indexName))
			}

			s = s.Pipe(index.Insert(indexName))
		}
	} else {
		// otherwise, we can just replace the old records with the new ones

		// validate the record against the new schema
		s = s.Pipe(table.Validate(stmt.TableName))

		// replace the old record with the new one
		s = s.Pipe(table.Replace(stmt.TableName))

		// update the new indexes only
		for _, idx := range newIdxs {
			if idx.Unique {
				s = s.Pipe(index.Validate(idx.IndexName))
			}

			s = s.Pipe(index.Insert(idx.IndexName))
		}
	}

	// ALTER TABLE ADD FIELD does not return any result
	s = s.Pipe(stream.Discard())

	// do NOT optimize the stream
	return Result{
		Iterator: &StreamStmtIterator{
			Stream:  s,
			Context: ctx,
		},
	}, nil
}
