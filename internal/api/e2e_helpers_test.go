package api

import (
	"database/sql"
	"encoding/json"

	"github.com/leassh-dev/leassh/internal/store"
)

type sqlRows = sql.Rows

type rowsQ struct{ st *store.Store }

func (r rowsQ) Query(q string, a ...any) (*sql.Rows, error) { return r.st.DB.Query(q, a...) }

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
