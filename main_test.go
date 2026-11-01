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
	d := Deck{Checks: []Check{{Name: "pass", URL: s.URL, Status: 201, Contains: "ready", TimeoutMS: 1000}, {Name: "status", URL: s.URL, Status: 200, Contains: "", TimeoutMS: 1000}, {Name: "body", URL: s.URL, Status: 201, Contains: "missing", TimeoutMS: 1000}}}
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
	r := runDeck(context.Background(), Deck{[]Check{{Name: "redirect", URL: s.URL, Status: 302, Contains: "", TimeoutMS: 1000}}})
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
	r := runDeck(context.Background(), Deck{[]Check{{Name: "slow", URL: s.URL + "/slow", Status: 200, Contains: "", TimeoutMS: 10}, {Name: "large", URL: s.URL, Status: 200, Contains: "", TimeoutMS: 1000}}})
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
		b, _ := json.Marshal(Deck{[]Check{{Name: "demo", URL: s.URL, Status: status, Contains: "ok", TimeoutMS: 1000}}})
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

func TestStructuredAssertions(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-State", "ready")
		w.Write([]byte(`{"items":[{"a/b":null}],"count":9007199254740993,"flag":true}`))
	}))
	defer s.Close()
	c := Check{Name: "json", URL: s.URL, Status: 200, TimeoutMS: 1000, Headers: map[string]string{"x-state": "ready"}, JSON: []JSONAssertion{
		{"/items/0/a~1b", json.RawMessage(`null`)}, {"/count", json.RawMessage(`9007199254740993`)}, {"/flag", json.RawMessage(`true`)},
	}}
	if r := runDeck(context.Background(), Deck{[]Check{c}}); !r.Passed {
		t.Fatalf("%+v", r)
	}
	for _, a := range []JSONAssertion{{"/missing", json.RawMessage(`null`)}, {"/items/00", json.RawMessage(`null`)}, {"/flag", json.RawMessage(`"true"`)}, {"/count", json.RawMessage(`9007199254740992`)}} {
		c.JSON = []JSONAssertion{a}
		if r := runDeck(context.Background(), Deck{[]Check{c}}); r.Passed {
			t.Fatalf("unexpected pass: %+v", a)
		}
	}
}
func TestStructuredEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		body, pointer, expected string
		pass                    bool
	}{
		{`null`, "", `null`, true}, {`{"~":1}`, "/~0", `1`, true},
		{`{"":false}`, "/", `false`, true}, {`[]`, "/0", `null`, false},
		{`{} {}`, "", `{}`, false}, {`bad`, "", `null`, false},
	} {
		problems := assertStructured(Check{JSON: []JSONAssertion{{tc.pointer, json.RawMessage(tc.expected)}}}, http.Header{}, []byte(tc.body))
		if (len(problems) == 0) != tc.pass {
			t.Fatalf("%+v: %v", tc, problems)
		}
	}
	c := Check{Headers: map[string]string{"X-Value": ""}}
	if len(assertStructured(c, http.Header{}, nil)) == 0 {
		t.Fatal("missing header passed")
	}
	if len(assertStructured(c, http.Header{"X-Value": []string{""}}, nil)) != 0 {
		t.Fatal("empty present header failed")
	}
	if len(assertStructured(c, http.Header{"X-Value": []string{"", ""}}, nil)) == 0 {
		t.Fatal("repeated header passed")
	}
}
func TestAssertionValidation(t *testing.T) {
	for _, assertion := range []string{`{"pointer":"bad","equals":1}`, `{"pointer":"/~2","equals":1}`, `{"pointer":"/a"}`} {
		input := `{"checks":[{"name":"a","url":"http://127.0.0.1/","status":200,"timeout_ms":1000,"json":[` + assertion + `]}]}`
		if _, err := readDeck(strings.NewReader(input)); err == nil {
			t.Fatal("invalid assertion accepted", assertion)
		}
	}
}

func TestCancellationStopsLaterRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cancel(); <-r.Context().Done() }))
	defer s.Close()
	r := runDeck(ctx, Deck{Checks: []Check{{Name: "active", URL: s.URL, Status: 200, TimeoutMS: 1000}, {Name: "later", URL: s.URL, Status: 200, TimeoutMS: 1000}}})
	if r.Passed || r.Stopped == "" || r.Results[0].Skipped || !r.Results[1].Skipped {
		t.Fatalf("%+v", r)
	}
}
func TestOverallDeadline(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "deck.json")
	b, _ := json.Marshal(Deck{Checks: []Check{{Name: "slow", URL: s.URL, Status: 200, TimeoutMS: 30000}}})
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := cli([]string{"run", path, "--deadline-ms", "20"}, &out, &errs); code != 1 {
		t.Fatalf("%d %s", code, errs.String())
	}
	var report Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Stopped != "context deadline exceeded" {
		t.Fatalf("%s", out.String())
	}
	for _, v := range []string{"0", "-1", "3600001", "bad"} {
		if cli([]string{"run", path, "--deadline-ms", v}, &out, &errs) != 2 {
			t.Fatal(v)
		}
	}
}

func TestDuplicateDeckKeys(t *testing.T) {
	for _, input := range []string{`{"checks":[],"checks":[]}`, `{"checks":[{"name":"a","name":"b"}]}`, `{"checks":[{"json":[{"equals":{"x":1,"x":2}}]}]}`, `{"a":1,"\u0061":2}`} {
		if err := uniqueKeys([]byte(input)); err == nil {
			t.Fatal("duplicate accepted", input)
		}
	}
	if err := uniqueKeys([]byte(`{"a":{"x":1},"b":{"x":2}}`)); err != nil {
		t.Fatal(err)
	}
	if err := uniqueKeys([]byte(strings.Repeat("[", 130) + "0" + strings.Repeat("]", 130))); err == nil {
		t.Fatal("deep input accepted")
	}
}
