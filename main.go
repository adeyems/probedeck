package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const maxBytes = 1024 * 1024

type Check struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Status    int    `json:"status"`
	Contains  string `json:"contains,omitempty"`
	TimeoutMS int    `json:"timeout_ms"`
}
type Deck struct {
	Checks []Check `json:"checks"`
}
type Result struct {
	Name     string   `json:"name"`
	Passed   bool     `json:"passed"`
	Status   int      `json:"status,omitempty"`
	Problems []string `json:"problems"`
}
type Report struct {
	Passed  bool     `json:"passed"`
	Results []Result `json:"results"`
}

func readDeck(r io.Reader) (Deck, error) {
	var d Deck
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return d, err
	}
	if len(b) > maxBytes {
		return d, fmt.Errorf("deck exceeds 1 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&d); err != nil {
		return d, err
	}
	var extra interface{}
	if err = dec.Decode(&extra); err != io.EOF {
		return d, fmt.Errorf("expected one JSON document")
	}
	if len(d.Checks) == 0 || len(d.Checks) > 100 {
		return d, fmt.Errorf("deck must have 1 to 100 checks")
	}
	names := map[string]bool{}
	for _, c := range d.Checks {
		if strings.TrimSpace(c.Name) == "" || names[c.Name] {
			return d, fmt.Errorf("check names must be nonempty and unique")
		}
		names[c.Name] = true
		u, e := url.Parse(c.URL)
		if e != nil || u.Scheme != "http" || u.User != nil || u.Fragment != "" {
			return d, fmt.Errorf("%s: use an HTTP loopback URL without credentials or fragment", c.Name)
		}
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return d, fmt.Errorf("%s: URL host must be a literal loopback IP", c.Name)
		}
		if c.Status < 100 || c.Status > 599 {
			return d, fmt.Errorf("%s: status must be 100 to 599", c.Name)
		}
		if c.TimeoutMS < 1 || c.TimeoutMS > 30000 {
			return d, fmt.Errorf("%s: timeout_ms must be 1 to 30000", c.Name)
		}
	}
	return d, nil
}

func runDeck(ctx context.Context, d Deck) Report {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	report := Report{Passed: true, Results: []Result{}}
	for _, c := range d.Checks {
		result := Result{Name: c.Name, Problems: []string{}}
		func() {
			checkCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutMS)*time.Millisecond)
			defer cancel()
			req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, c.URL, nil)
			if err != nil {
				result.Problems = append(result.Problems, "invalid request")
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				result.Problems = append(result.Problems, "request failed or timed out")
				return
			}
			defer resp.Body.Close()
			result.Status = resp.StatusCode
			if resp.StatusCode != c.Status {
				result.Problems = append(result.Problems, fmt.Sprintf("expected status %d, got %d", c.Status, resp.StatusCode))
			}
			b, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
			if err != nil {
				result.Problems = append(result.Problems, "response read failed")
				return
			}
			if len(b) > maxBytes {
				result.Problems = append(result.Problems, "response exceeds 1 MiB")
				return
			}
			if c.Contains != "" && !bytes.Contains(b, []byte(c.Contains)) {
				result.Problems = append(result.Problems, "required text missing")
			}
		}()
		result.Passed = len(result.Problems) == 0
		if !result.Passed {
			report.Passed = false
		}
		report.Results = append(report.Results, result)
	}
	return report
}

func cli(args []string, out, errs io.Writer) int {
	if len(args) != 2 || args[0] != "run" {
		fmt.Fprintln(errs, "usage: probedeck run DECK.json")
		return 2
	}
	f, err := os.Open(args[1])
	if err != nil {
		fmt.Fprintln(errs, "cannot open deck")
		return 2
	}
	defer f.Close()
	d, err := readDeck(f)
	if err != nil {
		fmt.Fprintln(errs, "invalid deck:", err)
		return 2
	}
	report := runDeck(context.Background(), d)
	if err = json.NewEncoder(out).Encode(report); err != nil {
		fmt.Fprintln(errs, "cannot write report")
		return 2
	}
	if !report.Passed {
		return 1
	}
	return 0
}
func main() { os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr)) }
