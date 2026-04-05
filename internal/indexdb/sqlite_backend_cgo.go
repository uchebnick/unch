//go:build cgo

package indexdb

import (
	_ "github.com/mattn/go-sqlite3"
	sqlite_vec "github.com/uchebnick/unch/third_party/sqlite-vec-go-bindings/cgo"
)

func registerSQLiteVec() {
	sqlite_vec.Auto()
}

func serializeVector(vector []float32) ([]byte, error) {
	return sqlite_vec.SerializeFloat32(vector)
}
