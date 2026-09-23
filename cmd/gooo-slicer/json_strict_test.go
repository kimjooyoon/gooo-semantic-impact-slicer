package main

import "testing"

func TestStrictJSONUnmarshalRejectsNestedDuplicateKeys(t *testing.T) {
	var value map[string]any
	if err := strictJSONUnmarshal([]byte(`{"outer":{"id":"first","id":"second"}}`), &value); err == nil {
		t.Fatal("expected duplicate JSON key to be rejected")
	}
}

func TestStrictJSONUnmarshalAcceptsUniqueKeys(t *testing.T) {
	var value map[string]any
	if err := strictJSONUnmarshal([]byte(`{"outer":{"id":"first","kind":"semantic"}}`), &value); err != nil {
		t.Fatalf("expected unique JSON keys to be accepted: %v", err)
	}
}
