package extract

import (
	"reflect"
	"regexp"
	"testing"
)

func TestExtract_NamedGroups(t *testing.T) {
	re := regexp.MustCompile(`ERROR payment_id=(?P<pid>\d+) amount=(?P<amt>\d+\.\d+)`)
	got, ok := Extract("ERROR payment_id=12345 amount=99.9", re)
	if !ok {
		t.Fatal("expected match")
	}
	want := map[string]string{"pid": "12345", "amt": "99.9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestExtract_UnnamedGroups(t *testing.T) {
	re := regexp.MustCompile(`ERROR payment_id=(\d+) amount=(\d+\.\d+)`)
	got, ok := Extract("ERROR payment_id=12345 amount=99.9", re)
	if !ok {
		t.Fatal("expected match")
	}
	want := map[string]string{"1": "12345", "2": "99.9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestExtract_MixedNamedAndUnnamed(t *testing.T) {
	re := regexp.MustCompile(`(?P<level>\w+) payment_id=(\d+)`)
	got, ok := Extract("ERROR payment_id=42", re)
	if !ok {
		t.Fatal("expected match")
	}
	want := map[string]string{"level": "ERROR", "2": "42"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestExtract_NoMatch(t *testing.T) {
	re := regexp.MustCompile(`ERROR payment_id=(\d+)`)
	if got, ok := Extract("INFO hello world", re); ok || got != nil {
		t.Fatalf("expected no match, got ok=%v v=%+v", ok, got)
	}
}

func TestExtract_ZeroCaptureMatch(t *testing.T) {
	re := regexp.MustCompile(`^INFO$`)
	got, ok := Extract("INFO", re)
	if !ok {
		t.Fatal("expected match")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %+v", got)
	}
}
