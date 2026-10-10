package extract

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/0xTraceSentinel/MediaScraper/internal/model"
)

type JSONLD struct{}

func (JSONLD) Name() string { return "jsonld" }

func (j JSONLD) Extract(doc *Document) ([]model.Product, error) {
	var out []model.Product
	doc.HTML.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		raw := strings.TrimSpace(s.Text())
		raw = strings.TrimPrefix(raw, "<!--")
		raw = strings.TrimSuffix(raw, "-->")
		raw = strings.TrimPrefix(strings.TrimSpace(raw), "//<![CDATA[")
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "//]]>")

		dec := json.NewDecoder(strings.NewReader(raw))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return
		}
		walkLD(v, func(node map[string]any) {
			if p := productFromLD(node, doc); !p.IsEmpty() {
				p.Sources = []string{j.Name()}
				out = append(out, p)
			}
		})
	})
	return out, nil
}

func walkLD(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			walkLD(e, fn)
		}
	case map[string]any:
		if hasType(t, "Product", "ProductGroup", "IndividualProduct", "ProductModel") {
			fn(t)
			return
		}
		for _, e := range t {
			walkLD(e, fn)
		}
	}
}

func hasType(node map[string]any, types ...string) bool {
	check := func(s string) bool {
		if i := strings.LastIndexAny(s, "/:#"); i >= 0 {
			s = s[i+1:]
		}
		for _, t := range types {
			if strings.EqualFold(s, t) {
				return true
			}
		}
		return false
	}
	switch t := node["@type"].(type) {
	case string:
		return check(t)
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && check(s) {
				return true
			}
		}
	}
	return false
}

func productFromLD(node map[string]any, doc *Document) model.Product {
	p := model.Product{
		Name:        cleanText(ldString(node["name"])),
		Description: cleanText(ldString(node["description"])),
		SKU:         ldString(node["sku"]),
		MPN:         ldString(node["mpn"]),
		Brand:       ldName(node["brand"]),
		Category:    ldName(node["category"]),
		URL:         doc.Resolve(ldString(node["url"])),
	}
	if p.Brand == "" {
		p.Brand = ldName(node["manufacturer"])
	}
	for _, k := range []string{"gtin", "gtin13", "gtin12", "gtin14", "gtin8", "isbn"} {
		if g := ldString(node[k]); g != "" {
			p.GTIN = g
			break
		}
	}
	for _, img := range ldImages(node["image"]) {
		if u := doc.Resolve(img); u != "" {
			p.Images = append(p.Images, u)
		}
	}

	offers := node["offers"]
	if offers == nil {
		if variants, ok := node["hasVariant"].([]any); ok && len(variants) > 0 {
			if v, ok := variants[0].(map[string]any); ok {
				offers = v["offers"]
			}
		}
	}
	if price, currency, availability := ldOffer(offers); price != "" || availability != "" {
		p.Price, p.Currency, p.Availability = price, currency, availability
	}

	if r, ok := node["aggregateRating"].(map[string]any); ok {
		p.Rating = ldString(r["ratingValue"])
		p.ReviewCount = ldString(r["reviewCount"])
		if p.ReviewCount == "" {
			p.ReviewCount = ldString(r["ratingCount"])
		}
	}
	return p
}

func ldOffer(v any) (price, currency, availability string) {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			if price, currency, availability = ldOffer(e); price != "" {
				return
			}
		}
	case map[string]any:
		availability = normalizeAvailability(ldString(t["availability"]))
		currency = ldString(t["priceCurrency"])
		for _, k := range []string{"price", "lowPrice"} {
			if price = normalizePrice(ldString(t[k])); price != "" {
				return
			}
		}
		if spec := t["priceSpecification"]; spec != nil {
			specs, ok := spec.([]any)
			if !ok {
				specs = []any{spec}
			}
			for _, s := range specs {
				if m, ok := s.(map[string]any); ok {
					if price = normalizePrice(ldString(m["price"])); price != "" {
						if c := ldString(m["priceCurrency"]); c != "" {
							currency = c
						}
						return
					}
				}
			}
		}
		if nested := t["offers"]; nested != nil {
			p, c, a := ldOffer(nested)
			if p != "" {
				price = p
				if c != "" {
					currency = c
				}
				if availability == "" {
					availability = a
				}
			}
		}
	}
	return
}

func ldString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		for _, e := range t {
			if s := ldString(e); s != "" {
				return s
			}
		}
	case map[string]any:
		return ldString(t["@value"])
	}
	return ""
}

func ldName(v any) string {
	switch t := v.(type) {
	case map[string]any:
		return cleanText(ldString(t["name"]))
	case []any:
		for _, e := range t {
			if s := ldName(e); s != "" {
				return s
			}
		}
		return ""
	default:
		return cleanText(ldString(v))
	}
}

func ldImages(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, e := range t {
			out = append(out, ldImages(e)...)
		}
		return out
	case map[string]any:
		for _, k := range []string{"url", "contentUrl"} {
			if s := ldString(t[k]); s != "" {
				return []string{s}
			}
		}
	}
	return nil
}
