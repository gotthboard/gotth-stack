package site

import _ "embed"

var (
	//go:embed static/site-4c3b7f235729e101ffa964903e3ec0c23e47ff7b3fc7ba41452d030b900eec52.css
	siteCSS []byte

	//go:embed static/htmx-2.0.10.min.js
	htmxJS []byte
)
