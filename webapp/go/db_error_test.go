package isureal

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation(t *testing.T) {
	unique := &pgconn.PgError{Code: "23505", ConstraintName: "users_username_key"}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"unique violation", unique, true},
		{"wrapped unique violation", fmt.Errorf("insert user: %w", unique), true},
		{"other pg error", &pgconn.PgError{Code: "23503"}, false},
		{"plain error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := isUniqueViolation(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
