package datatables_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/netgarden/maf/datatables"

	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

type item struct {
	ID       uint `gorm:"primaryKey"`
	Name     string
	Hostname string
	Score    int
	Active   bool
}

func (item) TableName() string { return "datatables_test_items" }

var itemColumns = []datatables.Column{
	{Name: "name", DBColumn: "name", Sortable: true, FilterOperator: datatables.Contains},
	{Name: "hostname", DBColumn: "hostname", Sortable: true, FilterOperator: datatables.Contains},
	{Name: "score", DBColumn: "score", Sortable: true, FilterOperator: datatables.Eq},
	{Name: "active", DBColumn: "active", Sortable: true},
}

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

	if err := db.AutoMigrate(&item{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	reset := func() {
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&item{})
	}
	reset()
	t.Cleanup(reset)

	return db
}

func seed(t *testing.T, db *gorm.DB, items ...item) {
	t.Helper()
	for i := range items {
		if err := db.Create(&items[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// --- Pagination ---

func TestApply_PaginationBounds(t *testing.T) {
	db := testDB(t)
	seed(t, db,
		item{Name: "a", Hostname: "a.example.com"},
		item{Name: "b", Hostname: "b.example.com"},
		item{Name: "c", Hostname: "c.example.com"},
		item{Name: "d", Hostname: "d.example.com"},
		item{Name: "e", Hostname: "e.example.com"},
	)

	res, err := datatables.Apply[item](db.Model(&item{}).Order("name"), itemColumns, datatables.Query{Page: 0, PageSize: 2})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.TotalCount != 5 {
		t.Errorf("expected TotalCount 5, got %d", res.TotalCount)
	}
	if len(res.Items) != 2 || res.Items[0].Name != "a" || res.Items[1].Name != "b" {
		t.Errorf("page 0: got %+v", res.Items)
	}

	res, err = datatables.Apply[item](db.Model(&item{}).Order("name"), itemColumns, datatables.Query{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Items) != 2 || res.Items[0].Name != "c" || res.Items[1].Name != "d" {
		t.Errorf("page 1: got %+v", res.Items)
	}

	res, err = datatables.Apply[item](db.Model(&item{}).Order("name"), itemColumns, datatables.Query{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].Name != "e" {
		t.Errorf("page 2 (partial): got %+v", res.Items)
	}
}

func TestApply_PageSizeDefaultAndClamping(t *testing.T) {
	db := testDB(t)
	items := make([]item, 0, 150)
	for i := 0; i < 150; i++ {
		items = append(items, item{Name: "n"})
	}
	seed(t, db, items...)

	res, err := datatables.Apply[item](db.Model(&item{}), itemColumns, datatables.Query{PageSize: 0})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Items) != datatables.DefaultPageSize {
		t.Errorf("expected default page size %d, got %d", datatables.DefaultPageSize, len(res.Items))
	}

	res, err = datatables.Apply[item](db.Model(&item{}), itemColumns, datatables.Query{PageSize: 1000})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Items) != datatables.MaxPageSize {
		t.Errorf("expected page size clamped to %d, got %d", datatables.MaxPageSize, len(res.Items))
	}
}

// --- Sorting ---

func TestApply_SortAscDesc(t *testing.T) {
	db := testDB(t)
	seed(t, db, item{Name: "banana"}, item{Name: "apple"}, item{Name: "cherry"})

	res, err := datatables.Apply[item](db.Model(&item{}), itemColumns, datatables.Query{SortBy: "name", SortDir: datatables.Asc, PageSize: 10})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	gotAsc := []string{res.Items[0].Name, res.Items[1].Name, res.Items[2].Name}
	wantAsc := []string{"apple", "banana", "cherry"}
	for i := range wantAsc {
		if gotAsc[i] != wantAsc[i] {
			t.Errorf("asc order = %v, want %v", gotAsc, wantAsc)
			break
		}
	}

	res, err = datatables.Apply[item](db.Model(&item{}), itemColumns, datatables.Query{SortBy: "name", SortDir: datatables.Desc, PageSize: 10})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	gotDesc := []string{res.Items[0].Name, res.Items[1].Name, res.Items[2].Name}
	wantDesc := []string{"cherry", "banana", "apple"}
	for i := range wantDesc {
		if gotDesc[i] != wantDesc[i] {
			t.Errorf("desc order = %v, want %v", gotDesc, wantDesc)
			break
		}
	}
}

func TestApply_RejectsUnknownOrNonSortableColumn(t *testing.T) {
	db := testDB(t)
	seed(t, db, item{Name: "a"})

	_, err := datatables.Apply[item](db.Model(&item{}), itemColumns, datatables.Query{SortBy: "does_not_exist"})
	if !errors.Is(err, datatables.ErrUnknownSortColumn) {
		t.Errorf("expected ErrUnknownSortColumn, got %v", err)
	}
}

// --- Filtering ---

func TestApply_ContainsOperator(t *testing.T) {
	db := testDB(t)
	seed(t, db, item{Name: "web-1"}, item{Name: "web-2"}, item{Name: "db-1"})

	res, err := datatables.Apply[item](db.Model(&item{}), itemColumns, datatables.Query{Filters: map[string]string{"name": "web"}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.TotalCount != 2 {
		t.Errorf("expected 2 matches for Contains \"web\", got %d", res.TotalCount)
	}
}

func TestApply_ContainsEscapesLikeWildcards(t *testing.T) {
	db := testDB(t)
	seed(t, db, item{Name: "100% done"}, item{Name: "100 done"}, item{Name: "anything"})

	res, err := datatables.Apply[item](db.Model(&item{}), itemColumns, datatables.Query{Filters: map[string]string{"name": "100%"}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.TotalCount != 1 {
		t.Errorf("expected literal \"100%%\" to match exactly 1 row, got %d (TotalCount)", res.TotalCount)
	}
	if len(res.Items) != 1 || res.Items[0].Name != "100% done" {
		t.Errorf("got %+v", res.Items)
	}
}

func TestApply_EqOperator(t *testing.T) {
	db := testDB(t)
	seed(t, db, item{Name: "a", Score: 10}, item{Name: "b", Score: 20})

	res, err := datatables.Apply[item](db.Model(&item{}), itemColumns, datatables.Query{Filters: map[string]string{"score": "10"}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.TotalCount != 1 || res.Items[0].Name != "a" {
		t.Errorf("got count=%d items=%+v", res.TotalCount, res.Items)
	}
}

func TestApply_ComparisonOperators(t *testing.T) {
	db := testDB(t)
	seed(t, db, item{Name: "a", Score: 10}, item{Name: "b", Score: 20}, item{Name: "c", Score: 30})

	cases := []struct {
		op   string
		val  string
		want int64
	}{
		{"gt", "10", 2},
		{"gte", "10", 3},
		{"lt", "30", 2},
		{"lte", "30", 3},
	}

	// Reuse Score's Eq operator by swapping in each comparison operator for
	// this test only, since itemColumns fixes score's operator to Eq.
	for _, c := range cases {
		cols := []datatables.Column{
			{Name: "score", DBColumn: "score", Sortable: true, FilterOperator: datatables.Operator(c.op)},
		}
		res, err := datatables.Apply[item](db.Model(&item{}), cols, datatables.Query{Filters: map[string]string{"score": c.val}})
		if err != nil {
			t.Fatalf("Apply(%s %s): %v", c.op, c.val, err)
		}
		if res.TotalCount != c.want {
			t.Errorf("%s %s: TotalCount = %d, want %d", c.op, c.val, res.TotalCount, c.want)
		}
	}
}

func TestApply_RejectsUnknownFilterColumn(t *testing.T) {
	db := testDB(t)
	seed(t, db, item{Name: "a"})

	_, err := datatables.Apply[item](db.Model(&item{}), itemColumns, datatables.Query{Filters: map[string]string{"does_not_exist": "x"}})
	if !errors.Is(err, datatables.ErrUnknownFilterColumn) {
		t.Errorf("expected ErrUnknownFilterColumn, got %v", err)
	}

	// "active" is sortable but not filterable (no FilterOperator) — must
	// also be rejected as a filter key, not silently ignored.
	_, err = datatables.Apply[item](db.Model(&item{}), itemColumns, datatables.Query{Filters: map[string]string{"active": "true"}})
	if !errors.Is(err, datatables.ErrUnknownFilterColumn) {
		t.Errorf("expected ErrUnknownFilterColumn for non-filterable column, got %v", err)
	}
}

func TestApply_TotalCountReflectsFiltersNotPagination(t *testing.T) {
	db := testDB(t)
	for i := 0; i < 10; i++ {
		seed(t, db, item{Name: "web-host"})
	}
	seed(t, db, item{Name: "db-host"})

	res, err := datatables.Apply[item](db.Model(&item{}), itemColumns, datatables.Query{
		Filters:  map[string]string{"name": "web"},
		Page:     0,
		PageSize: 3,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.TotalCount != 10 {
		t.Errorf("expected TotalCount 10 (independent of PageSize), got %d", res.TotalCount)
	}
	if len(res.Items) != 3 {
		t.Errorf("expected 3 items on this page, got %d", len(res.Items))
	}
}
