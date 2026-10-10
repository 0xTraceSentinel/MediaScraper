package extract

import (
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/0xTraceSentinel/MediaScraper/internal/model"
)

func mustDoc(t *testing.T, rawURL, html string) *Document {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDocument(u, []byte(html))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNormalizePrice(t *testing.T) {
	tests := map[string]string{
		"$1,299.99":     "1299.99",
		"1.299,99 €":    "1299.99",
		"€ 1 299,99":    "1299.99",
		"1,299":         "1299",
		"19,99":         "19.99",
		"19.9":          "19.9",
		"From 49":       "49",
		"CHF 1'234.50":  "1234.50",
		"1,234,567.89":  "1234567.89",
		"1.234.567":     "1234567",
		"Price: $5.00.": "5.00",
		"free":          "",
		"":              "",
	}
	for in, want := range tests {
		if got := normalizePrice(in); got != want {
			t.Errorf("normalizePrice(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetectCurrency(t *testing.T) {
	tests := map[string]string{
		"€19.99":    "EUR",
		"£5":        "GBP",
		"USD 10.00": "USD",
		"$10.00":    "",
		"10.00 ABC": "",
	}
	for in, want := range tests {
		if got := detectCurrency(in); got != want {
			t.Errorf("detectCurrency(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeAvailability(t *testing.T) {
	tests := map[string]string{
		"https://schema.org/InStock":   "InStock",
		"http://schema.org/OutOfStock": "OutOfStock",
		"schema:PreOrder":              "PreOrder",
		"In Stock":                     "InStock",
		"sold-out":                     "OutOfStock",
		"In stock (22 available)":      "InStock",
		"Out of stock - notify me":     "OutOfStock",
		"Ships in 3 weeks":             "Ships in 3 weeks",
	}
	for in, want := range tests {
		if got := normalizeAvailability(in); got != want {
			t.Errorf("normalizeAvailability(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJSONLDProduct(t *testing.T) {
	doc := mustDoc(t, "https://shop.example.com/p/widget", readFixture(t, "jsonld_product.html"))
	got, err := JSONLD{}.Extract(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Product{{
		URL:          "https://shop.example.com/p/widget",
		Name:         "Acme Widget Pro",
		Description:  "The best widget.",
		SKU:          "AW-PRO-1",
		GTIN:         "0012345678905",
		MPN:          "AWP1",
		Brand:        "Acme",
		Price:        "1299.00",
		Currency:     "USD",
		Availability: "InStock",
		Rating:       "4.6",
		ReviewCount:  "128",
		Images:       []string{"https://shop.example.com/img/widget-1.jpg", "https://cdn.example.com/widget-2.jpg"},
		Sources:      []string{"jsonld"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestJSONLDItemListAndMalformedBlocks(t *testing.T) {
	html := `<html><head>
<script type="application/ld+json">{ this is not json </script>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"ItemList","itemListElement":[
  {"@type":"ListItem","position":1,"item":{"@type":"Product","name":"A","offers":{"@type":"Offer","price":10,"priceCurrency":"EUR"}}},
  {"@type":"ListItem","position":2,"item":{"@type":"Product","name":"B","offers":{"@type":"AggregateOffer","lowPrice":"5.5","priceCurrency":"EUR"}}}
]}
</script></head></html>`
	got, err := JSONLD{}.Extract(mustDoc(t, "https://x.test/", html))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d products, want 2", len(got))
	}
	if got[0].Name != "A" || got[0].Price != "10" || got[1].Name != "B" || got[1].Price != "5.5" {
		t.Errorf("unexpected products: %+v", got)
	}
}

func TestJSONLDProductGroupUsesVariantOffer(t *testing.T) {
	html := `<script type="application/ld+json">
{"@type":"ProductGroup","name":"Tee","hasVariant":[{"@type":"Product","name":"Tee S","offers":{"price":"20.00","priceCurrency":"GBP"}}]}
</script>`
	got, _ := JSONLD{}.Extract(mustDoc(t, "https://x.test/", html))
	if len(got) != 1 || got[0].Name != "Tee" || got[0].Price != "20.00" || got[0].Currency != "GBP" {
		t.Errorf("got %+v", got)
	}
}

func TestMicrodataProduct(t *testing.T) {
	doc := mustDoc(t, "https://shop.example.com/p/kettle", readFixture(t, "microdata_product.html"))
	got, err := Microdata{}.Extract(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d products, want 1: %+v", len(got), got)
	}
	p := got[0]
	if p.Name != "Steel Kettle" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.Brand != "BoilCo" {
		t.Errorf("Brand = %q", p.Brand)
	}
	if p.Price != "34.95" || p.Currency != "EUR" || p.Availability != "InStock" {
		t.Errorf("offer = %q %q %q", p.Price, p.Currency, p.Availability)
	}
	if !reflect.DeepEqual(p.Images, []string{"https://shop.example.com/img/kettle.jpg"}) {
		t.Errorf("Images = %v", p.Images)
	}
}

func TestOpenGraph(t *testing.T) {
	product := `<head>
<meta property="og:type" content="product">
<meta property="og:title" content="Desk Lamp">
<meta property="og:image" content="/lamp.jpg">
<meta property="product:price:amount" content="45.00">
<meta property="product:price:currency" content="CAD">
<meta property="product:availability" content="in stock">
</head>`
	got, _ := OpenGraph{}.Extract(mustDoc(t, "https://lamps.test/p/1", product))
	if len(got) != 1 {
		t.Fatalf("got %d products", len(got))
	}
	p := got[0]
	if p.Name != "Desk Lamp" || p.Price != "45.00" || p.Currency != "CAD" || p.Availability != "InStock" {
		t.Errorf("got %+v", p)
	}
	if !reflect.DeepEqual(p.Images, []string{"https://lamps.test/lamp.jpg"}) {
		t.Errorf("Images = %v", p.Images)
	}

	article := `<meta property="og:type" content="article"><meta property="og:title" content="Blog post">`
	if got, _ := (OpenGraph{}).Extract(mustDoc(t, "https://lamps.test/blog", article)); len(got) != 0 {
		t.Errorf("non-product page produced %+v", got)
	}
}

func TestSelectorProfileListing(t *testing.T) {
	prof := Profile{
		Name:    "test-shop",
		Domains: []string{"shop.test"},
		Product: ProductRules{
			Container: ".tile",
			Fields: map[string]FieldRule{
				"name":     {Selector: ".title"},
				"price":    {Selector: ".price"},
				"url":      {Selector: "a", Attr: "href"},
				"images":   {Selector: "img", Attr: "data-src"},
				"sku":      {Selector: "[data-sku]", Attr: "data-sku", Regex: `SKU-(\d+)`},
				"currency": {Value: "USD"},
			},
			Extra: map[string]FieldRule{"badge": {Selector: ".badge"}},
		},
	}
	if err := prof.Validate(); err != nil {
		t.Fatal(err)
	}
	html := `<div class="tile" data-sku="SKU-1">
  <a href="/p/1"><span class="title">  First
  item </span></a><span class="price">$1,000.50</span><img data-src="//cdn.test/1.jpg"><span class="badge">New</span>
</div>
<div class="tile" data-sku="SKU-2"><a href="/p/2"><span class="title">Second</span></a><span class="price">$9</span></div>`
	got, err := NewSelector(&prof).Extract(mustDoc(t, "https://www.shop.test/c/all", html))
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Product{
		{URL: "https://www.shop.test/p/1", Name: "First item", SKU: "1", Price: "1000.50", Currency: "USD",
			Images: []string{"https://cdn.test/1.jpg"}, Extra: map[string]string{"badge": "New"}, Sources: []string{"profile:test-shop"}},
		{URL: "https://www.shop.test/p/2", Name: "Second", SKU: "2", Price: "9", Currency: "USD", Sources: []string{"profile:test-shop"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestProfileValidate(t *testing.T) {
	tests := map[string]Profile{
		"missing name":    {Domains: []string{"a.test"}},
		"missing domains": {Name: "x"},
		"unknown field":   {Name: "x", Domains: []string{"a.test"}, Product: ProductRules{Fields: map[string]FieldRule{"colour": {Selector: "p"}}}},
		"bad selector":    {Name: "x", Domains: []string{"a.test"}, Product: ProductRules{Fields: map[string]FieldRule{"name": {Selector: "[["}}}},
		"bad regex":       {Name: "x", Domains: []string{"a.test"}, Product: ProductRules{Fields: map[string]FieldRule{"name": {Selector: "p", Regex: "("}}}},
		"empty rule":      {Name: "x", Domains: []string{"a.test"}, Product: ProductRules{Fields: map[string]FieldRule{"name": {}}}},
		"bad follow":      {Name: "x", Domains: []string{"a.test"}, Follow: []string{"a[["}},
	}
	for name, p := range tests {
		t.Run(name, func(t *testing.T) {
			if err := p.Validate(); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestProfileMatches(t *testing.T) {
	p := Profile{Name: "x", Domains: []string{"Example.com"}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	for host, want := range map[string]bool{
		"example.com":      true,
		"www.example.com":  true,
		"badexample.com":   false,
		"example.com.evil": false,
	} {
		if got := p.Matches(host); got != want {
			t.Errorf("Matches(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestPipelineMergesSourcesAndFindsLinks(t *testing.T) {
	html := `<html><head>
<base href="https://shop.test/en/">
<meta property="og:type" content="product">
<meta property="og:title" content="Mug (og)">
<meta property="og:image" content="https://shop.test/og.jpg">
<script type="application/ld+json">{"@type":"Product","name":"Mug","offers":{"price":"12.00","priceCurrency":"USD"}}</script>
</head><body>
<h1 class="name">Mug (profile)</h1>
<a class="next" href="page/2">next</a>
<a class="next" href="page/2#top">dup</a>
<a class="other" href="/elsewhere">x</a>
</body></html>`
	pl, err := NewPipeline([]Profile{{
		Name:    "shop",
		Domains: []string{"shop.test"},
		Product: ProductRules{Fields: map[string]FieldRule{"name": {Selector: "h1.name"}}},
		Follow:  []string{"a.next"},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("https://shop.test/en/mug")
	page, err := pl.Process(u, []byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if page.Profile != "shop" {
		t.Errorf("Profile = %q", page.Profile)
	}
	if len(page.Products) != 1 {
		t.Fatalf("got %d products", len(page.Products))
	}
	p := page.Products[0]
	if p.Name != "Mug (profile)" || p.Price != "12.00" || p.Currency != "USD" {
		t.Errorf("merged product = %+v", p)
	}
	if !reflect.DeepEqual(p.Images, []string{"https://shop.test/og.jpg"}) {
		t.Errorf("Images = %v", p.Images)
	}
	if !reflect.DeepEqual(p.Sources, []string{"profile:shop", "jsonld", "opengraph"}) {
		t.Errorf("Sources = %v", p.Sources)
	}
	if p.URL != "https://shop.test/en/mug" {
		t.Errorf("URL = %q", p.URL)
	}
	if !reflect.DeepEqual(page.Links, []string{"https://shop.test/en/page/2"}) {
		t.Errorf("Links = %v", page.Links)
	}
}
