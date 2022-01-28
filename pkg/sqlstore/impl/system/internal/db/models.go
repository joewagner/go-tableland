// Code generated by sqlc. DO NOT EDIT.

package db

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type SystemAuth struct {
	Address   string
	CreatedAt time.Time
	LastSeen  time.Time
}

type SystemTable struct {
	UUID       uuid.UUID
	Controller string
	CreatedAt  time.Time
	Type       sql.NullString
}
