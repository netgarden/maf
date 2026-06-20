package datatable

import "strings"

type Column struct {
	Title  string
	Render func(sb *strings.Builder, item interface{})
}
