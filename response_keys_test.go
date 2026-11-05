package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponseJSONAmbiguity(t *testing.T) {
	for _, body := range []string{
		`{"ready":false,"ready":true}`,
		`{"nested":{"secret":1,"secret":2}}`,
		`{"ready":false,"\u0072eady":true}`,
		`[{"a":1,"a":2}]`,
		strings.Repeat("[", 129) + "0" + strings.Repeat("]", 129),
	} {
		t.Run(body[:minLength(len(body), 40)], func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer s.Close()
			c := Check{Name: "synthetic", URL: s.URL, Status: 200, TimeoutMS: 1000, JSON: []JSONAssertion{{Pointer: "", Type: "object"}}}
			r := runDeck(context.Background(), Deck{Checks: []Check{c}})
			if r.Passed || len(r.Results[0].Problems) != 1 || !strings.Contains(r.Results[0].Problems[0], "unique object keys") {
				t.Fatalf("unexpected report: %+v", r)
			}
			if strings.Contains(r.Results[0].Problems[0], "secret") {
				t.Fatal("response key leaked")
			}
			c.JSON = nil
			if r := runDeck(context.Background(), Deck{Checks: []Check{c}}); !r.Passed {
				t.Fatal("status-only check should not parse JSON")
			}
		})
	}
}
func minLength(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestResponseJSONSeparateKeysAndDepthBoundary(t *testing.T) {
	for _, tc := range []struct{ body, kind string }{
		{`{"left":{"a":1},"right":{"a":2}}`, "object"},
		{strings.Repeat("[", 128) + "0" + strings.Repeat("]", 128), "array"},
		{`null`, "null"},
	} {
		c := Check{JSON: []JSONAssertion{{Pointer: "", Type: tc.kind}}}
		if p := assertStructured(c, http.Header{}, []byte(tc.body)); len(p) != 0 {
			t.Fatal(p)
		}
	}
}
