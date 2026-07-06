// Package nestedset implements the nested-set (a.k.a. MPTT) tree model on
// top of GORM, generic over the entity's own ID type — uuid.UUID, int64,
// string, or anything else comparable. See Node for what an entity needs
// to implement, and README.md for a full worked example.
//
// Unlike reflection/struct-tag-driven implementations (e.g.
// github.com/longbridgeapp/nested-set, which this package's design was
// checked against), entities satisfy a plain Go interface instead of
// magic tags, and the package assumes standard GORM snake_case column
// names for the tagged fields (lft, rgt, depth, parent_id, id). One
// narrow, contained use of reflection remains: newInstance allocates a
// fresh zero-value pointer of the caller's concrete entity type, needed
// to work around a real GORM gotcha (see that function's comment) — this
// is not a schema/tag parser, just "give me an empty one of these."
package nestedset

import (
	"errors"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrInvalidMove is returned by MoveTo when asked to move a node into its
// own subtree (including into itself).
var ErrInvalidMove = errors.New("nestedset: cannot move a node into itself or one of its own descendants")

// Position holds a node's tree-bookkeeping fields — always plain ints,
// regardless of the entity's own ID type. Child counts are deliberately
// not tracked here: use ChildrenCount (query.go) to compute one on demand
// rather than maintaining a denormalized counter that Create/MoveTo/Delete
// would otherwise all have to keep in sync.
type Position struct {
	Lft, Rgt, Depth int
}

// Node is implemented by any GORM entity that wants nested-set tree
// behavior. ID is the entity's own primary-key type; this package never
// does anything with it beyond equality comparisons and passing it to
// GORM as a query parameter, so it places no constraint on how your
// application generates IDs.
//
// Implementations need pointer receivers on the Set* methods (to mutate
// the caller's value), so in practice T is always instantiated as a
// pointer type (e.g. *Category) at every call site in this package.
//
// The package assumes standard GORM snake_case column names for the
// tagged fields (lft, rgt, depth, parent_id, id) — the default GORM
// naming strategy already gives you this if your Go fields are named
// Lft/Rgt/Depth/ParentID/ID. Override via your entity's own
// `gorm:"column:..."` tag if you need different names.
type Node[ID comparable] interface {
	GetID() ID
	GetParentID() *ID
	SetParentID(*ID)
	GetPosition() Position
	SetPosition(Position)
}

// MoveDirection selects where MoveTo places a node relative to another.
type MoveDirection int

const (
	// MoveLeft: MoveTo(db, a, to, MoveLeft) => a|to|... (a becomes to's left sibling)
	MoveLeft MoveDirection = -1
	// MoveInner: MoveTo(db, a, to, MoveInner) => [to [a|...]] (a becomes to's first child)
	MoveInner MoveDirection = 0
	// MoveRight: MoveTo(db, a, to, MoveRight) => ...|to|a| (a becomes to's right sibling)
	MoveRight MoveDirection = 1
)

// newInstance allocates a fresh, zero-value pointer of sample's own
// concrete type. Needed because passing an already-loaded (non-zero ID)
// entity to db.Model(...) makes GORM silently AND an "id = <that row's
// id>" condition onto every subsequent Where(...) on that statement —
// exactly the range-based bulk Update/Delete calls this package's
// Create/MoveTo/Delete all rely on. Confirmed empirically: db.Model(node)
// with a populated node restricts a WHERE rgt > ? UpdateColumn to 0 rows
// even when other rows plainly match; db.Model(newInstance(node)) does
// not have this problem. This is the one place this package uses
// reflection, and only to allocate an empty struct — never to inspect
// tags or field names.
func newInstance[T any](sample T) T {
	t := reflect.TypeOf(sample)
	if t != nil && t.Kind() == reflect.Ptr {
		return reflect.New(t.Elem()).Interface().(T)
	}
	var zero T
	return zero
}

// lockAll takes a full-table SELECT ... FOR UPDATE lock, serializing this
// transaction against any other concurrent Create/MoveTo/Delete on the
// same entity type. Deliberately global (not scoped to the subtree being
// touched): the standard technique for nested-set writes, since any write
// anywhere in the tree can shift Lft/Rgt values anywhere else in it. The
// right tradeoff for a small, low-write-frequency hierarchy (an
// admin-curated org chart, a category tree) — the wrong one for anything
// high-throughput, which is exactly why this package doesn't try to be a
// generic solution for every possible tree-shaped table.
func lockAll[ID comparable, T Node[ID]](tx *gorm.DB, sample T) error {
	var discard []ID
	return tx.Model(newInstance(sample)).Clauses(clause.Locking{Strength: "UPDATE"}).Pluck("id", &discard).Error
}

// reload re-reads node's row fresh from within the current transaction
// (and lock), overwriting node's ParentID/Position fields in place.
// Necessary because a caller-supplied node may have been fetched before
// this transaction's lock was acquired — the lock only blocks *further*
// concurrent writes, it doesn't retroactively refresh values already read
// into memory.
//
// Scans into a fresh instance first and copies ParentID/Position across
// via the Node setters, rather than reusing node as First's destination
// directly. Confirmed empirically: gorm's First(dest) does not overwrite
// an already-non-nil pointer field (e.g. *uuid.UUID) with nil when the
// column is actually NULL — it silently leaves the old in-memory value in
// place. That would otherwise show up as a real bug the moment a node
// with a parent is moved to become a new root (ParentID: non-nil -> nil).
func reload[ID comparable, T Node[ID]](tx *gorm.DB, node T) error {
	fresh := newInstance(node)
	if err := tx.Where("id = ?", node.GetID()).First(fresh).Error; err != nil {
		return err
	}
	node.SetParentID(fresh.GetParentID())
	node.SetPosition(fresh.GetPosition())
	return nil
}

// Create inserts node as parent's new last child, or as a new last root
// (placed after the current maximum Rgt in the whole tree) if parent is
// nil.
func Create[ID comparable, T Node[ID]](db *gorm.DB, node T, parent *T) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := lockAll[ID](tx, node); err != nil {
			return err
		}

		if parent == nil {
			var maxRgt int
			if err := tx.Model(newInstance(node)).Select("COALESCE(MAX(rgt), 0)").Scan(&maxRgt).Error; err != nil {
				return err
			}
			node.SetParentID(nil)
			node.SetPosition(Position{Lft: maxRgt + 1, Rgt: maxRgt + 2, Depth: 0})
			return tx.Create(node).Error
		}

		p := *parent
		if err := reload[ID](tx, p); err != nil {
			return err
		}
		parentPos := p.GetPosition()
		parentID := p.GetID()

		if err := tx.Model(newInstance(node)).Where("rgt >= ?", parentPos.Rgt).
			UpdateColumn("rgt", gorm.Expr("rgt + 2")).Error; err != nil {
			return err
		}
		if err := tx.Model(newInstance(node)).Where("lft > ?", parentPos.Rgt).
			UpdateColumn("lft", gorm.Expr("lft + 2")).Error; err != nil {
			return err
		}

		node.SetParentID(&parentID)
		node.SetPosition(Position{Lft: parentPos.Rgt, Rgt: parentPos.Rgt + 1, Depth: parentPos.Depth + 1})
		return tx.Create(node).Error
	})
}

// moveParkOffset is added to the moving subtree's Lft/Rgt to get it out of
// the way of the gap-close/gap-open shifts below, then subtracted back off
// (net of the actual move delta) once those have settled. Large enough to
// never collide with any real Lft/Rgt value.
const moveParkOffset = 1 << 30

// MoveTo moves node to a position relative to to (which must not be node
// itself or inside node's own subtree — that returns ErrInvalidMove).
//
// Implementation note: an earlier version of this function ported
// github.com/longbridgeapp/nested-set's single-pass "affected range" shift
// arithmetic directly. That algorithm assumes there's always a genuine gap
// between a subtree's old and new position for the "affected" nodes to
// occupy — it silently produces a corrupt tree when moving a node to
// become its immediately-following sibling's child (or otherwise directly
// adjacent to it), since the destination node itself gets caught by the
// "affected" shift instead of the separate "grow to hold a new child"
// logic it needs. Caught by this package's own tests
// (TestMoveTo/inner_reparents_and_updates_descendant_depth). Replaced with
// the park/close-gap/open-gap/un-park approach below, which identifies the
// moving subtree by primary key throughout rather than by a coordinate
// range that has to stay unambiguous across every intermediate step —
// slower (one extra pair of queries) but straightforward to verify by hand,
// which matters more here than shaving a round trip.
func MoveTo[ID comparable, T Node[ID]](db *gorm.DB, node, to T, direction MoveDirection) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := lockAll[ID](tx, node); err != nil {
			return err
		}
		if err := reload[ID](tx, node); err != nil {
			return err
		}
		if err := reload[ID](tx, to); err != nil {
			return err
		}

		nodeID, toID := node.GetID(), to.GetID()
		nodePos := node.GetPosition()

		if nodeID == toID {
			return ErrInvalidMove
		}
		if toPos := to.GetPosition(); toPos.Lft >= nodePos.Lft && toPos.Lft <= nodePos.Rgt {
			return ErrInvalidMove
		}

		var subtreeIDs []ID
		if err := tx.Model(newInstance(node)).
			Where("lft >= ? AND rgt <= ?", nodePos.Lft, nodePos.Rgt).
			Pluck("id", &subtreeIDs).Error; err != nil {
			return err
		}

		width := nodePos.Rgt - nodePos.Lft + 1

		// 1. park the moving subtree out of the way of the shifts below.
		if err := tx.Model(newInstance(node)).Where("id IN ?", subtreeIDs).
			Updates(map[string]interface{}{
				"lft": gorm.Expr("lft + ?", moveParkOffset),
				"rgt": gorm.Expr("rgt + ?", moveParkOffset),
			}).Error; err != nil {
			return err
		}

		// 2. close the gap the subtree left behind.
		if err := tx.Model(newInstance(node)).
			Where("rgt > ? AND id NOT IN ?", nodePos.Rgt, subtreeIDs).
			UpdateColumn("rgt", gorm.Expr("rgt - ?", width)).Error; err != nil {
			return err
		}
		if err := tx.Model(newInstance(node)).
			Where("lft > ? AND id NOT IN ?", nodePos.Rgt, subtreeIDs).
			UpdateColumn("lft", gorm.Expr("lft - ?", width)).Error; err != nil {
			return err
		}

		// 3. to's position may have just shifted in step 2 — re-read it.
		if err := reload[ID](tx, to); err != nil {
			return err
		}
		toPos := to.GetPosition()

		var newParentID *ID
		var newLft, depthChange int
		switch direction {
		case MoveLeft:
			newParentID = to.GetParentID()
			depthChange = toPos.Depth - nodePos.Depth
			newLft = toPos.Lft
		case MoveRight:
			newParentID = to.GetParentID()
			depthChange = toPos.Depth - nodePos.Depth
			newLft = toPos.Rgt + 1
		default: // MoveInner
			newParentID = &toID
			depthChange = toPos.Depth + 1 - nodePos.Depth
			newLft = toPos.Lft + 1
		}

		// 4. open a gap at the new position. When direction is MoveInner and
		// to has no other children yet, newLft == toPos.Rgt, so this also
		// correctly grows to's own Rgt to enclose its new child — there's no
		// separate "grow the new parent" step needed.
		if err := tx.Model(newInstance(node)).
			Where("rgt >= ? AND id NOT IN ?", newLft, subtreeIDs).
			UpdateColumn("rgt", gorm.Expr("rgt + ?", width)).Error; err != nil {
			return err
		}
		if err := tx.Model(newInstance(node)).
			Where("lft >= ? AND id NOT IN ?", newLft, subtreeIDs).
			UpdateColumn("lft", gorm.Expr("lft + ?", width)).Error; err != nil {
			return err
		}

		// 5. un-park the subtree into its final position and adjust depth.
		delta := newLft - nodePos.Lft
		if err := tx.Model(newInstance(node)).Where("id IN ?", subtreeIDs).
			Updates(map[string]interface{}{
				"lft":   gorm.Expr("lft - ? + ?", moveParkOffset, delta),
				"rgt":   gorm.Expr("rgt - ? + ?", moveParkOffset, delta),
				"depth": gorm.Expr("depth + ?", depthChange),
			}).Error; err != nil {
			return err
		}

		// 6. reparent only the subtree's own root.
		if err := tx.Model(newInstance(node)).Where("id = ?", nodeID).Update("parent_id", newParentID).Error; err != nil {
			return err
		}

		// 7. sync node/to's in-memory state to their final values, same
		// courtesy Create gives its caller — without this, node and to would
		// be left holding whatever position they had part-way through the
		// steps above (e.g. to's Rgt before it grew to enclose its new
		// child), silently wrong for any code that inspects them right after
		// a successful MoveTo instead of re-querying.
		if err := reload[ID](tx, node); err != nil {
			return err
		}
		return reload[ID](tx, to)
	})
}

// Delete removes node and its entire subtree, closing the gap left
// behind. Delete has no opinion on whether that's actually safe for your
// application (e.g. whether something outside this tree still references
// one of these rows) — that's your own business-logic check before
// calling this, deliberately not this package's concern.
func Delete[ID comparable, T Node[ID]](db *gorm.DB, node T) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := lockAll[ID](tx, node); err != nil {
			return err
		}
		if err := reload[ID](tx, node); err != nil {
			return err
		}

		pos := node.GetPosition()
		width := pos.Rgt - pos.Lft + 1

		if err := tx.Model(newInstance(node)).
			Where("lft >= ? AND rgt <= ?", pos.Lft, pos.Rgt).
			Delete(newInstance(node)).Error; err != nil {
			return err
		}
		if err := tx.Model(newInstance(node)).Where("rgt > ?", pos.Rgt).
			UpdateColumn("rgt", gorm.Expr("rgt - ?", width)).Error; err != nil {
			return err
		}
		return tx.Model(newInstance(node)).Where("lft > ?", pos.Rgt).
			UpdateColumn("lft", gorm.Expr("lft - ?", width)).Error
	})
}
