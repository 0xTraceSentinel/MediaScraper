package extract

import (
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/0xTraceSentinel/MediaScraper/internal/model"
)

type Microdata struct{}

func (Microdata) Name() string { return "microdata" }

func (m Microdata) Extract(doc *Document) ([]model.Product, error) {
	var out []model.Product
	doc.HTML.Find("[itemscope][itemtype]").Each(func(_ int, s *goquery.Selection) {
		if !isProductScope(s) {
			return
		}
		if s.ParentsFiltered("[itemscope][itemtype]").FilterFunction(func(_ int, p *goquery.Selection) bool {
			return isProductScope(p)
		}).Length() > 0 {
			return
		}
		node := microdataItem(s, doc)
		if p := productFromLD(node, doc); !p.IsEmpty() {
			p.Sources = []string{m.Name()}
			out = append(out, p)
		}
	})
	return out, nil
}

func isProductScope(s *goquery.Selection) bool {
	for _, t := range strings.Fields(s.AttrOr("itemtype", "")) {
		node := map[string]any{"@type": t}
		if hasType(node, "Product", "ProductGroup", "IndividualProduct", "ProductModel") {
			return true
		}
	}
	return false
}

func microdataItem(scope *goquery.Selection, doc *Document) map[string]any {
	item := map[string]any{"@type": scope.AttrOr("itemtype", "")}
	scope.Find("[itemprop]").Each(func(_ int, el *goquery.Selection) {
		if el.ParentsUntilSelection(scope).Filter("[itemscope]").Length() > 0 {
			return
		}
		var val any
		if _, nested := el.Attr("itemscope"); nested {
			val = microdataItem(el, doc)
		} else {
			val = microdataValue(el, doc)
		}
		for _, name := range strings.Fields(el.AttrOr("itemprop", "")) {
			switch existing := item[name].(type) {
			case nil:
				item[name] = val
			case []any:
				item[name] = append(existing, val)
			default:
				item[name] = []any{existing, val}
			}
		}
	})
	return item
}

func microdataValue(el *goquery.Selection, doc *Document) string {
	if c, ok := el.Attr("content"); ok {
		return strings.TrimSpace(c)
	}
	switch goquery.NodeName(el) {
	case "img", "audio", "video", "source", "embed", "iframe", "track":
		return doc.Resolve(el.AttrOr("src", ""))
	case "a", "area", "link":
		return doc.Resolve(el.AttrOr("href", ""))
	case "object":
		return doc.Resolve(el.AttrOr("data", ""))
	case "data", "meter":
		return strings.TrimSpace(el.AttrOr("value", ""))
	case "time":
		if dt, ok := el.Attr("datetime"); ok {
			return strings.TrimSpace(dt)
		}
	}
	return cleanText(el.Text())
}
