package tuck

import (
	"database/sql"
	_ "embed"
	"reflect"
	"strings"
	"sync"
)

const databaseID = 0x5455434b
const databaseVersion = 4

// Released SQL is immutable. New versions append a numbered step. The same
// chain initializes a fresh store and upgrades an existing store.
//
//go:embed contract/storage/001.sql
var schemaV1 string

//go:embed contract/storage/002.sql
var schemaV2 string

//go:embed contract/storage/003.sql
var schemaV3 string

//go:embed contract/storage/004.sql
var schemaV4 string

var schemaShapes struct {
	sync.Once
	values map[int]map[string]string
	err    error
}

type schemaReader interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func readSchema(q schemaReader) (map[string]string, error) {
	rows, err := q.Query("SELECT type,name,sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shape := map[string]string{}
	for rows.Next() {
		var kind, name, ddl string
		if err = rows.Scan(&kind, &name, &ddl); err != nil {
			return nil, err
		}
		shape[kind+":"+name] = strings.Join(strings.Fields(ddl), " ")
	}
	return shape, rows.Err()
}

func expectedSchemas() (map[int]map[string]string, error) {
	schemaShapes.Do(func() {
		db, err := openDatabase(":memory:", "rwc")
		if err != nil {
			schemaShapes.err = err
			return
		}
		defer db.Close()
		schemaShapes.values = map[int]map[string]string{}
		for i, ddl := range []string{schemaV1, schemaV2, schemaV3, schemaV4} {
			if _, err = db.Exec(ddl); err == nil {
				schemaShapes.values[i+1], err = readSchema(db)
			}
			if err != nil {
				schemaShapes.err = err
				return
			}
		}
	})
	return schemaShapes.values, schemaShapes.err
}

// Validate ownership AND actual DDL before enabling WAL or changing anything.
// Unknown/future/partially modified schemas never enter a migration step.
func inspectDatabase(q schemaReader) (int, error) {
	var version, appID int
	if err := q.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return 0, err
	}
	if err := q.QueryRow("PRAGMA application_id").Scan(&appID); err != nil {
		return 0, err
	}
	actual, err := readSchema(q)
	if err != nil {
		return 0, err
	}
	if version == 0 && appID == 0 && len(actual) == 0 {
		return 0, nil
	}
	expected, err := expectedSchemas()
	if err != nil {
		return 0, err
	}
	if appID != databaseID || expected[version] == nil || !reflect.DeepEqual(actual, expected[version]) {
		return 0, Problem(409, "database_version", "数据库身份、版本或结构不受支持；请保留原数据并使用匹配的程序或备份")
	}
	return version, nil
}
