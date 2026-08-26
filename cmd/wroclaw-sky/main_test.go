package main

import (
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"

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

func TestRunWithLiveTokenAndAlerts(t *testing.T) {
	prevG, prevL := getenv, listenAndServe
	t.Cleanup(func() {
		getenv = prevG
		listenAndServe = prevL
	})
	db := t.TempDir() + "/trails.db"
	getenv = func(k string) string {
		switch k {
		case "LIVE_TOKEN":
			return "t"
		case "LIVE_COOKIE_HOURS":
			return "2"
		case "LIVE_AUTH_RPM":
			return "30"
		case "LIVE_COOKIE_SECURE":
			return "true"
		case "LIVE_COOKIE_SAMESITE":
			return "strict"
		case "ALERT_WEBHOOK_DIGEST":
			return "true"
		case "ALERT_WEBHOOK_URL":
			return "https://example.com/hook"
		case "APPROACH_RADIUS_KM":
			return "30"
		case "LOW_PASS_ALT_M":
			return "1200"
		case "FOCUS_RADIUS_KM":
			return "55"
		case "TRAILS_DB":
			return db
		default:
			return ""
		}
	}
	listenAndServe = func(string, http.Handler) error { return nil }
	if code := run(); code != 0 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunTrailsRedisWarn(t *testing.T) {
	prevG, prevL := getenv, listenAndServe
	t.Cleanup(func() {
		getenv = prevG
		listenAndServe = prevL
	})
	getenv = func(k string) string {
		if k == "TRAILS_REDIS_URL" {
			return "redis://127.0.0.1:1"
		}
		return ""
	}
	listenAndServe = func(string, http.Handler) error { return nil }
	if code := run(); code != 0 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunTrailsDBWarn(t *testing.T) {
	prevG, prevL := getenv, listenAndServe
	t.Cleanup(func() {
		getenv = prevG
		listenAndServe = prevL
	})
	dir := t.TempDir()
	getenv = func(k string) string {
		if k == "TRAILS_DB" {
			return dir // directory → OpenTrailsDB fails
		}
		return ""
	}
	listenAndServe = func(string, http.Handler) error { return nil }
	if code := run(); code != 0 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunTrailsRedisLoadWarn(t *testing.T) {
	prevG, prevL := getenv, listenAndServe
	t.Cleanup(func() {
		getenv = prevG
		listenAndServe = prevL
	})
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	if err := mr.Set("wroclaw-sky:trails", "{"); err != nil {
		t.Fatal(err)
	}
	getenv = func(k string) string {
		if k == "TRAILS_REDIS_URL" {
			return "redis://" + mr.Addr()
		}
		return ""
	}
	listenAndServe = func(string, http.Handler) error { return nil }
	if code := run(); code != 0 {
		t.Fatalf("code=%d", code)
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

func TestHealthcheck(t *testing.T) {
	prevG, prevH := getenv, httpGet
	t.Cleanup(func() {
		getenv = prevG
		httpGet = prevH
	})

	getenv = func(string) string { return "" }
	httpGet = func(url string) (*http.Response, error) {
		if url != "http://127.0.0.1:8081/healthz" {
			t.Fatalf("url=%s", url)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	}
	if code := healthcheck(); code != 0 {
		t.Fatalf("ok code=%d", code)
	}

	getenv = func(k string) string {
		if k == "PORT" {
			return "9999"
		}
		return ""
	}
	httpGet = func(url string) (*http.Response, error) {
		if url != "http://127.0.0.1:9999/healthz" {
			t.Fatalf("url=%s", url)
		}
		return nil, errors.New("down")
	}
	if code := healthcheck(); code != 1 {
		t.Fatalf("err code=%d", code)
	}

	httpGet = func(string) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody}, nil
	}
	if code := healthcheck(); code != 1 {
		t.Fatalf("503 code=%d", code)
	}
}

func TestMainHealthcheckArg(t *testing.T) {
	prevG, prevH, prevE, prevArgs := getenv, httpGet, exitFunc, os.Args
	t.Cleanup(func() {
		getenv = prevG
		httpGet = prevH
		exitFunc = prevE
		os.Args = prevArgs
	})
	os.Args = []string{"wroclaw-sky", "healthcheck"}
	getenv = func(string) string { return "" }
	httpGet = func(string) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	}
	var got int
	exitFunc = func(code int) { got = code }
	main()
	if got != 0 {
		t.Fatalf("exit=%d", got)
	}
}

func TestDefaultHTTPGet(t *testing.T) {
	// Hit a closed port so the real client path runs without needing a server.
	resp, err := defaultHTTPGet("http://127.0.0.1:1/")
	if err == nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		t.Fatal("expected connection error")
	}
}
