// Package apitest holds characterization tests for the subscriptions HTTP API.
// They run the real router against a real PostgreSQL instance so that the
// recorded responses in testdata/ describe what the service actually does
// today, not what a mock says it does.
//
// The routes covered here are the ones other services call: app-exposer checks
// overages before launching an analysis, resource-usage-api and data-usage-api
// record usage, and terrain manages subscription add-ons. Those responses are a
// wire contract, and this service is the one that survives the QMS merge, so
// the goldens exist to make a change to them deliberate rather than incidental.
package apitest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cyverse-de/subscriptions/app"
	"github.com/cyverse-de/subscriptions/migrations"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	_ "github.com/lib/pq"
)

// UsernameSuffix is the suffix the test server is configured with. Note that
// App.FixUsername strips everything after an "@" with a regexp rather than
// trimming this value, so the two only agree by convention — a difference from
// QMS worth keeping visible while the two services are being merged.
const UsernameSuffix = "@example.org"

// Extensions required by the schema. All four ship with the stock postgres
// image; insert_username and moddatetime come from the contrib spi module and
// back the audit-column triggers the migrations install.
var requiredExtensions = []string{"uuid-ossp", "moddatetime", "btree_gist", "insert_username"}

// Tables holding test-created rows. The reference tables (plans,
// resource_types, update_operations, plan_quota_defaults, plan_rates, addons,
// addon_rates) are seeded by the migrations with fixed UUIDs and must survive a
// reset, because the golden files assert those UUIDs exactly.
var mutableTables = []string{
	"subscription_addons",
	"updates",
	"usages",
	"quotas",
	"subscriptions",
	"users",
}

var (
	testDB     *sqlx.DB
	testRouter *echo.Echo
)

func TestMain(m *testing.M) {
	os.Exit(runSuite(m))
}

// runSuite owns the container lifecycle so that deferred cleanup still runs;
// os.Exit in TestMain would skip it.
func runSuite(m *testing.M) int {
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:17",
		postgres.WithDatabase("qms"),
		postgres.WithUsername("qms"),
		postgres.WithPassword("notprod"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		// Fail loudly rather than skipping: a suite that silently skips when
		// Docker is unavailable is a suite that never runs.
		log.Fatalf("unable to start the postgres container (is Docker running?): %s", err)
	}
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			log.Printf("unable to terminate the postgres container: %s", err)
		}
	}()

	databaseURI, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("unable to determine the database connection string: %s", err)
	}

	if err = installExtensions(databaseURI); err != nil {
		log.Fatalf("unable to install the required extensions: %s", err)
	}

	if err = runMigrations(databaseURI); err != nil {
		log.Fatalf("unable to run the schema migrations: %s", err)
	}

	testDB, err = sqlx.Connect("postgres", databaseURI)
	if err != nil {
		log.Fatalf("unable to connect to the database: %s", err)
	}
	defer func() { _ = testDB.Close() }()

	// Overage reporting is on, which is how the service is deployed; the
	// existing unit tests only cover the short circuit when it's off.
	//
	// The two route trees are wired exactly as main() wires them, including the
	// suffix discrepancy: New gets the bare domain from users.domain, while the
	// QMS handlers get it with the leading "@" they trim with.
	a := app.New(testDB, strings.TrimPrefix(UsernameSuffix, "@"), true)
	a.RegisterQMSAPI(UsernameSuffix)
	testRouter = a.Router

	return m.Run()
}

func installExtensions(databaseURI string) error {
	conn, err := sqlx.Connect("postgres", databaseURI)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	for _, extension := range requiredExtensions {
		if _, err = conn.Exec(fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %q", extension)); err != nil {
			return fmt.Errorf("creating extension %s: %w", extension, err)
		}
	}
	return nil
}

func runMigrations(databaseURI string) error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return err
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURI)
	if err != nil {
		return err
	}

	if err = m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

// resetDB empties the tables tests write to, leaving migration-seeded reference
// data in place.
func resetDB(t *testing.T) {
	t.Helper()
	for _, table := range mutableTables {
		if _, err := testDB.Exec(fmt.Sprintf("DELETE FROM %q", table)); err != nil {
			t.Fatalf("unable to clear the %s table: %s", table, err)
		}
	}
}

// response is the outcome of one request through the router.
type response struct {
	status int
	body   []byte
}

// do sends a request through the real router. body may be empty.
func do(t *testing.T, method, path, body string) response {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}

	rec := httptest.NewRecorder()
	testRouter.ServeHTTP(rec, req)

	return response{status: rec.Code, body: rec.Body.Bytes()}
}

var (
	uuidPattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
)

// seededIDs are the fixed UUIDs the migrations insert. They are asserted
// exactly rather than redacted, so a golden file still pins which plan or
// resource type a response refers to.
var seededIDs = map[string]bool{
	"99e47c22-950a-11ec-84a4-406c8f3e9cbb": true, // plan: Basic
	"c6d39580-98dc-11ec-bbe3-406c8f3e9cbb": true, // plan: Regular
	"cdf7ac7a-98dc-11ec-bbe3-406c8f3e9cbb": true, // plan: Pro
	"d80b5482-98dc-11ec-bbe3-406c8f3e9cbb": true, // plan: Commercial
	"99e3bc7e-950a-11ec-84a4-406c8f3e9cbb": true, // resource type: cpu.hours
	"99e3f91e-950a-11ec-84a4-406c8f3e9cbb": true, // resource type: data.size
	"f1f9df66-9676-11ec-aa73-406c8f3e9cbb": true, // update operation: ADD
	"f1fa4a1e-9676-11ec-aa73-406c8f3e9cbb": true, // update operation: SET
}

// unorderedFields name response fields whose queries carry no ORDER BY, so the
// database returns their rows in arbitrary order. Their contents are sorted
// before comparison. Arrays reached by any other route keep their order and are
// still verified, so a broken sort would fail.
var unorderedFields = map[string]bool{
	"quotas":              true,
	"usages":              true,
	"plan_quota_defaults": true,
	"plan_rates":          true,
	"overages":            true,
}

// redact replaces database-generated UUIDs and timestamps with placeholders so
// goldens are stable across runs. Everything else is compared verbatim.
func redact(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			nested = redact(nested)
			if list, isList := nested.([]any); isList && unorderedFields[key] {
				sortByContent(list)
			}
			typed[key] = nested
		}
		return typed
	case []any:
		for i, nested := range typed {
			typed[i] = redact(nested)
		}
		return typed
	case string:
		switch {
		case uuidPattern.MatchString(typed) && !seededIDs[typed]:
			return "<uuid>"
		case timestampPattern.MatchString(typed):
			return "<timestamp>"
		}
		return typed
	default:
		return value
	}
}

// sortByContent orders elements by their encoded form, giving an arbitrary but
// reproducible order.
func sortByContent(list []any) {
	keys := make(map[int]string, len(list))
	for i, element := range list {
		encoded, err := json.Marshal(element)
		if err != nil {
			return
		}
		keys[i] = string(encoded)
	}
	sort.SliceStable(list, func(i, j int) bool { return keys[i] < keys[j] })
}

// encodeGolden renders a redacted response the way golden files store it.
// json.Marshal's HTML escaping is disabled so the placeholders stay readable.
func encodeGolden(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buffer.Bytes()), nil
}

// assertGolden compares a response against testdata/<name>.json after redaction.
//
// A missing golden is written and the test still fails, so a new baseline has to
// be read and committed deliberately. An existing golden is never overwritten:
// there is no regeneration flag, because one would let a genuine behavior change
// be erased instead of explained.
func assertGolden(t *testing.T, name string, got response, wantStatus int) {
	t.Helper()

	if got.status != wantStatus {
		t.Errorf("status = %d, want %d; body: %s", got.status, wantStatus, got.body)
	}

	var decoded any
	if err := json.Unmarshal(got.body, &decoded); err != nil {
		t.Fatalf("response body is not valid JSON: %s; body: %s", err, got.body)
	}

	actual, err := encodeGolden(redact(decoded))
	if err != nil {
		t.Fatalf("unable to re-encode the response: %s", err)
	}

	path := fmt.Sprintf("testdata/%s.json", name)
	expected, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err = os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("unable to create the testdata directory: %s", err)
		}
		if err = os.WriteFile(path, append(actual, '\n'), 0o644); err != nil {
			t.Fatalf("unable to write the golden file %s: %s", path, err)
		}
		t.Fatalf("golden file %s did not exist and has been created; review it and re-run", path)
	}
	if err != nil {
		t.Fatalf("unable to read the golden file %s: %s", path, err)
	}

	if !bytes.Equal(bytes.TrimSpace(expected), actual) {
		t.Errorf("response does not match %s\n--- want ---\n%s\n--- got ---\n%s", path, bytes.TrimSpace(expected), actual)
	}
}

// mustDecode unmarshals a response body, failing the test if it isn't JSON.
func mustDecode(t *testing.T, got response) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(got.body, &decoded); err != nil {
		t.Fatalf("response body is not valid JSON: %s; body: %s", err, got.body)
	}
	return decoded
}
