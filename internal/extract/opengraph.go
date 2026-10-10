package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/0xTraceSentinel/MediaScraper/internal/model"
)

type OpenGraph struct{}

func (OpenGraph) Name() string { return "opengraph" }

func (o OpenGraph) Extract(doc *Document) ([]model.Product, error) {
	meta := map[string][]string{}
	doc.HTML.Find("meta[content]").Each(func(_ int, s *goquery.Selection) {
		key := s.AttrOr("property", s.AttrOr("name", ""))
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			return
		}
		if v := strings.TrimSpace(s.AttrOr("content", "")); v != "" {
			meta[key] = append(meta[key], v)
		}
	})
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := meta[k]; len(v) > 0 {
				return v[0]
			}
		}
		return ""
	}

	price := first("product:price:amount", "og:price:amount", "product:sale_price:amount")
	if !strings.Contains(strings.ToLower(first("og:type")), "product") && price == "" {
		return nil, nil
	}

	p := model.Product{
		Name:         cleanText(first("og:title", "twitter:title")),
		Description:  cleanText(first("og:description", "twitter:description")),
		URL:          doc.Resolve(first("og:url")),
		Price:        normalizePrice(price),
		Currency:     first("product:price:currency", "og:price:currency", "product:sale_price:currency"),
		Availability: normalizeAvailability(first("product:availability", "og:availability")),
		Brand:        first("product:brand", "og:brand"),
		SKU:          first("product:retailer_item_id", "product:sku"),
		Category:     first("product:category"),
		Sources:      []string{o.Name()},
	}
	images := meta["og:image"]
	if len(images) == 0 {
		images = meta["og:image:secure_url"]
	}
	for _, img := range images {
		if u := doc.Resolve(img); u != "" {
			p.Images = append(p.Images, u)
		}
	}
	if p.IsEmpty() {
		return nil, nil
	}
	return []model.Product{p}, nil
}
