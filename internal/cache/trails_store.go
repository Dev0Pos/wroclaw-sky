package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

// Overridable in tests.
var (
	sqlOpen         = sql.Open
	loadTrailsDB    = LoadTrailsDB
	openTrailsDB    = OpenTrailsDB
	insertTrailsSQL = `INSERT INTO trails(icao, points_json, seen_at) VALUES(?,?,?)`
	scanTrailRow    = func(rows *sql.Rows, icao, raw *string, seen *int64) error {
		return rows.Scan(icao, raw, seen)
	}
	txPrepare = func(tx *sql.Tx, query string) (*sql.Stmt, error) {
		return tx.Prepare(query)
	}
)

// OpenTrailsDB opens (or creates) a SQLite trails database at path.
func OpenTrailsDB(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("trails db path required")
	}
	db, err := sqlOpen("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS trails (
  icao TEXT PRIMARY KEY,
  points_json TEXT NOT NULL,
  seen_at INTEGER NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// LoadTrailsDB loads non-expired trails from SQLite.
func LoadTrailsDB(db *sql.DB) (map[string]*trailEntry, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.Query(`SELECT icao, points_json, seen_at FROM trails`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	now := time.Now()
	out := make(map[string]*trailEntry)
	for rows.Next() {
		var icao, raw string
		var seenUnix int64
		if err := scanTrailRow(rows, &icao, &raw, &seenUnix); err != nil {
			return nil, err
		}
		seen := time.Unix(seenUnix, 0)
		if now.Sub(seen) > trailGrace {
			continue
		}
		var pts []Point
		if err := json.Unmarshal([]byte(raw), &pts); err != nil || len(pts) == 0 {
			continue
		}
		if len(pts) > maxTrailPoints {
			pts = pts[len(pts)-maxTrailPoints:]
		}
		out[icao] = &trailEntry{Points: pts, SeenAt: seen}
	}
	return out, rows.Err()
}

// SaveTrailsDB replaces all trails in SQLite (best-effort prune of expired).
func SaveTrailsDB(db *sql.DB, trails map[string]*trailEntry) error {
	if db == nil {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM trails`); err != nil {
		return err
	}
	now := time.Now()
	stmt, err := txPrepare(tx, insertTrailsSQL)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for k, ent := range trails {
		if ent == nil || len(ent.Points) == 0 {
			continue
		}
		if now.Sub(ent.SeenAt) > trailGrace {
			continue
		}
		raw, err := marshalJSON(ent.Points)
		if err != nil {
			return err
		}
		if _, err := stmt.Exec(k, string(raw), ent.SeenAt.Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetTrailsDB enables SQLite persistence. Closes any previous DB handle.
func (s *Store) SetTrailsDB(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trailsDB != nil {
		_ = s.trailsDB.Close()
		s.trailsDB = nil
	}
	s.trailsDBPath = path
	if path == "" {
		return nil
	}
	db, err := openTrailsDB(path)
	if err != nil {
		return err
	}
	loaded, err := loadTrailsDB(db)
	if err != nil {
		_ = db.Close()
		return err
	}
	s.trailsDB = db
	if s.trails == nil {
		s.trails = make(map[string]*trailEntry)
	}
	for k, v := range loaded {
		s.trails[k] = v
	}
	return nil
}

// TrailsRedis is a minimal Redis GET/SET client for trail blobs.
type TrailsRedis interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
}

const trailsRedisKey = "wroclaw-sky:trails"

// SetTrailsRedis enables Redis persistence of the trails map as one JSON blob.
func (s *Store) SetTrailsRedis(r TrailsRedis) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trailsRedis = r
	if r == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := r.Get(ctx, trailsRedisKey)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil
		}
		return err
	}
	if raw == "" {
		return nil
	}
	var data map[string]trailFileEntry
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return err
	}
	now := time.Now()
	if s.trails == nil {
		s.trails = make(map[string]*trailEntry)
	}
	for k, v := range data {
		if len(v.Points) == 0 || now.Sub(v.SeenAt) > trailGrace {
			continue
		}
		pts := append([]Point(nil), v.Points...)
		if len(pts) > maxTrailPoints {
			pts = pts[len(pts)-maxTrailPoints:]
		}
		s.trails[k] = &trailEntry{Points: pts, SeenAt: v.SeenAt}
	}
	return nil
}

func (s *Store) persistTrailsRedisLocked() {
	if s.trailsRedis == nil {
		return
	}
	data := make(map[string]trailFileEntry, len(s.trails))
	now := time.Now()
	for k, ent := range s.trails {
		if ent == nil || len(ent.Points) == 0 || now.Sub(ent.SeenAt) > trailGrace {
			continue
		}
		data[k] = trailFileEntry{Points: ent.Points, SeenAt: ent.SeenAt}
	}
	raw, err := marshalJSON(data)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.trailsRedis.Set(ctx, trailsRedisKey, string(raw), trailGrace)
}
