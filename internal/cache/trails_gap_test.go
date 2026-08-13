package cache

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"wroclaw-sky/internal/opensky"
)

type failRedis struct{ mode string }

func (f failRedis) Get(ctx context.Context, key string) (string, error) {
	switch f.mode {
	case "nil":
		return "", redis.Nil
	case "badjson":
		return "{", nil
	case "empty":
		return "", nil
	case "err":
		return "", errors.New("boom")
	default:
		return "", nil
	}
}

func (f failRedis) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return errors.New("set fail")
}

func TestTrailsStoreCoverageGaps(t *testing.T) {
	if _, err := OpenTrailsDB(""); err == nil {
		t.Fatal("empty")
	}
	if got, err := LoadTrailsDB(nil); err != nil || got != nil {
		t.Fatalf("%v %#v", err, got)
	}
	if err := SaveTrailsDB(nil, nil); err != nil {
		t.Fatal(err)
	}

	prevOpen := sqlOpen
	t.Cleanup(func() { sqlOpen = prevOpen })
	sqlOpen = func(string, string) (*sql.DB, error) { return nil, errors.New("open") }
	if _, err := OpenTrailsDB(filepath.Join(t.TempDir(), "x.db")); err == nil {
		t.Fatal("sqlOpen fail")
	}
	sqlOpen = prevOpen

	path := filepath.Join(t.TempDir(), "g.db")
	db, err := OpenTrailsDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// seed expired + empty + long + ok
	now := time.Now()
	long := make([]Point, maxTrailPoints+5)
	for i := range long {
		long[i] = Point{Lat: float64(i), Lon: 1, At: int64(i)}
	}
	rawLong, _ := marshalJSON(long)
	_, _ = db.Exec(`INSERT INTO trails(icao, points_json, seen_at) VALUES(?,?,?)`, "ok", `[{"lat":1,"lon":2,"at":1}]`, now.Unix())
	_, _ = db.Exec(`INSERT INTO trails(icao, points_json, seen_at) VALUES(?,?,?)`, "old", `[{"lat":1,"lon":2,"at":1}]`, now.Add(-time.Hour).Unix())
	_, _ = db.Exec(`INSERT INTO trails(icao, points_json, seen_at) VALUES(?,?,?)`, "bad", `{`, now.Unix())
	_, _ = db.Exec(`INSERT INTO trails(icao, points_json, seen_at) VALUES(?,?,?)`, "empty", `[]`, now.Unix())
	_, _ = db.Exec(`INSERT INTO trails(icao, points_json, seen_at) VALUES(?,?,?)`, "long", string(rawLong), now.Unix())

	loaded, err := LoadTrailsDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["ok"] == nil || loaded["old"] != nil || loaded["bad"] != nil || loaded["empty"] != nil {
		t.Fatalf("%#v", loaded)
	}
	if len(loaded["long"].Points) != maxTrailPoints {
		t.Fatalf("trim %d", len(loaded["long"].Points))
	}

	// Query fail (closed DB)
	closed, err := OpenTrailsDB(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = closed.Close()
	if _, err := LoadTrailsDB(closed); err == nil {
		t.Fatal("query fail")
	}
	if err := SaveTrailsDB(closed, map[string]*trailEntry{"x": {Points: []Point{{1, 2, 3}}, SeenAt: now}}); err == nil {
		t.Fatal("begin fail")
	}

	// Scan fail
	prevScan := scanTrailRow
	t.Cleanup(func() { scanTrailRow = prevScan })
	scanTrailRow = func(*sql.Rows, *string, *string, *int64) error { return errors.New("scan") }
	if _, err := LoadTrailsDB(db); err == nil {
		t.Fatal("scan fail")
	}
	scanTrailRow = prevScan

	// DELETE fail — read-only
	roPath := filepath.Join(t.TempDir(), "ro.db")
	wdb, err := OpenTrailsDB(roPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = wdb.Close()
	ro, err := sql.Open("sqlite", "file:"+roPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ro.Close() })
	if err := SaveTrailsDB(ro, map[string]*trailEntry{"x": {Points: []Point{{1, 2, 3}}, SeenAt: now}}); err == nil {
		t.Fatal("delete fail")
	}

	// Prepare fail
	prevPrep := txPrepare
	t.Cleanup(func() { txPrepare = prevPrep })
	txPrepare = func(*sql.Tx, string) (*sql.Stmt, error) { return nil, errors.New("prep") }
	if err := SaveTrailsDB(db, map[string]*trailEntry{"x": {Points: []Point{{1, 2, 3}}, SeenAt: now}}); err == nil {
		t.Fatal("prepare fail")
	}
	txPrepare = prevPrep

	// Exec fail — trigger
	_, _ = db.Exec(`CREATE TRIGGER fail_ins BEFORE INSERT ON trails BEGIN SELECT RAISE(FAIL, 'nope'); END`)
	if err := SaveTrailsDB(db, map[string]*trailEntry{"x": {Points: []Point{{1, 2, 3}}, SeenAt: now}}); err == nil {
		t.Fatal("exec fail")
	}
	_, _ = db.Exec(`DROP TRIGGER fail_ins`)

	in := map[string]*trailEntry{
		"nil":   nil,
		"empty": {Points: nil, SeenAt: now},
		"old":   {Points: []Point{{1, 2, 3}}, SeenAt: now.Add(-time.Hour)},
		"ok":    {Points: []Point{{1, 2, 3}}, SeenAt: now},
	}
	if err := SaveTrailsDB(db, in); err != nil {
		t.Fatal(err)
	}

	prevM := marshalJSON
	t.Cleanup(func() { marshalJSON = prevM })
	marshalJSON = func(any) ([]byte, error) { return nil, errors.New("m") }
	if err := SaveTrailsDB(db, map[string]*trailEntry{"x": {Points: []Point{{1, 2, 3}}, SeenAt: now}}); err == nil {
		t.Fatal("marshal fail")
	}
	marshalJSON = prevM

	s := New(nil, opensky.Wroclaw)
	if err := s.SetTrailsDB(path); err != nil {
		t.Fatal(err)
	}
	// close + reopen path covering previous close
	if err := s.SetTrailsDB(path); err != nil {
		t.Fatal(err)
	}
	s2 := &Store{}
	if err := s2.SetTrailsDB(path); err != nil {
		t.Fatal(err)
	}
	if err := s2.SetTrailsDB(t.TempDir()); err == nil {
		t.Fatal("openTrailsDB fail on directory")
	}
	prevLoad := loadTrailsDB
	t.Cleanup(func() { loadTrailsDB = prevLoad })
	loadTrailsDB = func(*sql.DB) (map[string]*trailEntry, error) { return nil, errors.New("load") }
	if err := s2.SetTrailsDB(filepath.Join(t.TempDir(), "load.db")); err == nil {
		t.Fatal("load fail")
	}
	loadTrailsDB = prevLoad

	// redis branches
	s3 := New(nil, opensky.Wroclaw)
	if err := s3.SetTrailsRedis(failRedis{mode: "nil"}); err != nil {
		t.Fatal(err)
	}
	if err := s3.SetTrailsRedis(failRedis{mode: "empty"}); err != nil {
		t.Fatal(err)
	}
	if err := s3.SetTrailsRedis(failRedis{mode: "badjson"}); err == nil {
		t.Fatal("bad json")
	}
	if err := s3.SetTrailsRedis(failRedis{mode: "err"}); err == nil {
		t.Fatal("get err")
	}

	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	blob, _ := marshalJSON(map[string]trailFileEntry{
		"e":    {Points: nil, SeenAt: now},
		"o":    {Points: []Point{{1, 2, 3}}, SeenAt: now.Add(-time.Hour)},
		"ok":   {Points: []Point{{1, 2, 3}, {2, 3, 4}}, SeenAt: now},
		"long": {Points: long, SeenAt: now},
	})
	_ = rc.Set(context.Background(), trailsRedisKey, string(blob), 0).Err()
	s4 := &Store{}
	if err := s4.SetTrailsRedis(&RedisTrails{Client: rc}); err != nil {
		t.Fatal(err)
	}
	if s4.trails["ok"] == nil || len(s4.trails["long"].Points) != maxTrailPoints {
		t.Fatalf("%#v", s4.trails)
	}

	s5 := New(nil, opensky.Wroclaw)
	_ = s5.SetTrailsRedis(failRedis{mode: "nil"})
	s5.trails = map[string]*trailEntry{
		"a": {Points: []Point{{1, 2, 3}}, SeenAt: now},
		"b": nil,
		"c": {Points: []Point{{1, 2, 3}}, SeenAt: now.Add(-time.Hour)},
	}
	s5.persistTrailsRedisLocked() // set fails — covered
	prevM = marshalJSON
	marshalJSON = func(any) ([]byte, error) { return nil, errors.New("m") }
	s5.persistTrailsRedisLocked()
	marshalJSON = prevM
	s5.trailsRedis = nil
	s5.persistTrailsRedisLocked()

	emptyClient := &RedisTrails{}
	if _, err := emptyClient.Get(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
	if err := emptyClient.Set(context.Background(), "k", "v", time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestOpenTrailsDBExecFail(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenTrailsDB(dir); err == nil {
		t.Fatal("expected create fail on directory")
	}
}
