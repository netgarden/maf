package maf

type ConfigItemType int

const (
	String ConfigItemType = iota
	Integer
	Int
	Float64
	Bool
	Duration
	StringSlice
)

type ConfigSchema struct {
	items []*ConfigItem
}

type ConfigItem struct {
	Name         string
	Type         ConfigItemType
	Required     bool
	DefaultValue any
}
