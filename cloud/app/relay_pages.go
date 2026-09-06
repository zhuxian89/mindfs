package app

import (
	"embed"
	"html/template"
)

// Keep Relay-owned pages separate from the upstream Web assets. Shared styles
// and helpers are rendered inline so these pages need no asset server or build.
//
//go:embed pages/*.html
var relayPageFiles embed.FS

func relayPage(name string) *template.Template {
	return template.Must(template.New(name+".html").ParseFS(relayPageFiles, "pages/shared.html", "pages/"+name+".html"))
}

var (
	browserLoginPage = relayPage("login")
	browserNodesPage = relayPage("nodes")
	bindPage         = relayPage("bind")
)
