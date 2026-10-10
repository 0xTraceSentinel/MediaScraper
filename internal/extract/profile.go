package extract

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"

	"github.com/0xTraceSentinel/MediaScraper/internal/model"
)

type Profile struct {
	Name    string       `yaml:"name" json:"name"`
	Domains []string     `yaml:"domains" json:"domains"`
	Product ProductRules `yaml:"product" json:"product"`
	Follow  []string     `yaml:"follow" json:"follow,omitempty"`
}

type ProductRules struct {
	Container string               `yaml:"container" json:"container,omitempty"`
	Fields    map[string]FieldRule `yaml:"fields" json:"fields"`
	Extra     map[string]FieldRule `yaml:"extra" json:"extra,omitempty"`
}

type FieldRule struct {
	Selector string `yaml:"selector" json:"selector"`
	Attr     string `yaml:"attr" json:"attr,omitempty"`
	Regex    string `yaml:"regex" json:"regex,omitempty"`
	Value    string `yaml:"value" json:"value,omitempty"`

	re *regexp.Regexp
}

var ProductFields = []string{
	"name", "description", "sku", "gtin", "mpn", "brand", "category",
	"price", "currency", "availability", "rating", "review_count", "images", "url",
}

func (r *FieldRule) compile() error {
	if r.Selector == "" && r.Value == "" {
		return errors.New("selector or value is required")
	}
	if r.Selector != "" {
		if _, err := cascadia.ParseGroup(r.Selector); err != nil {
			return fmt.Errorf("invalid selector %q: %w", r.Selector, err)
		}
	}
	if r.Regex != "" {
		re, err := regexp.Compile(r.Regex)
		if err != nil {
			return fmt.Errorf("invalid regex %q: %w", r.Regex, err)
		}
		r.re = re
	}
	return nil
}

func (p *Profile) Validate() error {
	if p.Name == "" {
		return errors.New("profile name is required")
	}
	if len(p.Domains) == 0 {
		return fmt.Errorf("profile %q: at least one domain is required", p.Name)
	}
	for i, d := range p.Domains {
		p.Domains[i] = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "."))
	}
	if p.Product.Container != "" {
		if _, err := cascadia.ParseGroup(p.Product.Container); err != nil {
			return fmt.Errorf("profile %q: invalid container selector: %w", p.Name, err)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(p.Product.Fields)) {
		if !slices.Contains(ProductFields, name) {
			return fmt.Errorf("profile %q: unknown field %q (valid: %s)", p.Name, name, strings.Join(ProductFields, ", "))
		}
		rule := p.Product.Fields[name]
		if err := rule.compile(); err != nil {
			return fmt.Errorf("profile %q: field %q: %w", p.Name, name, err)
		}
		p.Product.Fields[name] = rule
	}
	for _, name := range slices.Sorted(maps.Keys(p.Product.Extra)) {
		rule := p.Product.Extra[name]
		if err := rule.compile(); err != nil {
			return fmt.Errorf("profile %q: extra %q: %w", p.Name, name, err)
		}
		p.Product.Extra[name] = rule
	}
	for _, sel := range p.Follow {
		if _, err := cascadia.ParseGroup(sel); err != nil {
			return fmt.Errorf("profile %q: invalid follow selector %q: %w", p.Name, sel, err)
		}
	}
	return nil
}

func (p *Profile) Matches(host string) bool {
	host = strings.ToLower(host)
	for _, d := range p.Domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

type Selector struct {
	profile *Profile
}

func NewSelector(p *Profile) *Selector { return &Selector{profile: p} }

func (s *Selector) Name() string { return "profile:" + s.profile.Name }

func (s *Selector) Extract(doc *Document) ([]model.Product, error) {
	rules := s.profile.Product
	if len(rules.Fields) == 0 && len(rules.Extra) == 0 {
		return nil, nil
	}
	scopes := doc.HTML.Selection
	if rules.Container != "" {
		scopes = doc.HTML.Find(rules.Container)
	}
	var out []model.Product
	scopes.Each(func(_ int, scope *goquery.Selection) {
		p := s.extractOne(scope, doc)
		if !p.IsEmpty() {
			out = append(out, p)
		}
	})
	return out, nil
}

func (s *Selector) extractOne(scope *goquery.Selection, doc *Document) model.Product {
	f := s.profile.Product.Fields
	get := func(name string) string {
		r, ok := f[name]
		if !ok {
			return ""
		}
		return cleanText(readRule(scope, r))
	}

	p := model.Product{
		Name:         get("name"),
		Description:  get("description"),
		SKU:          get("sku"),
		GTIN:         get("gtin"),
		MPN:          get("mpn"),
		Brand:        get("brand"),
		Category:     get("category"),
		Availability: normalizeAvailability(get("availability")),
		Rating:       normalizePrice(get("rating")),
		ReviewCount:  normalizePrice(get("review_count")),
		URL:          doc.Resolve(get("url")),
		Sources:      []string{s.Name()},
	}
	if rawPrice := get("price"); rawPrice != "" {
		p.Price = normalizePrice(rawPrice)
		p.Currency = get("currency")
		if p.Currency == "" {
			p.Currency = detectCurrency(rawPrice)
		}
	}
	if r, ok := f["images"]; ok {
		findInScope(scope, r.Selector).Each(func(_ int, el *goquery.Selection) {
			if u := doc.Resolve(applyRegex(r, readValue(el, r.Attr))); u != "" && !slices.Contains(p.Images, u) {
				p.Images = append(p.Images, u)
			}
		})
	}
	for name, r := range s.profile.Product.Extra {
		if v := cleanText(readRule(scope, r)); v != "" {
			if p.Extra == nil {
				p.Extra = make(map[string]string)
			}
			p.Extra[name] = v
		}
	}
	return p
}

func readRule(scope *goquery.Selection, r FieldRule) string {
	if r.Selector == "" {
		return r.Value
	}
	el := findInScope(scope, r.Selector).First()
	if el.Length() == 0 {
		return r.Value
	}
	return applyRegex(r, readValue(el, r.Attr))
}

func readValue(el *goquery.Selection, attr string) string {
	if attr != "" {
		return strings.TrimSpace(el.AttrOr(attr, ""))
	}
	return el.Text()
}

func applyRegex(r FieldRule, v string) string {
	if r.re == nil {
		return v
	}
	m := r.re.FindStringSubmatch(v)
	switch {
	case m == nil:
		return ""
	case len(m) > 1:
		return m[1]
	default:
		return m[0]
	}
}

func findInScope(scope *goquery.Selection, sel string) *goquery.Selection {
	return scope.Filter(sel).AddSelection(scope.Find(sel))
}
