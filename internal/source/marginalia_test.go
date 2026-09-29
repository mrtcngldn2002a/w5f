package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMarginaliaRandomIsFreshEveryTime(t *testing.T) {
	withFetcher(t) // with a disk cache: the random list must not come from it
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		fmt.Fprintf(w, `<html><head><title>Random</title></head><body><article><p>Random list number %d. %s</p></article></body></html>`, n, strings.Repeat("Small sites of the independent web. ", 20))
	}))
	defer srv.Close()
	old := marginaliaRandomURL
	marginaliaRandomURL = srv.URL + "/explore/random"
	defer func() { marginaliaRandomURL = old }()
	a, err := Load(context.Background(), MarginaliaRandom, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), MarginaliaRandom, Options{}); err != nil || n != 2 {
		t.Fatalf("second visit: %v (requests %d)", err, n)
	}
	if a.URL != MarginaliaRandom || len(a.Links) == 0 || a.Links[len(a.Links)-1].Href != MarginaliaRandom {
		t.Errorf("page: %q %+v", a.URL, a.Links)
	}
}
