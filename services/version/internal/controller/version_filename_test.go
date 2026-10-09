package controller

import "testing"

func TestValidateVersionFileName(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		wantErr  bool
	}{
		{name: "generated module tar.gz", fileName: "0190d5a4-7c2e-7b1a-9f3d-2a4b6c8d0e1f.tar.gz"},
		{name: "generated module zip", fileName: "0190d5a4-7c2e-7b1a-9f3d-2a4b6c8d0e1f.zip"},
		{name: "user supplied plain name", fileName: "null.zip"},
		{name: "parent traversal", fileName: "../secret.zip", wantErr: true},
		{name: "embedded traversal", fileName: "a/../../secret.zip", wantErr: true},
		{name: "double dot only", fileName: "..", wantErr: true},
		{name: "double dot in name", fileName: "a..b.zip", wantErr: true},
		{name: "forward slash", fileName: "nested/file.zip", wantErr: true},
		{name: "backslash", fileName: `nested\file.zip`, wantErr: true},
		{name: "leading slash", fileName: "/etc/passwd", wantErr: true},
		{name: "leading dot", fileName: ".hidden.zip", wantErr: true},
		{name: "empty", fileName: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateVersionFileName(tt.fileName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateVersionFileName(%q) error = %v, wantErr %v", tt.fileName, err, tt.wantErr)
			}
		})
	}
}
