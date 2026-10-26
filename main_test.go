package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPAssertions(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201); w.Write([]byte("synthetic ready")) }))
	defer s.Close()
	d := Deck{Checks: []Check{{"pass", s.URL, 201, "ready", 1000}, {"status", s.URL, 200, "", 1000}, {"body", s.URL, 201, "missing", 1000}}}
	r := runDeck(context.Background(), d)
	if r.Passed || !r.Results[0].Passed || r.Results[1].Passed || r.Results[2].Passed {
		t.Fatalf("unexpected report: %+v", r)
	}
}
func TestRedirectNotFollowed(t *testing.T) {
	hits := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer destination.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer s.Close()
	r := runDeck(context.Background(), Deck{[]Check{{"redirect", s.URL, 302, "", 1000}}})
	if !r.Passed || hits != 0 {
		t.Fatalf("redirect followed or assertion failed: %+v", r)
	}
}
func TestTimeoutAndOversizedBody(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(100 * time.Millisecond)
		} else {
			w.Write([]byte(strings.Repeat("a", maxBytes+1)))
		}
	}))
	defer s.Close()
	r := runDeck(context.Background(), Deck{[]Check{{"slow", s.URL + "/slow", 200, "", 10}, {"large", s.URL, 200, "", 1000}}})
	if r.Passed || r.Results[0].Passed || r.Results[1].Passed {
		t.Fatalf("unexpected report: %+v", r)
	}
}
func TestValidation(t *testing.T) {
	valid := `{"checks":[{"name":"a","url":"http://127.0.0.1:8080/","status":200,"timeout_ms":1000}]}`
	if _, err := readDeck(strings.NewReader(valid)); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`{}`, valid + `{}`, strings.Replace(valid, "127.0.0.1", "example.com", 1), strings.Replace(valid, "http:", "https:", 1), strings.Replace(valid, "1000", "0", 1), strings.Replace(valid, "200", "600", 1), strings.Replace(valid, `"name"`, `"unknown"`, 1), strings.Repeat(" ", maxBytes+1)} {
		if _, err := readDeck(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted invalid deck %.100s", input)
		}
	}
}
func TestCLI(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "deck.json")
	for _, status := range []int{200, 404} {
		b, _ := json.Marshal(Deck{[]Check{{"demo", s.URL, status, "ok", 1000}}})
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		var out, errs bytes.Buffer
		code := cli([]string{"run", path}, &out, &errs)
		expected := 0
		if status == 404 {
			expected = 1
		}
		if code != expected {
			t.Fatalf("code %d: %s", code, errs.String())
		}
		var r Report
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
	}
	var out, errs bytes.Buffer
	if cli(nil, &out, &errs) != 2 || cli([]string{"run", path + "missing"}, &out, &errs) != 2 {
		t.Fatal("expected usage errors")
	}
}
