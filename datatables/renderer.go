package datatable

import "strings"

type Renderer struct {
	datatable *Datatable
}

func NewRenderer(datatable *Datatable) *Renderer {
	return &Renderer{
		datatable: datatable,
	}
}

func (r *Renderer) Render() (string, error) {

	/*
		totalCount, err := r.datatable.GetTotalCount()
		if err != nil {
			return "", err
		}
	*/

	data, err := r.datatable.GetData()
	if err != nil {
		return "", err
	}

	columns := r.datatable.GetColumns()

	sb := &strings.Builder{}
	r.renderHeader(sb, columns)
	r.renderData(sb, columns, data)
	r.renderFooter(sb)

	return sb.String(), nil
}

func (r *Renderer) renderHeader(sb *strings.Builder, columns []*Column) {

	sb.WriteString("<div class=\"table-responsive\">\n")
	sb.WriteString("<table class=\"table table-striped table-sm\">\n")
	sb.WriteString("  <thead>\n")
	sb.WriteString("    <tr>\n")

	for i := 0; i < len(columns); i++ {
		column := columns[i]
		sb.WriteString("      <th>" + column.Title + "</th>")
	}

	sb.WriteString("    </tr>\n")
	sb.WriteString("  </thead>\n")
	sb.WriteString("  </tbody>\n")

}

func (r *Renderer) renderFooter(sb *strings.Builder) {
	sb.WriteString("  </tbody>\n")
	sb.WriteString("</table>\n")
	sb.WriteString("</div>")
}

func (r *Renderer) renderData(sb *strings.Builder, columns []*Column, data []interface{}) {

	for i := 0; i < len(data); i++ {
		item := data[i]
		sb.WriteString("    <tr>\n")

		for c := 0; c < len(columns); c++ {
			column := columns[c]
			sb.WriteString("      <td>\n")
			column.Render(sb, item)
			sb.WriteString("      </td>\n")
		}

		sb.WriteString("    </tr>\n")
	}

}
