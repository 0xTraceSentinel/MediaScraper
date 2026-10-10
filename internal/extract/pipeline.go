package extract

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"

	"github.com/0xTraceSentinel/MediaScraper/internal/model"
)

type Extractor interface {
	Name() string
	Extract(doc *Document) ([]model.Product, error)
}

type Page struct {
	Products []model.Product
	Links    []string
	Profile  string
}

type Pipeline struct {
	profiles []*Profile
	generic  []Extractor
	follow   []string
}

func DefaultExtractors() []Extractor {
	return []Extractor{JSONLD{}, Microdata{}, OpenGraph{}}
}

func NewPipeline(profiles []Profile, follow []string) (*Pipeline, error) {
	p := &Pipeline{generic: DefaultExtractors(), follow: follow}
	for i := range profiles {
		prof := profiles[i]
		prof.Domains = slices.Clone(prof.Domains)
		prof.Product.Fields = maps.Clone(prof.Product.Fields)
		prof.Product.Extra = maps.Clone(prof.Product.Extra)
		if err := prof.Validate(); err != nil {
			return nil, err
		}
		p.profiles = append(p.profiles, &prof)
	}
	for _, sel := range follow {
		if _, err := cascadia.ParseGroup(sel); err != nil {
			return nil, fmt.Errorf("invalid follow selector %q: %w", sel, err)
		}
	}
	return p, nil
}

func (p *Pipeline) profileFor(host string) *Profile {
	for _, prof := range p.profiles {
		if prof.Matches(host) {
			return prof
		}
	}
	return nil
}

func (p *Pipeline) Process(pageURL *url.URL, body []byte) (*Page, error) {
	doc, err := NewDocument(pageURL, body)
	if err != nil {
		return nil, err
	}

	extractors := p.generic
	follow := p.follow
	page := &Page{}
	if prof := p.profileFor(pageURL.Hostname()); prof != nil {
		page.Profile = prof.Name
		extractors = append([]Extractor{NewSelector(prof)}, p.generic...)
		follow = append(slices.Clone(prof.Follow), p.follow...)
	}

	var errs []error
	for _, ex := range extractors {
		products, err := ex.Extract(doc)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ex.Name(), err))
			continue
		}
		page.Products = mergeProducts(page.Products, products)
	}
	if len(page.Products) == 1 && page.Products[0].URL == "" {
		page.Products[0].URL = pageURL.String()
	}
	page.Links = links(doc, follow)
	return page, errors.Join(errs...)
}

func mergeProducts(base, next []model.Product) []model.Product {
	switch {
	case len(base) == 0:
		return next
	case len(base) == 1 && len(next) == 1:
		base[0].Merge(next[0])
	}
	return base
}

func links(doc *Document, selectors []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, sel := range selectors {
		doc.HTML.Find(sel).Each(func(_ int, s *goquery.Selection) {
			ref, ok := s.Attr("href")
			if !ok {
				ref = s.AttrOr("src", "")
			}
			if u := doc.Resolve(ref); u != "" && !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		})
	}
	return out
}
