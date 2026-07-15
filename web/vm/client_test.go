// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

package vm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPointUnmarshal(t *testing.T) {
	var p Point
	if err := p.UnmarshalJSON([]byte(`[1718000000,"12.34"]`)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p[0] != 1718000000 || p[1] != 12.34 {
		t.Fatalf("got %v", p)
	}
	// value as number (not string) should also parse
	var p2 Point
	if err := p2.UnmarshalJSON([]byte(`[1,5]`)); err != nil {
		t.Fatalf("unmarshal num: %v", err)
	}
	if p2[1] != 5 {
		t.Fatalf("got %v", p2)
	}
}

func TestQueryRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"success",
			"data":{"resultType":"matrix","result":[
				{"metric":{"hostname":"h1"},"values":[[1718000000,"1"],[1718000060,"2.5"]]}
			]}
		}`))
	}))
	defer srv.Close()

	c := New(srv.URL, 5*time.Second)
	series, err := c.QueryRange(context.Background(), "cpu_usage", "1717990000", "1718000000", "60")
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if len(series) != 1 || len(series[0].Values) != 2 {
		t.Fatalf("unexpected series: %+v", series)
	}
	if series[0].Values[1][1] != 2.5 {
		t.Fatalf("bad value: %v", series[0].Values[1])
	}
	if series[0].Metric["hostname"] != "h1" {
		t.Fatalf("bad label: %v", series[0].Metric)
	}
}

func TestQueryVMError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	c := New(srv.URL, 5*time.Second)
	if _, err := c.Query(context.Background(), "x"); err == nil {
		t.Fatal("expected error")
	}
}
