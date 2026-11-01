package main

import (
	"strings"
	"testing"
)

func TestStandaloneTypes(t *testing.T) {
	body := []byte(`{"n":null,"b":true,"s":"x","a":[],"o":{},"num":1.0}`)
	for pointer, kind := range map[string]string{"/n": "null", "/b": "boolean", "/s": "string", "/a": "array", "/o": "object", "/num": "number"} {
		c := Check{JSON: []JSONAssertion{{Pointer: pointer, Type: kind}}}
		if p := assertStructured(c, nil, body); len(p) != 0 {
			t.Fatal(p)
		}
	}
	for _, a := range []JSONAssertion{{Pointer: "/missing", Type: "null"}, {Pointer: "/n", Type: "object"}, {Pointer: "/num", Type: "string"}} {
		if p := assertStructured(Check{JSON: []JSONAssertion{a}}, nil, body); len(p) != 1 {
			t.Fatal(p)
		}
	}
}
func TestTypeConfiguration(t *testing.T) {
	for _, assertion := range []string{`{"pointer":"","type":"integer"}`, `{"pointer":"","type":"null","equals":null}`, `{"pointer":""}`} {
		_, err := readDeck(strings.NewReader(`{"checks":[{"name":"test","url":"http://127.0.0.1/","status":200,"timeout_ms":100,"json":[` + assertion + `]}]}`))
		if err == nil {
			t.Fatal("accepted invalid assertion", assertion)
		}
	}
	_, err := readDeck(strings.NewReader(`{"checks":[{"name":"test","url":"http://127.0.0.1/","status":200,"timeout_ms":100,"json":[{"pointer":"","type":"null"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
}
