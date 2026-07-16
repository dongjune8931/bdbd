package main

import "testing"

func TestValidContentMD5(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "valid", value: "1B2M2Y8AsgTpgAmY7PhCfg==", want: true},
		{name: "invalid base64", value: "not-base64", want: false},
		{name: "wrong digest length", value: "c2hvcnQ=", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validContentMD5(test.value); got != test.want {
				t.Fatalf("validContentMD5(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}
