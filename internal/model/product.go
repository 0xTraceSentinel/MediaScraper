package model

type Product struct {
	URL          string            `json:"url,omitempty"`
	Name         string            `json:"name,omitempty"`
	Description  string            `json:"description,omitempty"`
	SKU          string            `json:"sku,omitempty"`
	GTIN         string            `json:"gtin,omitempty"`
	MPN          string            `json:"mpn,omitempty"`
	Brand        string            `json:"brand,omitempty"`
	Category     string            `json:"category,omitempty"`
	Price        string            `json:"price,omitempty"`
	Currency     string            `json:"currency,omitempty"`
	Availability string            `json:"availability,omitempty"`
	Rating       string            `json:"rating,omitempty"`
	ReviewCount  string            `json:"review_count,omitempty"`
	Images       []string          `json:"images,omitempty"`
	Extra        map[string]string `json:"extra,omitempty"`
	Sources      []string          `json:"sources,omitempty"`
}

func (p *Product) IsEmpty() bool {
	return p.Name == "" && p.Price == "" && p.SKU == "" && p.GTIN == ""
}

func (p *Product) Merge(other Product) {
	fill := func(dst *string, src string) {
		if *dst == "" {
			*dst = src
		}
	}
	fill(&p.URL, other.URL)
	fill(&p.Name, other.Name)
	fill(&p.Description, other.Description)
	fill(&p.SKU, other.SKU)
	fill(&p.GTIN, other.GTIN)
	fill(&p.MPN, other.MPN)
	fill(&p.Brand, other.Brand)
	fill(&p.Category, other.Category)
	if p.Price == "" && other.Price != "" {
		p.Price = other.Price
		p.Currency = other.Currency
	}
	fill(&p.Currency, other.Currency)
	fill(&p.Availability, other.Availability)
	fill(&p.Rating, other.Rating)
	fill(&p.ReviewCount, other.ReviewCount)
	if len(p.Images) == 0 {
		p.Images = other.Images
	}
	for k, v := range other.Extra {
		if p.Extra == nil {
			p.Extra = make(map[string]string, len(other.Extra))
		}
		if _, ok := p.Extra[k]; !ok {
			p.Extra[k] = v
		}
	}
	p.Sources = append(p.Sources, other.Sources...)
}
