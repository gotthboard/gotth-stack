package site

import _ "embed"

var (
	//go:embed static/site-ab3aa9255fd5fa082e8fc2f5c6739fa76ea155924477bd238ea44996b8b5e7ed.css
	siteCSS []byte

	//go:embed static/htmx-2.0.10.min.js
	htmxJS []byte
)
