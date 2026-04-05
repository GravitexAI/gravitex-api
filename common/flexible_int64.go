package common

import (
	"bytes"
	"fmt"
	"strconv"
)

// Int64Flexible accepts either a JSON number or a JSON string and stores it as int64.
// This is useful when clients need to send large integers precisely (e.g. JS bigint).
type Int64Flexible int64

func (v *Int64Flexible) UnmarshalJSON(data []byte) error {
	b := bytes.TrimSpace(data)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*v = 0
		return nil
	}

	// Handle quoted numbers: "123"
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		s := string(b[1 : len(b)-1])
		if s == "" {
			*v = 0
			return nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid int64 string %q: %w", s, err)
		}
		*v = Int64Flexible(n)
		return nil
	}

	// Handle numeric tokens: 123
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid int64 number %q: %w", string(b), err)
	}
	*v = Int64Flexible(n)
	return nil
}

func (v Int64Flexible) Int64() int64 {
	return int64(v)
}

func (v Int64Flexible) Int() int {
	return int(v)
}
