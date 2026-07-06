package rpc

// -- Structs ---------------------------------------------

type DatatablePageInfo struct {
	TotalCount int `json:"totalCount"`
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
}

type DatatableRequest struct {
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
	SortBy   string            `json:"sortBy"`
	SortDir  string            `json:"sortDir"`
	Filters  map[string]string `json:"filters"`
}
