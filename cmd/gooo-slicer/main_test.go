package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRequestRejectsTrailingJSONValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, []byte(`{"scenario":"trailing"} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRequest(path); err == nil {
		t.Fatal("readRequest accepted a trailing JSON value")
	}
}
