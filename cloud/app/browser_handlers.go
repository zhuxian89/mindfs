package app

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

var browserNodesPage = template.Must(template.New("nodes").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>MindFS Nodes</title>
<style>
:root{color-scheme:light dark}*{box-sizing:border-box}body{margin:0;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#111827;color:#f8fafc}main{width:min(680px,100%);margin:0 auto;padding:40px 20px}header{display:flex;align-items:center;justify-content:space-between;gap:16px;border-bottom:1px solid #334155;padding-bottom:18px}h1{margin:0;font-size:24px;letter-spacing:0}p{color:#cbd5e1;line-height:1.55}.nodes{display:grid;gap:8px;margin:22px 0}.node{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:12px 0;border-bottom:1px solid #1f2937}.node a{min-width:0;color:#7dd3fc;text-decoration:none;overflow-wrap:anywhere}.node a:hover{text-decoration:underline}.node span{min-width:0;color:#94a3b8;font-size:12px;overflow-wrap:anywhere;text-align:right}.empty{padding:18px 0;color:#94a3b8}form{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;margin-top:24px}input,button{font:inherit;border:1px solid #475569;border-radius:6px;padding:10px 12px}input{min-width:0;background:#0f172a;color:#f8fafc}button{background:#0284c7;color:white;border-color:#0284c7;cursor:pointer}button.secondary{background:transparent;color:#cbd5e1;border-color:#475569}.error{min-height:22px;color:#fca5a5;margin-top:8px;font-size:13px}@media(max-width:520px){main{padding:24px 16px}form{grid-template-columns:1fr}header{align-items:flex-start}.node{align-items:flex-start;flex-direction:column;gap:4px}.node span{text-align:left}}
</style>
</head>
<body>
<main>
<header><h1>MindFS Nodes</h1><button id="refresh" class="secondary" type="button">Refresh</button></header>
<p>Open a node saved by this browser, or enter its full relay URL.</p>
<div id="nodes" class="nodes"></div>
<form id="open-node"><input id="node-url" type="url" inputmode="url" autocomplete="url" placeholder="https://relay.example.com/n/node-id/" required><button type="submit">Open</button></form>
<div id="error" class="error" role="alert"></div>
</main>
<script>
const storageKey="mindfs_launcher_nodes";
const nodesElement=document.getElementById("nodes");
const errorElement=document.getElementById("error");
function relayNodeURL(value){try{const parsed=new URL(String(value||""),location.origin);if(parsed.origin!==location.origin||!/^\/n\/[^/]+\/?$/.test(parsed.pathname))return "";return parsed.toString()}catch{return ""}}
function storedNodes(){try{const parsed=JSON.parse(localStorage.getItem(storageKey)||"[]");if(!Array.isArray(parsed))return [];return parsed.map(item=>({name:String(item&&item.name||"").trim(),url:relayNodeURL(item&&item.url)})).filter(item=>item.name&&item.url)}catch{return []}}
function render(){const seen=new Set();const nodes=storedNodes();const referrer=relayNodeURL(document.referrer);if(referrer&&!nodes.some(item=>item.url===referrer))nodes.unshift({name:"Previous node",url:referrer});nodesElement.replaceChildren();for(const node of nodes){if(seen.has(node.url))continue;seen.add(node.url);const row=document.createElement("div");row.className="node";const link=document.createElement("a");link.href=node.url;link.textContent=node.name;const address=document.createElement("span");address.textContent=new URL(node.url).pathname;row.append(link,address);nodesElement.append(row)}if(!nodesElement.childElementCount){const empty=document.createElement("div");empty.className="empty";empty.textContent="No node is saved in this browser.";nodesElement.append(empty)}}
document.getElementById("refresh").addEventListener("click",()=>location.reload());
document.getElementById("open-node").addEventListener("submit",event=>{event.preventDefault();const target=relayNodeURL(document.getElementById("node-url").value);if(!target){errorElement.textContent="Enter a node URL on this relay.";return}location.assign(target)});
render();
</script>
</body>
</html>`))

func (a *App) handleBrowserRoot(w http.ResponseWriter, r *http.Request) {
	setBrowserNoStoreHeaders(w)
	http.Redirect(w, r, "/nodes", http.StatusSeeOther)
}

func (a *App) handleBrowserAuthStatus(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"authenticated": false,
		"auth_required": false,
		"access_mode":   "node_auth",
	})
}

func (a *App) handleBrowserLogin(w http.ResponseWriter, r *http.Request) {
	setBrowserNoStoreHeaders(w)
	target := safeNodeRedirect(r.URL.Query().Get("next"))
	if target == "" {
		target = "/nodes"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (a *App) handleBrowserNodes(w http.ResponseWriter, _ *http.Request) {
	setBrowserNoStoreHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	if err := browserNodesPage.Execute(w, nil); err != nil {
		return
	}
}

func safeNodeRedirect(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "\\") {
		return ""
	}
	target, err := url.ParseRequestURI(raw)
	if err != nil || target.IsAbs() || target.Host != "" || strings.Contains(target.Path, "\\") || !strings.HasPrefix(target.Path, "/n/") {
		return ""
	}
	remainder := strings.TrimPrefix(target.Path, "/n/")
	nodeID := strings.TrimSpace(strings.SplitN(remainder, "/", 2)[0])
	if nodeID == "" || nodeID == "." || nodeID == ".." {
		return ""
	}
	return target.RequestURI()
}

func setBrowserNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
}
