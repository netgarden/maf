package datatable

import "gorm.io/gorm"

type Source interface {
	GetColumns() []*Column
	CreateQuery(db *gorm.DB) []*gorm.DB
	SetSelect(db *gorm.DB, i int) *gorm.DB
	GetData(db *gorm.DB, i int) ([]interface{}, error)
}
