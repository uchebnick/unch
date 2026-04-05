//go:build windows && !cgo

package indexdb

import (
	_ "github.com/ncruces/go-sqlite3/driver"
	sqlite_vec "github.com/uchebnick/unch/third_party/sqlite-vec-go-bindings/ncruces"
)

func registerSQLiteVec() {}

func serializeVector(vector []float32) ([]byte, error) {
	return sqlite_vec.SerializeFloat32(vector)
}
