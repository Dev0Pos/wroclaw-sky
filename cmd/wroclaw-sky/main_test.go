package main

import (
	"errors"
	"net/http"
	"os"
	"testing"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/server"
)

func TestRunTrailsFileWarn(t *testing.T) {
	prevG, prevL := getenv, listenAndServe
	t.Cleanup(func() {
		getenv = prevG
		listenAndServe = prevL
	})
	bad := t.TempDir() + "/bad.json"
	if err := os.WriteFile(bad, []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	getenv = func(k string) string {
		if k == "TRAILS_FILE" {
			return bad
		}
		return ""
	}
	listenAndServe = func(string, http.Handler) error { return nil }
	if code := run(); code != 0 {
		t.Fatalf("code = %d", code)
	}
}

func TestRunBadBBox(t *testing.T) {
	prev := getenv
	t.Cleanup(func() { getenv = prev })
	getenv = func(k string) string {
		if k == "OPENSKY_BBOX" {
			return "bad"
		}
		return ""
	}
	if code := run(); code != 1 {
		t.Fatalf("code = %d", code)
	}
}

func TestRunListenOKAndMapLabel(t *testing.T) {
	prevG, prevL := getenv, listenAndServe
	t.Cleanup(func() {
		getenv = prevG
		listenAndServe = prevL
	})
	getenv = func(k string) string {
		switch k {
		case "PORT":
			return "9999"
		case "MAP_LABEL":
			return "TEST LABEL"
		case "OPENSKY_USER":
			return "u"
		default:
			return ""
		}
	}
	var gotAddr string
	listenAndServe = func(addr string, h http.Handler) error {
		gotAddr = addr
		if h == nil {
			t.Fatal("nil handler")
		}
		return nil
	}
	if code := run(); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if gotAddr != ":9999" {
		t.Fatalf("addr = %q", gotAddr)
	}
}

func TestRunListenError(t *testing.T) {
	prevG, prevL := getenv, listenAndServe
	t.Cleanup(func() {
		getenv = prevG
		listenAndServe = prevL
	})
	getenv = func(string) string { return "" }
	listenAndServe = func(string, http.Handler) error {
		return errors.New("bind failed")
	}
	if code := run(); code != 1 {
		t.Fatalf("code = %d", code)
	}
}

func TestRunServerNewError(t *testing.T) {
	prevG, prevN := getenv, newServer
	t.Cleanup(func() {
		getenv = prevG
		newServer = prevN
	})
	getenv = func(string) string { return "" }
	newServer = func(*cache.Store, *meta.Enricher) (*server.Server, error) {
		return nil, errors.New("tmpl")
	}
	if code := run(); code != 1 {
		t.Fatalf("code = %d", code)
	}
}

func TestMainCallsExit(t *testing.T) {
	prevG, prevL, prevE := getenv, listenAndServe, exitFunc
	t.Cleanup(func() {
		getenv = prevG
		listenAndServe = prevL
		exitFunc = prevE
	})
	getenv = func(string) string { return "" }
	listenAndServe = func(string, http.Handler) error { return nil }
	var got int
	exitFunc = func(code int) { got = code }
	main()
	if got != 0 {
		t.Fatalf("exit = %d", got)
	}
}
