package datatable

import (
	"gorm.io/gorm"
	"github.com/netgarden/maf/web"
	"strconv"
)

type Datatable struct {
	db       *gorm.DB
	source   Source
	page     int
	pageSize int
}

func New(db *gorm.DB, source Source) *Datatable {
	return &Datatable{
		db:       db,
		source:   source,
		page:     0,
		pageSize: 100,
	}
}

func (dt *Datatable) LoadRequestParams(ctx *web.Context, paramPrefix string) error {
	pageStr := ctx.Param(paramPrefix + "page")
	if pageStr != "" {
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			return err
		}
		if page < 0 {
			page = 0
		}
		dt.page = page
	}

	pageSizeStr := ctx.Param(paramPrefix + "page_size")
	if pageSizeStr != "" {
		pageSize, err := strconv.Atoi(pageSizeStr)
		if err != nil {
			return err
		}
		if pageSize > 100 {
			pageSize = 100
		}
		dt.pageSize = pageSize
	}

	return nil
}

func (dt *Datatable) GetColumns() []*Column {
	return dt.source.GetColumns()
}

func (dt *Datatable) GetTotalCount() (int64, error) {

	var dbs []*gorm.DB
	dbs = dt.source.CreateQuery(dt.db)

	var totalCount int64
	for _, db := range dbs {
		var res int64
		err := db.Count(&res).Error
		if err != nil {
			return 0, err
		}
		totalCount += res
	}

	return totalCount, nil
}

func (dt *Datatable) GetData() ([]interface{}, error) {

	var dbs []*gorm.DB
	dbs = dt.source.CreateQuery(dt.db)

	ret := make([]interface{}, 0)
	for i, db := range dbs {

		db = dt.source.SetSelect(db, i)
		res, err := dt.source.GetData(db, i)
		if err != nil {
			return nil, err
		}

		for _, v := range res {
			ret = append(ret, v)
		}
	}

	return ret, nil
}

func (dt *Datatable) RenderHTML() (string, error) {
	renderer := NewRenderer(dt)
	return renderer.Render()
}

func (dt *Datatable) DTO() (*DTO, error) {

	totalCount, err := dt.GetTotalCount()
	if err != nil {
		return nil, err
	}

	data, err := dt.GetData()
	if err != nil {
		return nil, err
	}

	return &DTO{
		TotalCount: totalCount,
		Data:       data,
	}, nil
}
