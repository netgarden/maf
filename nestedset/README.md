# nestedset

`github.com/netgarden/maf/nestedset` — the nested-set (a.k.a. MPTT) tree
model on top of GORM, generic over the entity's own ID type. Store a
hierarchy (categories, org charts, groups) in one table with `Lft`/`Rgt`/
`Depth` columns and query any subtree/ancestor-chain/children list with a
single indexed range query — no recursive CTEs.

## Why generic, and why not `github.com/longbridgeapp/nested-set`

That library (the actual import path — its GitHub org display name is now
`longbridge`, but the module itself still declares
`github.com/longbridgeapp/nested-set`) hard-requires an `int64` ID: it
uses reflection (`reflect.Value.Int()`) to read the tagged ID field, and
builds its `UPDATE`/`WHERE` SQL by string-substituting column names
discovered via struct tags. Neither is actually necessary — `Lft`/`Rgt`/
`Depth` are always plain `int`, entirely independent of the row's own
identity, and `ID`/`ParentID` are only ever used for equality/`IN (...)`
filtering, never arithmetic, so they can be any type GORM can bind as a
query parameter (`uuid.UUID`, `int64`, `string`, ...).

So entities here satisfy a plain Go generic interface (`Node[ID]`) instead
of magic tags. The only reflection this package uses is one narrow,
contained helper (`newInstance`) that allocates a fresh zero-value pointer
of the caller's concrete type — needed to work around a real GORM gotcha
(see below), not a schema/tag parser.

## Usage

Implement `Node[ID]` on your GORM entity:

```go
import (
	uuid "github.com/satori/go.uuid"
	"github.com/netgarden/maf/nestedset"
)

type Category struct {
	ID       uuid.UUID `gorm:"type:uuid;primary_key"`
	ParentID *uuid.UUID
	Lft, Rgt int
	Depth    int
	Name     string
}

func (c *Category) GetID() uuid.UUID          { return c.ID }
func (c *Category) GetParentID() *uuid.UUID   { return c.ParentID }
func (c *Category) SetParentID(id *uuid.UUID) { c.ParentID = id }
func (c *Category) GetPosition() nestedset.Position {
	return nestedset.Position{Lft: c.Lft, Rgt: c.Rgt, Depth: c.Depth}
}
func (c *Category) SetPosition(p nestedset.Position) {
	c.Lft, c.Rgt, c.Depth = p.Lft, p.Rgt, p.Depth
}
```

`int64`, `string`, or any other `comparable` ID type works exactly the
same way — `ParentID` just needs to be `*<your ID type>` (nil = root).

```go
root := &Category{Name: "Electronics"}
nestedset.Create[uuid.UUID](db, root, nil)

phones := &Category{Name: "Phones"}
nestedset.Create[uuid.UUID](db, phones, &root) // root's new last child

// list the whole tree, in order, indented by Depth
all, _ := nestedset.List[uuid.UUID](db, &Category{})

// reparent (and, for its subtree, re-depth) phones to become its own root
nestedset.MoveTo[uuid.UUID](db, phones, root, nestedset.MoveRight)

// remove a node and everything under it
nestedset.Delete[uuid.UUID](db, phones)
```

`Descendants`/`Ancestors`/`Children`/`ChildrenCount` are the standard
nested-set range queries — each a single indexed query, no recursive CTE.
`ChildrenCount` is computed on demand (`COUNT(*) WHERE parent_id = ?`)
rather than stored as a denormalized column: `Create`/`MoveTo`/`Delete`
have no counter to keep in sync as a result.

Column names are fixed by convention (`lft`, `rgt`, `depth`, `parent_id`,
`id`) — GORM's default naming strategy already gives you this if your Go
fields are named `Lft`/`Rgt`/`Depth`/`ParentID`/`ID`; override via your
entity's own `gorm:"column:..."` tag if you need different names.

## Concurrency

Every mutating function (`Create`/`MoveTo`/`Delete`) runs inside its own
transaction, opening with a full-table `SELECT ... FOR UPDATE` lock before
touching `Lft`/`Rgt` — deliberately global, not scoped to the subtree
being touched, since a write anywhere in the tree can shift `Lft`/`Rgt`
values anywhere else in it. The right tradeoff for a small,
low-write-frequency hierarchy (an admin-curated org chart, a category
tree) — the wrong one for anything high-throughput, which is exactly why
this package doesn't try to be a generic solution for every possible
tree-shaped table.

## The GORM gotcha `newInstance` works around

Passing an already-loaded (non-zero ID) entity to `db.Model(...)` makes
GORM silently AND an `id = <that row's id>` condition onto every
subsequent `Where(...)` on that statement — including bulk range-based
`Update`/`Delete` calls, which this package's `Create`/`MoveTo`/`Delete`
all rely on. Confirmed empirically while building this package:
`db.Model(node).Where("rgt > ?", x).UpdateColumn(...)` silently affects 0
rows when `node` has a non-zero ID, even though other rows plainly match —
`db.Model(newInstance(node))` (a genuinely empty struct of the same
concrete type) does not have this problem. Every bulk query in this
package goes through `newInstance` for exactly this reason.

## The GORM gotcha `reload` works around

A second, related gotcha, caught while building citadel's `HostGroup`
feature on top of this package: `gorm`'s `First(dest)` does **not**
overwrite an already-non-nil pointer field (e.g. `*uuid.UUID`) with `nil`
when the queried column is actually `NULL` — it silently leaves the old
in-memory value in place. `MoveTo`'s final "sync `node`/`to` back to their
real values" step used to reuse the caller's own (already-populated)
values as `First`'s destination directly; the one transition that exposed
it was a node moving from "has a parent" to "is a root" (`ParentID`:
non-nil → nil) — `Depth` updated correctly, `ParentID` silently did not.
Fixed by having `reload` scan into a fresh (zero-value) instance first and
copying `ParentID`/`Position` across via the `Node` setters, instead of
reusing the caller's struct as the scan destination. Regression-tested via
`TestMoveTo/to_new_root_clears_parent_id`.

## `MoveTo`'s algorithm

An earlier version of `MoveTo` ported `github.com/longbridgeapp/nested-set`'s
single-pass "affected range" shift arithmetic directly. That algorithm
assumes there's always a genuine gap between a subtree's old and new
position for the "affected" nodes to occupy — it silently corrupts the
tree when moving a node to become its immediately-following sibling's
child (or otherwise directly adjacent to it), since the destination node
itself gets caught by the "affected" shift instead of the separate "grow
to hold a new child" logic it needs. Caught by this package's own tests
(`TestMoveTo/inner_reparents_and_updates_descendant_depth`) — worth noting
since it means the reference implementation's own approach isn't safe to
copy uncritically.

Replaced with a park/close-gap/open-gap/un-park approach that identifies
the moving subtree by primary key throughout, rather than by a coordinate
range that has to stay unambiguous across every intermediate arithmetic
step: one extra pair of queries, but each step is independently checkable
by hand.

## Testing

```bash
go test ./... -race
```

`TestMain` starts a disposable Postgres via `testcontainers-go`
(`postgres:17-alpine`) — Docker required, no manual setup. Tests are
skipped (not failed) if Docker isn't available. Every behavioral test runs
against two entities — one `uuid.UUID`-keyed, one `int64`-keyed — to
actually prove the ID-type-genericity claim rather than just assert it in
a comment, plus an invariant check (every node's `Rgt > Lft`; a parent's
`[Lft,Rgt]` strictly contains every descendant's) after a longer sequence
of create/move/delete calls, since that's the class of bug this model is
genuinely easy to get subtly wrong on — as `MoveTo`'s own history above
demonstrates.
