package store

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
)

func openSQLiteConnections(filename string, readOnly bool) (*sql.DB, error) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	path := filepath.ToSlash(absolute)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	dsn := url.URL{Scheme: "file", Path: path}
	options := url.Values{}
	options.Add("_pragma", "busy_timeout(5000)")
	maxConnections := 1
	if readOnly {
		options.Set("mode", "ro")
		maxConnections = 4
	} else {
		// Keep writes serialized and durable. WAL lets short node reads proceed
		// alongside those writes, without caching ownership or revocation state.
		options.Add("_pragma", "journal_mode(WAL)")
		options.Add("_pragma", "synchronous(FULL)")
	}
	dsn.RawQuery = options.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(maxConnections)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
