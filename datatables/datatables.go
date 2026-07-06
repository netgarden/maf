// Package datatables is a generic query engine for paginated, sortable,
// filterable list endpoints on top of GORM. It's built to sit behind a
// single shared rrpc struct — DatatableRequest (def/datatable.rrpc,
// "include"d directly into a consuming service's own .rrpc file) — so
// every list endpoint's request shape is genuinely the same wire type,
// not just the same field names repeated by convention.
//
// DatatableRequest.Filters is a plain map[string]string: a client
// supplies a column name and a raw value, never an operator. The
// operator applied for each column (Contains, Eq, ...) is fixed
// server-side by that entity's own Column definitions — a deliberate
// safety boundary (a client can't ask for e.g. "gt" on a free-text
// column), not a missing feature.
package datatables

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// SortDir is the direction of a single-column sort.
type SortDir string

const (
	Asc  SortDir = "asc"
	Desc SortDir = "desc"
)

// Operator identifies how a Column's FilterOperator compares its value
// against an incoming Query.Filters entry. Never client-chosen — see the
// package doc comment.
type Operator string

const (
	Eq       Operator = "eq"
	Contains Operator = "contains" // ILIKE, Postgres-specific; %, _, \ escaped in the value first
	Gt       Operator = "gt"
	Gte      Operator = "gte"
	Lt       Operator = "lt"
	Lte      Operator = "lte"
)

// Column declares one queryable field of an entity: the name a Query's
// SortBy/Filters refers to it by, the real SQL column, whether it can be
// sorted on, and — if filterable — the one operator applied whenever
// Query.Filters has a value for it. Built once per entity by the owning
// service, never derived from client input.
type Column struct {
	Name           string
	DBColumn       string
	Sortable       bool
	FilterOperator Operator // "" = not filterable
}

// Query is gathered by a service's RPC impl directly from its generated
// *rpc.DatatableRequest — a straight field copy, since the wire shape
// matches this shape exactly.
type Query struct {
	Page     int // 0-based
	PageSize int
	SortBy   string
	SortDir  SortDir
	Filters  map[string]string
}

// Result is the paginated, filtered, sorted outcome of Apply.
type Result[T any] struct {
	Items      []T
	TotalCount int64
}

const (
	DefaultPageSize = 25
	MaxPageSize     = 100
)

var (
	ErrUnknownSortColumn   = errors.New("datatables: unknown or non-sortable column")
	ErrUnknownFilterColumn = errors.New("datatables: unknown or non-filterable column")
)

// Apply validates q against columns (rejecting an unknown/non-sortable
// SortBy or an unknown/non-filterable Filters key), then returns db's
// filtered, sorted, paginated result as []T, plus the total matching row
// count (computed after filters, before pagination).
func Apply[T any](db *gorm.DB, columns []Column, q Query) (*Result[T], error) {
	byName := make(map[string]Column, len(columns))
	for _, c := range columns {
		byName[c.Name] = c
	}

	for name, value := range q.Filters {
		col, ok := byName[name]
		if !ok || col.FilterOperator == "" {
			return nil, ErrUnknownFilterColumn
		}
		db = applyFilter(db, col, value)
	}

	var totalCount int64
	if err := db.Session(&gorm.Session{}).Count(&totalCount).Error; err != nil {
		return nil, err
	}

	if q.SortBy != "" {
		col, ok := byName[q.SortBy]
		if !ok || !col.Sortable {
			return nil, ErrUnknownSortColumn
		}
		dir := q.SortDir
		if dir != Asc && dir != Desc {
			dir = Asc
		}
		db = db.Order(col.DBColumn + " " + string(dir))
	}

	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	page := q.Page
	if page < 0 {
		page = 0
	}

	var items []T
	if err := db.Offset(page * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, err
	}

	return &Result[T]{Items: items, TotalCount: totalCount}, nil
}

func applyFilter(db *gorm.DB, col Column, value string) *gorm.DB {
	switch col.FilterOperator {
	case Contains:
		return db.Where(col.DBColumn+" ILIKE ? ESCAPE '\\'", "%"+escapeLike(value)+"%")
	case Eq:
		return db.Where(col.DBColumn+" = ?", value)
	case Gt:
		return db.Where(col.DBColumn+" > ?", value)
	case Gte:
		return db.Where(col.DBColumn+" >= ?", value)
	case Lt:
		return db.Where(col.DBColumn+" < ?", value)
	case Lte:
		return db.Where(col.DBColumn+" <= ?", value)
	default:
		return db
	}
}

// escapeLike escapes ILIKE's own wildcard characters (%, _) and its own
// escape character (\) in a user-supplied search value, so e.g. a
// literal "%" or "_" in the search text is matched literally rather than
// treated as a pattern wildcard.
func escapeLike(value string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(value)
}
