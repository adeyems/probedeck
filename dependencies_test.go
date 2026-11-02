package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDependenciesSkipFailuresAndPreserveIndependentWork(t *testing.T) {
	hits := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		if r.URL.Path == "/first" {
			w.WriteHeader(500)
		} else {
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	deck := Deck{Checks: []Check{{Name: "first", URL: server.URL + "/first", Status: 200, TimeoutMS: 1000}, {Name: "second", URL: server.URL + "/second", Status: 200, TimeoutMS: 1000, DependsOn: []string{"first"}}, {Name: "third", URL: server.URL + "/third", Status: 200, TimeoutMS: 1000, DependsOn: []string{"second"}}, {Name: "independent", URL: server.URL + "/independent", Status: 200, TimeoutMS: 1000}}}
	raw, _ := json.Marshal(deck)
	validated, err := readDeck(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	report := runDeck(context.Background(), validated)
	if report.Passed || !report.Results[1].Skipped || !report.Results[2].Skipped || !report.Results[3].Passed {
		t.Fatal(report)
	}
	if strings.Join(hits, ",") != "/first,/independent" {
		t.Fatal(hits)
	}
	hits = nil
	if report := runConcurrent(context.Background(), validated, 2); report.Stopped == "" {
		t.Fatal(report)
	}
	if len(hits) != 0 {
		t.Fatal(hits)
	}
}
func TestInvalidDependencyOrder(t *testing.T) {
	for _, dependencies := range []string{`["self"]`, `["missing"]`, `["previous","previous"]`} {
		raw := `{"checks":[{"name":"previous","url":"http://127.0.0.1/","status":200,"timeout_ms":100},{"name":"self","url":"http://127.0.0.1/","status":200,"timeout_ms":100,"depends_on":` + dependencies + `}]}`
		if _, err := readDeck(strings.NewReader(raw)); err == nil {
			t.Fatal("accepted", dependencies)
		}
	}
}

func TestSuccessfulDependencyRunsChild(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	deck := Deck{Checks: []Check{{Name: "parent", URL: server.URL, Status: 200, TimeoutMS: 1000}, {Name: "child", URL: server.URL, Status: 200, TimeoutMS: 1000, DependsOn: []string{"parent"}}}}
	report := runDeck(context.Background(), deck)
	if !report.Passed || len(report.Results) != 2 || report.Results[1].Skipped {
		t.Fatal(report)
	}
}
