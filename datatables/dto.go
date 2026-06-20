package datatable

type DTO struct {
	TotalCount int64       `json:"total_count"`
	Data       interface{} `json:"data"`
}
