package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDiscoveryAndCatalog(t *testing.T) {
	ts := newTestServer(t)

	_, b := do(t, &http.Client{}, "GET", ts.URL+"/", "", nil)
	if field(t, b, "service") != "ufo-hub" || !strings.HasSuffix(field(t, b, "api_catalog"), "/.well-known/api-catalog") {
		t.Fatalf("root discovery = %s", b)
	}

	res, err := http.Get(ts.URL + "/.well-known/api-catalog")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "application/linkset+json" {
		t.Fatalf("catalog content-type = %q, want application/linkset+json", ct)
	}
	body, _ := io.ReadAll(res.Body)
	var cat struct {
		Linkset []struct {
			Anchor      string              `json:"anchor"`
			Item        []map[string]string `json:"item"`
			ServiceDesc []map[string]string `json:"service-desc"`
		} `json:"linkset"`
	}
	if err := json.Unmarshal(body, &cat); err != nil {
		t.Fatalf("decode catalog: %v (%s)", err, body)
	}
	var sawItem, sawDesc bool
	for _, l := range cat.Linkset {
		for _, it := range l.Item {
			sawItem = sawItem || strings.HasSuffix(it["href"], "/v1")
		}
		for _, d := range l.ServiceDesc {
			sawDesc = sawDesc || strings.HasSuffix(d["href"], "/openapi.yaml")
		}
	}
	if !sawItem || !sawDesc {
		t.Fatalf("catalog missing item or service-desc: %s", body)
	}

	code, spec := do(t, &http.Client{}, "GET", ts.URL+"/openapi.yaml", "", nil)
	if code != http.StatusOK || !strings.Contains(string(spec), "openapi:") {
		t.Fatalf("openapi.yaml serve = %d %.60s", code, spec)
	}
}
