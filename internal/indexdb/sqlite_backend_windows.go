//go:build windows && !cgo

package indexdb

import (
	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	_ "github.com/ncruces/go-sqlite3/driver"
)

func registerSQLiteVec() {}

func serializeVector(vector []float32) ([]byte, error) {
	return sqlite_vec.SerializeFloat32(vector)
}
