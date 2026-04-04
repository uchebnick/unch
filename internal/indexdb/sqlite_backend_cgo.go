//go:build cgo

package indexdb

import (
	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

func registerSQLiteVec() {
	sqlite_vec.Auto()
}

func serializeVector(vector []float32) ([]byte, error) {
	return sqlite_vec.SerializeFloat32(vector)
}
