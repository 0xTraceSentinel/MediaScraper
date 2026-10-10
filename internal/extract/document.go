package extract

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type Document struct {
	URL  *url.URL
	HTML *goquery.Document
}

func NewDocument(pageURL *url.URL, body []byte) (*Document, error) {
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("extract: parse html: %w", err)
	}
	base := pageURL
	if href, ok := d.Find("base[href]").First().Attr("href"); ok {
		if b, err := pageURL.Parse(strings.TrimSpace(href)); err == nil {
			base = b
		}
	}
	return &Document{URL: base, HTML: d}, nil
}

func (d *Document) Resolve(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "javascript:") {
		return ""
	}
	u, err := d.URL.Parse(ref)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

var spaceRe = regexp.MustCompile(`\s+`)

func cleanText(s string) string {
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

var numberRe = regexp.MustCompile(`\d[\d.,'\s\x{00A0}\x{202F}]*`)

func normalizePrice(s string) string {
	m := strings.TrimRight(numberRe.FindString(s), ".,'   ")
	if m == "" {
		return ""
	}
	m = strings.NewReplacer(" ", "", " ", "", " ", "", "'", "").Replace(m)

	lastDot, lastComma := strings.LastIndex(m, "."), strings.LastIndex(m, ",")
	switch {
	case lastDot >= 0 && lastComma >= 0:
		if lastComma > lastDot {
			m = strings.ReplaceAll(m, ".", "")
			m = strings.Replace(m, ",", ".", 1)
		} else {
			m = strings.ReplaceAll(m, ",", "")
		}
	case lastComma >= 0:
		m = resolveSingleSeparator(m, ",")
	case lastDot >= 0:
		m = resolveSingleSeparator(m, ".")
	}
	return m
}

func resolveSingleSeparator(m, sep string) string {
	if strings.Count(m, sep) > 1 {
		return strings.ReplaceAll(m, sep, "")
	}
	i := strings.Index(m, sep)
	if len(m)-i-1 == 3 {
		return strings.ReplaceAll(m, sep, "")
	}
	return strings.Replace(m, sep, ".", 1)
}

var (
	isoCurrencyRe   = regexp.MustCompile(`\b[A-Z]{3}\b`)
	currencySymbols = map[string]string{"€": "EUR", "£": "GBP", "₹": "INR", "₩": "KRW", "₺": "TRY", "₽": "RUB", "₫": "VND", "₱": "PHP"}
	knownISO        = map[string]bool{
		"USD": true, "EUR": true, "GBP": true, "JPY": true, "CNY": true, "CAD": true, "AUD": true, "NZD": true,
		"CHF": true, "SEK": true, "NOK": true, "DKK": true, "PLN": true, "CZK": true, "HUF": true, "INR": true,
		"BRL": true, "MXN": true, "SGD": true, "HKD": true, "KRW": true, "ZAR": true, "TRY": true, "AED": true,
		"SAR": true, "ILS": true, "THB": true, "MYR": true, "IDR": true, "PHP": true, "VND": true, "TWD": true,
	}
)

func detectCurrency(s string) string {
	for _, code := range isoCurrencyRe.FindAllString(s, -1) {
		if knownISO[code] {
			return code
		}
	}
	for sym, code := range currencySymbols {
		if strings.Contains(s, sym) {
			return code
		}
	}
	return ""
}

var availabilityAliases = map[string]string{
	"instock":             "InStock",
	"available":           "InStock",
	"outofstock":          "OutOfStock",
	"soldout":             "OutOfStock",
	"unavailable":         "OutOfStock",
	"oos":                 "OutOfStock",
	"preorder":            "PreOrder",
	"presale":             "PreSale",
	"backorder":           "BackOrder",
	"limitedavailability": "LimitedAvailability",
	"discontinued":        "Discontinued",
	"instoreonly":         "InStoreOnly",
	"onlineonly":          "OnlineOnly",
}

func normalizeAvailability(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	key := strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.ToLower(s))
	if v, ok := availabilityAliases[key]; ok {
		return v
	}
	for alias, v := range availabilityAliases {
		if len(alias) >= 7 && strings.HasPrefix(key, alias) {
			return v
		}
	}
	return s
}
