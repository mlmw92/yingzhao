package configdoc

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestIsConflictError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"duplicate key", &mysql.MySQLError{Number: 1062}, true},
		{"deadlock", &mysql.MySQLError{Number: 1213}, true},
		{"wrapped deadlock", fmt.Errorf("insert: %w", &mysql.MySQLError{Number: 1213}), true},
		{"table missing", &mysql.MySQLError{Number: 1146}, false},
		{"plain", errors.New("boom"), false},
		{"nil", nil, false},
	} {
		if got := isConflictError(tc.err); got != tc.want {
			t.Fatalf("%s: isConflictError = %v, want %v", tc.name, got, tc.want)
		}
	}
}
