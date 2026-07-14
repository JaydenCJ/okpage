package site

// Per-service SVG badges in the familiar shields style, so READMEs and
// dashboards can embed live service state straight from the static host:
//
//	![api](https://status.example.test/badge/api.svg)
//
// Badges are plain hand-built SVG — no fonts fetched, no external service.

import (
	"fmt"
	"html"

	"github.com/JaydenCJ/okpage/internal/stats"
)

// Approximate advance width of one character of DejaVu Sans / Verdana at
// 11px. Shields.io measures real glyphs; a fixed estimate keeps this
// dependency-free and is close enough for short labels.
const charWidth = 7

const badgeTemplate = `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">
  <linearGradient id="s" x2="0" y2="100%%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>
  <clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath>
  <g clip-path="url(#r)">
    <rect width="%d" height="20" fill="#555"/>
    <rect x="%d" width="%d" height="20" fill="%s"/>
    <rect width="%d" height="20" fill="url(#s)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">
    <text x="%d" y="14">%s</text>
    <text x="%d" y="14">%s</text>
  </g>
</svg>
`

// renderBadge builds the SVG badge for one service.
func renderBadge(svc stats.Service) []byte {
	label := svc.Name
	value, color := "no data", "#9f9f9f"
	if svc.HasData {
		if svc.LastOK {
			value, color = "up", "#4c1" // shields brightgreen
		} else {
			value, color = "down", "#e05d44" // shields red
		}
	}

	labelW := len(label)*charWidth + 10
	valueW := len(value)*charWidth + 10
	total := labelW + valueW

	return []byte(fmt.Sprintf(badgeTemplate,
		total, html.EscapeString(label), value,
		total,
		labelW,
		labelW, valueW, color,
		total,
		labelW/2, html.EscapeString(label),
		labelW+valueW/2, value,
	))
}
