package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/0xTraceSentinel/MediaScraper/internal/model"
	"github.com/0xTraceSentinel/MediaScraper/internal/scraper"
)

var results = []scraper.Result{
	{URL: "https://a.test/1", StatusCode: 200, Products: []model.Product{
		{Name: "One", Price: "1.00", Images: []string{"https://a.test/1.jpg", "https://a.test/2.jpg"}},
		{Name: "=HYPERLINK(\"http://evil\")", Price: "2.00"},
	}},
	{URL: "https://a.test/2", StatusCode: 500, Error: "boom"},
}

func write(t *testing.T, f Format, rs []scraper.Result) string {
	t.Helper()
	var buf bytes.Buffer
	s, err := New(f, &buf)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if err := s.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestJSONLines(t *testing.T) {
	out := write(t, FormatJSONL, results)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines", len(lines))
	}
	var r scraper.Result
	if err := json.Unmarshal([]byte(lines[1]), &r); err != nil || r.Error != "boom" {
		t.Errorf("line 2 = %q (%v)", lines[1], err)
	}
}

func TestJSONArray(t *testing.T) {
	var got []scraper.Result
	if err := json.Unmarshal([]byte(write(t, FormatJSON, results)), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Products[0].Name != "One" {
		t.Errorf("got %+v", got)
	}

	if out := write(t, FormatJSON, nil); strings.TrimSpace(out) != "[]" {
		t.Errorf("empty array = %q", out)
	}
}

func TestCSV(t *testing.T) {
	rows, err := csv.NewReader(strings.NewReader(write(t, FormatCSV, results))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("got %d rows", len(rows))
	}
	col := func(name string) int {
		for i, h := range rows[0] {
			if h == name {
				return i
			}
		}
		t.Fatalf("no column %q", name)
		return -1
	}
	if rows[1][col("images")] != "https://a.test/1.jpg https://a.test/2.jpg" {
		t.Errorf("images = %q", rows[1][col("images")])
	}
	if got := rows[2][col("name")]; !strings.HasPrefix(got, "'=") {
		t.Errorf("formula not neutralised: %q", got)
	}
	if rows[3][col("error")] != "boom" || rows[3][col("page_url")] != "https://a.test/2" {
		t.Errorf("failed page row = %v", rows[3])
	}
}

func TestUnknownFormat(t *testing.T) {
	if _, err := New("xml", &bytes.Buffer{}); err == nil {
		t.Error("expected error")
	}
}
