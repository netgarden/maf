package nestedset_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/netgarden/maf/nestedset"

	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// --- test entities: one uuid.UUID-keyed, one int64-keyed, to actually
// prove the ID-type-genericity claim rather than just assert it. ---

type uuidCategory struct {
	ID       uuid.UUID  `gorm:"type:uuid;primary_key"`
	ParentID *uuid.UUID `gorm:"type:uuid"`
	Lft, Rgt int
	Depth    int
	Name     string
}

func (uuidCategory) TableName() string            { return "nestedset_test_uuid_categories" }
func (c *uuidCategory) GetID() uuid.UUID          { return c.ID }
func (c *uuidCategory) GetParentID() *uuid.UUID   { return c.ParentID }
func (c *uuidCategory) SetParentID(id *uuid.UUID) { c.ParentID = id }
func (c *uuidCategory) GetPosition() nestedset.Position {
	return nestedset.Position{Lft: c.Lft, Rgt: c.Rgt, Depth: c.Depth}
}
func (c *uuidCategory) SetPosition(p nestedset.Position) {
	c.Lft, c.Rgt, c.Depth = p.Lft, p.Rgt, p.Depth
}
func (c *uuidCategory) BeforeCreate(tx *gorm.DB) error {
	if uuid.Equal(c.ID, uuid.Nil) {
		c.ID = uuid.NewV4()
	}
	return nil
}

type intCategory struct {
	ID       int64 `gorm:"primary_key;autoIncrement"`
	ParentID *int64
	Lft, Rgt int
	Depth    int
	Name     string
}

func (intCategory) TableName() string        { return "nestedset_test_int_categories" }
func (c *intCategory) GetID() int64          { return c.ID }
func (c *intCategory) GetParentID() *int64   { return c.ParentID }
func (c *intCategory) SetParentID(id *int64) { c.ParentID = id }
func (c *intCategory) GetPosition() nestedset.Position {
	return nestedset.Position{Lft: c.Lft, Rgt: c.Rgt, Depth: c.Depth}
}
func (c *intCategory) SetPosition(p nestedset.Position) {
	c.Lft, c.Rgt, c.Depth = p.Lft, p.Rgt, p.Depth
}

// --- TestMain: one disposable Postgres container for this package's
// entire test run (same pattern as maf/locks, jobs, auth, mailer this
// session) — no manual DB setup, Docker is the only requirement. ---

var (
	testDSN    string
	testDSNErr error
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	pg, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
	)
	if err != nil {
		testDSNErr = err
		os.Exit(m.Run())
	}

	testDSN, testDSNErr = pg.ConnectionString(ctx, "sslmode=disable")
	if testDSNErr == nil {
		testDSNErr = waitForPostgresReady(testDSN)
	}

	code := m.Run()
	_ = pg.Terminate(ctx)
	os.Exit(code)
}

// waitForPostgresReady retries a plain ping for a few seconds — the
// container's own readiness signal (log line + port check) fires slightly
// before the mapped port reliably accepts connections in this
// environment, so the first real connection attempt can otherwise land in
// that gap.
func waitForPostgresReady(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	deadline := time.Now().Add(20 * time.Second)
	var pingErr error
	for time.Now().Before(deadline) {
		if pingErr = db.Ping(); pingErr == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return pingErr
}

func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	if testDSNErr != nil {
		t.Skipf("postgres testcontainer unavailable (Docker required): %v", testDSNErr)
	}

	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	if err := db.AutoMigrate(&uuidCategory{}, &intCategory{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	reset := func() {
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&uuidCategory{})
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&intCategory{})
	}
	reset()
	t.Cleanup(reset)

	return db
}

func assertPosition(t *testing.T, label string, got nestedset.Position, wantLft, wantRgt, wantDepth int) {
	t.Helper()
	if got.Lft != wantLft || got.Rgt != wantRgt || got.Depth != wantDepth {
		t.Errorf("%s: got Position{Lft:%d Rgt:%d Depth:%d}, want {Lft:%d Rgt:%d Depth:%d}",
			label, got.Lft, got.Rgt, got.Depth, wantLft, wantRgt, wantDepth)
	}
}

// --- Create ---

func testCreate_RootAndChildren[ID comparable, T nestedset.Node[ID]](t *testing.T, db *gorm.DB, newNode func(name string) T) {
	t.Helper()

	root := newNode("root")
	if err := nestedset.Create[ID](db, root, nil); err != nil {
		t.Fatalf("Create(root): %v", err)
	}
	assertPosition(t, "root", root.GetPosition(), 1, 2, 0)

	child1 := newNode("child1")
	if err := nestedset.Create[ID](db, child1, &root); err != nil {
		t.Fatalf("Create(child1): %v", err)
	}
	assertPosition(t, "child1", child1.GetPosition(), 2, 3, 1)

	child2 := newNode("child2")
	if err := nestedset.Create[ID](db, child2, &root); err != nil {
		t.Fatalf("Create(child2): %v", err)
	}
	// child2 inserted as root's new last child, to the right of child1.
	assertPosition(t, "child2", child2.GetPosition(), 4, 5, 1)

	grandchild := newNode("grandchild")
	if err := nestedset.Create[ID](db, grandchild, &child1); err != nil {
		t.Fatalf("Create(grandchild): %v", err)
	}
	assertPosition(t, "grandchild", grandchild.GetPosition(), 3, 4, 2)

	// root's own Rgt must have been pushed out to enclose everything.
	fresh, err := nestedset.List[ID](db, newNode(""))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, n := range fresh {
		if n.GetID() == root.GetID() {
			assertPosition(t, "root (reloaded)", n.GetPosition(), 1, 8, 0)
		}
	}

	secondRoot := newNode("second-root")
	if err := nestedset.Create[ID](db, secondRoot, nil); err != nil {
		t.Fatalf("Create(secondRoot): %v", err)
	}
	assertPosition(t, "secondRoot", secondRoot.GetPosition(), 9, 10, 0)
}

func TestCreate_RootAndChildren(t *testing.T) {
	db := testDB(t)
	t.Run("uuid", func(t *testing.T) {
		testCreate_RootAndChildren[uuid.UUID](t, db, func(name string) *uuidCategory {
			return &uuidCategory{Name: name}
		})
	})
	t.Run("int64", func(t *testing.T) {
		testCreate_RootAndChildren[int64](t, db, func(name string) *intCategory {
			return &intCategory{Name: name}
		})
	})
}

// --- Delete ---

func testDelete_RemovesSubtreeAndClosesGap[ID comparable, T nestedset.Node[ID]](t *testing.T, db *gorm.DB, newNode func(name string) T) {
	t.Helper()

	root := newNode("root")
	must(t, nestedset.Create[ID](db, root, nil))
	a := newNode("a")
	must(t, nestedset.Create[ID](db, a, &root))
	aChild := newNode("a-child")
	must(t, nestedset.Create[ID](db, aChild, &a))
	b := newNode("b")
	must(t, nestedset.Create[ID](db, b, &root))

	// Tree: root[1,8] > a[2,5] > aChild[3,4]; root > b[6,7]
	if err := nestedset.Delete[ID](db, a); err != nil {
		t.Fatalf("Delete(a): %v", err)
	}

	remaining, err := nestedset.List[ID](db, newNode(""))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining nodes (root, b), got %d", len(remaining))
	}
	for _, n := range remaining {
		if n.GetID() == a.GetID() || n.GetID() == aChild.GetID() {
			t.Errorf("expected %v and its child to be gone, found: %+v", a.GetID(), n)
		}
		if n.GetID() == root.GetID() {
			assertPosition(t, "root (after delete)", n.GetPosition(), 1, 4, 0)
		}
		if n.GetID() == b.GetID() {
			assertPosition(t, "b (after delete, gap closed)", n.GetPosition(), 2, 3, 1)
		}
	}
}

func TestDelete_RemovesSubtreeAndClosesGap(t *testing.T) {
	db := testDB(t)
	t.Run("uuid", func(t *testing.T) {
		testDelete_RemovesSubtreeAndClosesGap[uuid.UUID](t, db, func(name string) *uuidCategory {
			return &uuidCategory{Name: name}
		})
	})
	t.Run("int64", func(t *testing.T) {
		testDelete_RemovesSubtreeAndClosesGap[int64](t, db, func(name string) *intCategory {
			return &intCategory{Name: name}
		})
	})
}

// --- MoveTo ---

func testMoveTo_Inner_ReparentsAndUpdatesDescendantDepth[ID comparable, T nestedset.Node[ID]](t *testing.T, db *gorm.DB, newNode func(name string) T) {
	t.Helper()

	root := newNode("root")
	must(t, nestedset.Create[ID](db, root, nil))
	a := newNode("a")
	must(t, nestedset.Create[ID](db, a, &root))
	aChild := newNode("a-child")
	must(t, nestedset.Create[ID](db, aChild, &a))
	b := newNode("b")
	must(t, nestedset.Create[ID](db, b, &root))

	// Move a (with its child) to become b's child.
	if err := nestedset.MoveTo[ID](db, a, b, nestedset.MoveInner); err != nil {
		t.Fatalf("MoveTo(a, b, Inner): %v", err)
	}

	children, err := nestedset.Children[ID](db, b)
	if err != nil {
		t.Fatalf("Children(b): %v", err)
	}
	if len(children) != 1 || children[0].GetID() != a.GetID() {
		t.Fatalf("expected b's only child to be a, got %+v", children)
	}

	descendants, err := nestedset.Descendants[ID](db, b)
	if err != nil {
		t.Fatalf("Descendants(b): %v", err)
	}
	if len(descendants) != 2 {
		t.Fatalf("expected b to have 2 descendants (a, a-child), got %d", len(descendants))
	}
	// aChild itself was never passed to MoveTo (only its ancestor a was, as
	// part of a's subtree) so unlike node/to, its in-memory copy is never
	// refreshed by the package — fetch it fresh rather than trusting the
	// local aChild variable's now-stale position.
	var freshAChild T
	for _, n := range descendants {
		if n.GetID() == a.GetID() && n.GetPosition().Depth != b.GetPosition().Depth+1 {
			t.Errorf("expected a's depth to be b.Depth+1, got %d (b.Depth=%d)", n.GetPosition().Depth, b.GetPosition().Depth)
		}
		if n.GetID() == aChild.GetID() {
			freshAChild = n
			if n.GetPosition().Depth != b.GetPosition().Depth+2 {
				t.Errorf("expected a-child's depth to be b.Depth+2, got %d", n.GetPosition().Depth)
			}
		}
	}

	ancestors, err := nestedset.Ancestors[ID](db, freshAChild)
	if err != nil {
		t.Fatalf("Ancestors(aChild): %v", err)
	}
	if len(ancestors) != 3 { // root, b, a
		t.Fatalf("expected 3 ancestors of a-child after the move, got %d", len(ancestors))
	}
}

func testMoveTo_LeftAndRight_ReorderSiblingsUnderSameParent[ID comparable, T nestedset.Node[ID]](t *testing.T, db *gorm.DB, newNode func(name string) T) {
	t.Helper()

	root := newNode("root")
	must(t, nestedset.Create[ID](db, root, nil))
	a := newNode("a")
	must(t, nestedset.Create[ID](db, a, &root))
	b := newNode("b")
	must(t, nestedset.Create[ID](db, b, &root))
	c := newNode("c")
	must(t, nestedset.Create[ID](db, c, &root))

	// order is a, b, c — move c to the left of a: c, a, b
	if err := nestedset.MoveTo[ID](db, c, a, nestedset.MoveLeft); err != nil {
		t.Fatalf("MoveTo(c, a, Left): %v", err)
	}
	children, err := nestedset.Children[ID](db, root)
	if err != nil {
		t.Fatalf("Children(root): %v", err)
	}
	wantOrder := []ID{c.GetID(), a.GetID(), b.GetID()}
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(children))
	}
	for i, n := range children {
		if n.GetID() != wantOrder[i] {
			t.Errorf("position %d: got id %v, want %v", i, n.GetID(), wantOrder[i])
		}
	}

	// move b to the right of c: b, c, a
	if err := nestedset.MoveTo[ID](db, b, c, nestedset.MoveRight); err != nil {
		t.Fatalf("MoveTo(b, c, Right): %v", err)
	}
	children, err = nestedset.Children[ID](db, root)
	if err != nil {
		t.Fatalf("Children(root): %v", err)
	}
	wantOrder = []ID{c.GetID(), b.GetID(), a.GetID()}
	for i, n := range children {
		if n.GetID() != wantOrder[i] {
			t.Errorf("position %d: got id %v, want %v", i, n.GetID(), wantOrder[i])
		}
	}
}

func testMoveTo_RejectsMoveIntoOwnSubtree[ID comparable, T nestedset.Node[ID]](t *testing.T, db *gorm.DB, newNode func(name string) T) {
	t.Helper()

	root := newNode("root")
	must(t, nestedset.Create[ID](db, root, nil))
	a := newNode("a")
	must(t, nestedset.Create[ID](db, a, &root))
	aChild := newNode("a-child")
	must(t, nestedset.Create[ID](db, aChild, &a))

	if err := nestedset.MoveTo[ID](db, a, aChild, nestedset.MoveInner); err != nestedset.ErrInvalidMove {
		t.Errorf("expected ErrInvalidMove moving a into its own child, got %v", err)
	}
	if err := nestedset.MoveTo[ID](db, a, a, nestedset.MoveInner); err != nestedset.ErrInvalidMove {
		t.Errorf("expected ErrInvalidMove moving a into itself, got %v", err)
	}
}

// testMoveTo_ToNewRoot_ClearsParentID is the regression test for a real
// bug caught while building citadel's HostGroup feature on top of this
// package: reload's first implementation scanned query results directly
// into the caller-supplied (already non-nil) node/to values via gorm's
// First(dest) — which does not overwrite an already-non-nil pointer field
// with nil when the column is actually NULL, silently leaving the old
// value in place. A node moving from "has a parent" to "is a root"
// (ParentID: non-nil -> nil) is exactly the transition that exposed it:
// depth updated correctly, ParentID silently did not.
func testMoveTo_ToNewRoot_ClearsParentID[ID comparable, T nestedset.Node[ID]](t *testing.T, db *gorm.DB, newNode func(name string) T) {
	t.Helper()

	root := newNode("root")
	must(t, nestedset.Create[ID](db, root, nil))
	child := newNode("child")
	must(t, nestedset.Create[ID](db, child, &root))

	if err := nestedset.MoveTo[ID](db, child, root, nestedset.MoveRight); err != nil {
		t.Fatalf("MoveTo(child, root, Right): %v", err)
	}

	if child.GetPosition().Depth != 0 {
		t.Errorf("expected child's depth to become 0 after becoming a root, got %d", child.GetPosition().Depth)
	}
	if child.GetParentID() != nil {
		t.Errorf("expected child's in-memory ParentID to be nil after becoming a root, got %v", *child.GetParentID())
	}

	// Also verify the DB itself agrees, not just the in-memory struct.
	all, err := nestedset.List[ID](db, newNode(""))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, n := range all {
		if n.GetID() == child.GetID() && n.GetParentID() != nil {
			t.Errorf("expected child's stored ParentID to be nil after becoming a root, got %v", *n.GetParentID())
		}
	}
}

func TestMoveTo(t *testing.T) {
	db := testDB(t)
	t.Run("inner reparents and updates descendant depth/uuid", func(t *testing.T) {
		testMoveTo_Inner_ReparentsAndUpdatesDescendantDepth[uuid.UUID](t, db, func(name string) *uuidCategory {
			return &uuidCategory{Name: name}
		})
	})
	t.Run("inner reparents and updates descendant depth/int64", func(t *testing.T) {
		testMoveTo_Inner_ReparentsAndUpdatesDescendantDepth[int64](t, db, func(name string) *intCategory {
			return &intCategory{Name: name}
		})
	})
	t.Run("left/right reorder siblings/uuid", func(t *testing.T) {
		testMoveTo_LeftAndRight_ReorderSiblingsUnderSameParent[uuid.UUID](t, db, func(name string) *uuidCategory {
			return &uuidCategory{Name: name}
		})
	})
	t.Run("left/right reorder siblings/int64", func(t *testing.T) {
		testMoveTo_LeftAndRight_ReorderSiblingsUnderSameParent[int64](t, db, func(name string) *intCategory {
			return &intCategory{Name: name}
		})
	})
	t.Run("rejects move into own subtree/uuid", func(t *testing.T) {
		testMoveTo_RejectsMoveIntoOwnSubtree[uuid.UUID](t, db, func(name string) *uuidCategory {
			return &uuidCategory{Name: name}
		})
	})
	t.Run("rejects move into own subtree/int64", func(t *testing.T) {
		testMoveTo_RejectsMoveIntoOwnSubtree[int64](t, db, func(name string) *intCategory {
			return &intCategory{Name: name}
		})
	})
	t.Run("to new root clears parent id/uuid", func(t *testing.T) {
		testMoveTo_ToNewRoot_ClearsParentID[uuid.UUID](t, db, func(name string) *uuidCategory {
			return &uuidCategory{Name: name}
		})
	})
	t.Run("to new root clears parent id/int64", func(t *testing.T) {
		testMoveTo_ToNewRoot_ClearsParentID[int64](t, db, func(name string) *intCategory {
			return &intCategory{Name: name}
		})
	})
}

// --- ChildrenCount (computed on demand) ---

func testChildrenCount_ReflectsCurrentChildrenWithNoStoredCounter[ID comparable, T nestedset.Node[ID]](t *testing.T, db *gorm.DB, newNode func(name string) T) {
	t.Helper()

	root := newNode("root")
	must(t, nestedset.Create[ID](db, root, nil))

	count, err := nestedset.ChildrenCount[ID](db, root)
	if err != nil {
		t.Fatalf("ChildrenCount: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 children, got %d", count)
	}

	a := newNode("a")
	must(t, nestedset.Create[ID](db, a, &root))
	b := newNode("b")
	must(t, nestedset.Create[ID](db, b, &root))

	count, err = nestedset.ChildrenCount[ID](db, root)
	if err != nil {
		t.Fatalf("ChildrenCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 children after creating a, b, got %d", count)
	}

	must(t, nestedset.Delete[ID](db, a))

	count, err = nestedset.ChildrenCount[ID](db, root)
	if err != nil {
		t.Fatalf("ChildrenCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 child after deleting a, got %d", count)
	}
}

func TestChildrenCount(t *testing.T) {
	db := testDB(t)
	t.Run("uuid", func(t *testing.T) {
		testChildrenCount_ReflectsCurrentChildrenWithNoStoredCounter[uuid.UUID](t, db, func(name string) *uuidCategory {
			return &uuidCategory{Name: name}
		})
	})
	t.Run("int64", func(t *testing.T) {
		testChildrenCount_ReflectsCurrentChildrenWithNoStoredCounter[int64](t, db, func(name string) *intCategory {
			return &intCategory{Name: name}
		})
	})
}

// --- Invariants after a longer sequence of operations ---

func testInvariants_HoldAfterCreateMoveDeleteSequence[ID comparable, T nestedset.Node[ID]](t *testing.T, db *gorm.DB, newNode func(name string) T) {
	t.Helper()

	root := newNode("root")
	must(t, nestedset.Create[ID](db, root, nil))
	a := newNode("a")
	must(t, nestedset.Create[ID](db, a, &root))
	b := newNode("b")
	must(t, nestedset.Create[ID](db, b, &root))
	c := newNode("c")
	must(t, nestedset.Create[ID](db, c, &a))
	d := newNode("d")
	must(t, nestedset.Create[ID](db, d, &a))
	e := newNode("e")
	must(t, nestedset.Create[ID](db, e, &root))

	must(t, nestedset.MoveTo[ID](db, d, b, nestedset.MoveInner))
	must(t, nestedset.MoveTo[ID](db, e, a, nestedset.MoveLeft))
	must(t, nestedset.Delete[ID](db, c))
	must(t, nestedset.MoveTo[ID](db, b, root, nestedset.MoveInner))

	all, err := nestedset.List[ID](db, newNode(""))
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	byID := make(map[ID]T, len(all))
	for _, n := range all {
		byID[n.GetID()] = n
	}

	for _, n := range all {
		pos := n.GetPosition()
		if pos.Rgt <= pos.Lft {
			t.Errorf("node %v: Rgt (%d) must be > Lft (%d)", n.GetID(), pos.Rgt, pos.Lft)
		}

		if parentID := n.GetParentID(); parentID != nil {
			parent, ok := byID[*parentID]
			if !ok {
				t.Errorf("node %v: parent %v not found", n.GetID(), *parentID)
				continue
			}
			parentPos := parent.GetPosition()
			if !(parentPos.Lft < pos.Lft && pos.Rgt < parentPos.Rgt) {
				t.Errorf("node %v [%d,%d] must be strictly inside its parent %v [%d,%d]",
					n.GetID(), pos.Lft, pos.Rgt, *parentID, parentPos.Lft, parentPos.Rgt)
			}
			if pos.Depth != parentPos.Depth+1 {
				t.Errorf("node %v depth (%d) must be parent's depth (%d) + 1", n.GetID(), pos.Depth, parentPos.Depth)
			}
		}
	}

	// every Lft/Rgt value across the whole table must be unique (no overlaps).
	seen := make(map[int]bool, len(all)*2)
	for _, n := range all {
		pos := n.GetPosition()
		for _, v := range []int{pos.Lft, pos.Rgt} {
			if seen[v] {
				t.Errorf("duplicate lft/rgt value %d found across the tree", v)
			}
			seen[v] = true
		}
	}
}

func TestInvariants_HoldAfterCreateMoveDeleteSequence(t *testing.T) {
	db := testDB(t)
	t.Run("uuid", func(t *testing.T) {
		testInvariants_HoldAfterCreateMoveDeleteSequence[uuid.UUID](t, db, func(name string) *uuidCategory {
			return &uuidCategory{Name: name}
		})
	})
	t.Run("int64", func(t *testing.T) {
		testInvariants_HoldAfterCreateMoveDeleteSequence[int64](t, db, func(name string) *intCategory {
			return &intCategory{Name: name}
		})
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
