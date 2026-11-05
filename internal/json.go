package internal

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSON stores a Go value as JSON text in SQLite.
type JSON[T any] struct {
	Data T
}

func (j *JSON[T]) Scan(src any) error {
	var data []byte
	switch v := src.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	case nil:
		data = []byte("null")
	default:
		return fmt.Errorf("scan JSON: unsupported type %T", src)
	}
	if err := json.Unmarshal(data, &j.Data); err != nil {
		return fmt.Errorf("scan JSON: %w", err)
	}
	return nil
}

func (j JSON[T]) Value() (driver.Value, error) {
	data, err := json.Marshal(j.Data)
	if err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}
	return string(data), nil
}
