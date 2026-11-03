// Package jsonutil decodes a single JSON value without losing numeric precision.
package jsonutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Decode accepts an optional UTF-8 BOM and rejects trailing JSON values.
func Decode(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	d.UseNumber()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("JSON の末尾に余分なデータがあります")
	}
	return nil
}
