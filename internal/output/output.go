package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/0xTraceSentinel/MediaScraper/internal/scraper"
)

type Format string

const (
	FormatJSONL Format = "jsonl"
	FormatJSON  Format = "json"
	FormatCSV   Format = "csv"
)

var Formats = []Format{FormatJSONL, FormatJSON, FormatCSV}

type Sink interface {
	Write(r scraper.Result) error
	Close() error
}

func New(format Format, w io.Writer) (Sink, error) {
	switch format {
	case FormatJSONL:
		return &jsonLines{enc: json.NewEncoder(w)}, nil
	case FormatJSON:
		return &jsonArray{w: w}, nil
	case FormatCSV:
		return &csvSink{w: csv.NewWriter(w)}, nil
	default:
		return nil, fmt.Errorf("output: unsupported format %q", format)
	}
}

type jsonLines struct{ enc *json.Encoder }

func (s *jsonLines) Write(r scraper.Result) error { return s.enc.Encode(r) }
func (s *jsonLines) Close() error                 { return nil }

type jsonArray struct {
	w     io.Writer
	count int
}

func (s *jsonArray) Write(r scraper.Result) error {
	b, err := json.MarshalIndent(r, "  ", "  ")
	if err != nil {
		return err
	}
	prefix := ",\n  "
	if s.count == 0 {
		prefix = "[\n  "
	}
	s.count++
	_, err = fmt.Fprintf(s.w, "%s%s", prefix, b)
	return err
}

func (s *jsonArray) Close() error {
	trailer := "\n]\n"
	if s.count == 0 {
		trailer = "[]\n"
	}
	_, err := io.WriteString(s.w, trailer)
	return err
}

type csvSink struct {
	w           *csv.Writer
	wroteHeader bool
}

var csvHeader = []string{
	"page_url", "status_code", "error", "profile",
	"url", "name", "brand", "sku", "gtin", "mpn", "category",
	"price", "currency", "availability", "rating", "review_count",
	"images", "description", "sources",
}

func (s *csvSink) Write(r scraper.Result) error {
	if !s.wroteHeader {
		if err := s.w.Write(csvHeader); err != nil {
			return err
		}
		s.wroteHeader = true
	}
	page := []string{r.URL, fmt.Sprint(r.StatusCode), r.Error, r.Profile}
	if len(r.Products) == 0 {
		return s.w.Write(sanitizeRow(append(page, make([]string, len(csvHeader)-len(page))...)))
	}
	for _, p := range r.Products {
		row := append(page[:len(page):len(page)],
			p.URL, p.Name, p.Brand, p.SKU, p.GTIN, p.MPN, p.Category,
			p.Price, p.Currency, p.Availability, p.Rating, p.ReviewCount,
			strings.Join(p.Images, " "), p.Description, strings.Join(p.Sources, " "),
		)
		if err := s.w.Write(sanitizeRow(row)); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeRow(row []string) []string {
	for i, v := range row {
		if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
			row[i] = "'" + v
		}
	}
	return row
}

func (s *csvSink) Close() error {
	s.w.Flush()
	return s.w.Error()
}
