package nestedset

import "gorm.io/gorm"

// List returns every node of type T in tree order (ordered by Lft).
// Combine with each node's Depth to render/indent a tree with no further
// queries. model is a sample instance purely so the package knows which
// concrete type/table to query — its field values are ignored (pass
// e.g. &Category{}).
func List[ID comparable, T Node[ID]](db *gorm.DB, model T) ([]T, error) {
	var results []T
	err := db.Model(newInstance(model)).Order("lft").Find(&results).Error
	return results, err
}

// Descendants returns every node strictly inside node's own subtree
// (excluding node itself), in tree order.
func Descendants[ID comparable, T Node[ID]](db *gorm.DB, node T) ([]T, error) {
	pos := node.GetPosition()
	var results []T
	err := db.Model(newInstance(node)).
		Where("lft > ? AND rgt < ?", pos.Lft, pos.Rgt).
		Order("lft").
		Find(&results).Error
	return results, err
}

// Ancestors returns every node that contains node in its subtree
// (excluding node itself), ordered from the root down.
func Ancestors[ID comparable, T Node[ID]](db *gorm.DB, node T) ([]T, error) {
	pos := node.GetPosition()
	var results []T
	err := db.Model(newInstance(node)).
		Where("lft < ? AND rgt > ?", pos.Lft, pos.Rgt).
		Order("lft").
		Find(&results).Error
	return results, err
}

// Children returns node's direct children only (not further descendants),
// in tree order.
func Children[ID comparable, T Node[ID]](db *gorm.DB, node T) ([]T, error) {
	id := node.GetID()
	var results []T
	err := db.Model(newInstance(node)).
		Where("parent_id = ?", id).
		Order("lft").
		Find(&results).Error
	return results, err
}

// ChildrenCount is a plain COUNT(*) WHERE parent_id = ? — computed on
// demand rather than stored/maintained, so Create/MoveTo/Delete have no
// counter to keep in sync.
func ChildrenCount[ID comparable, T Node[ID]](db *gorm.DB, node T) (int64, error) {
	id := node.GetID()
	var count int64
	err := db.Model(newInstance(node)).Where("parent_id = ?", id).Count(&count).Error
	return count, err
}
