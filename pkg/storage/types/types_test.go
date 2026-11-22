package types

import "testing"

func TestStorageMethodString(t *testing.T) {
	tests := map[StorageMethod]string{
		Get:    "Get",
		Delete: "Delete",
		Put:    "Put",
	}

	for method, want := range tests {
		if got := method.String(); got != want {
			t.Errorf("StorageMethod(%d).String() = %q, want %q", int(method), got, want)
		}
	}
}
