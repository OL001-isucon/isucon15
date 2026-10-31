package isureal

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// PostgreSQL の一意制約違反 (SQLSTATE 23505) かを返す。
// LOCK TABLE で直列化していた箇所を、制約に任せて競合時だけ 409 に写すために使う
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
