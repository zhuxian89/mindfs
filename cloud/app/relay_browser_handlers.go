package app

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"mindfs-cloud/internal/binding"
)

type browserLoginPageData struct {
	Next string
}

var browserLoginPage = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>MindFS Relay Login</title>
<style>
:root{color-scheme:light dark}*{box-sizing:border-box}body{margin:0;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#111827;color:#f8fafc}main{width:min(440px,100%);margin:0 auto;padding:64px 20px}h1{margin:0 0 8px;font-size:24px}p{color:#cbd5e1;line-height:1.55}form{display:grid;gap:12px;margin-top:24px}input,button{font:inherit;border:1px solid #475569;border-radius:6px;padding:11px 12px}input{min-width:0;background:#0f172a;color:#f8fafc}button{background:#0284c7;color:white;border-color:#0284c7;cursor:pointer}.error{min-height:22px;color:#fca5a5;margin-top:10px;font-size:13px}
</style>
</head>
<body>
<main>
<h1>MindFS Relay</h1>
<p>Sign in with the bootstrap administrator account.</p>
<form id="bootstrap-login">
<input id="username" autocomplete="username" placeholder="Username" required>
<input id="password" type="password" autocomplete="current-password" placeholder="Password" required>
<button type="submit">Sign in</button>
</form>
<div id="error" class="error" role="alert"></div>
</main>
<script>
const next={{.Next}};
const login=document.getElementById("bootstrap-login");
const username=document.getElementById("username");
const password=document.getElementById("password");
const errorElement=document.getElementById("error");
login.addEventListener("submit",async event=>{event.preventDefault();errorElement.textContent="";const response=await fetch("/api/cloud/v1/auth/login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({username:username.value,password:password.value})});const body=await response.json().catch(()=>({}));if(!response.ok){errorElement.textContent=body.message||body.error||"Sign in failed.";return}location.assign(next)});
</script>
</body>
</html>`))

var browserNodesPage = template.Must(template.New("nodes").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>MindFS Nodes</title>
<style>
:root{color-scheme:light dark}*{box-sizing:border-box}body{margin:0;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#0b1220;color:#f8fafc}main{width:min(760px,100%);margin:0 auto;padding:36px 20px 56px}header{display:flex;align-items:center;justify-content:space-between;gap:16px;border-bottom:1px solid #334155;padding-bottom:18px}.title{min-width:0}h1{margin:0;font-size:24px;letter-spacing:0}.user{margin:5px 0 0;color:#94a3b8;font-size:13px}.actions,.node-tools{display:flex;align-items:center;gap:8px;flex:0 0 auto}button{font:inherit;border:1px solid #475569;border-radius:6px;padding:9px 11px;background:#111827;color:#e2e8f0;cursor:pointer}button:hover{border-color:#64748b}.node-action{padding:6px 9px;font-size:12px}.node-delete{color:#fca5a5;border-color:#7f1d1d}.nodes{display:grid;margin-top:18px;border-top:1px solid #1f2937}.node{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:18px;align-items:center;padding:16px 0;border-bottom:1px solid #1f2937}.node-main{min-width:0}.node-title{display:flex;align-items:center;gap:9px;min-width:0}.status{width:9px;height:9px;border-radius:50%;background:#64748b;flex:0 0 auto}.status.online{background:#22c55e}.node a{min-width:0;color:#f8fafc;text-decoration:none;font-weight:600;overflow-wrap:anywhere}.node a:hover{text-decoration:underline}.node a.offline{color:#94a3b8;cursor:not-allowed}.meta{margin-top:5px;color:#94a3b8;font-size:12px;overflow-wrap:anywhere}.state{color:#94a3b8;font-size:12px;text-transform:uppercase}.empty,.error{padding:28px 0;color:#94a3b8}.error{color:#fca5a5}@media(max-width:520px){main{padding:24px 16px 44px}header{align-items:flex-start}.node{grid-template-columns:1fr;gap:9px}.actions{gap:6px}.node-tools{justify-content:flex-start;flex-wrap:wrap}button{padding:8px 9px}}
</style>
</head>
<body>
<main>
<header>
<div class="title"><h1>MindFS Nodes</h1><p id="user" class="user"></p></div>
<div class="actions"><button id="refresh" type="button">Refresh</button><button id="logout" type="button">Sign out</button></div>
</header>
<div id="nodes" class="nodes"></div>
<div id="error" class="error" role="alert"></div>
</main>
<script>
const nodesElement=document.getElementById("nodes");
const errorElement=document.getElementById("error");
const userElement=document.getElementById("user");
function nodeURL(node){return "/n/"+encodeURIComponent(String(node&&node.id||"").trim())+"/"}
function relativeTime(value){if(!value)return "Never connected";const elapsed=Date.now()-new Date(value).getTime();if(!Number.isFinite(elapsed)||elapsed<60000)return "Just now";if(elapsed<3600000)return Math.max(1,Math.floor(elapsed/60000))+" min ago";if(elapsed<86400000)return Math.max(1,Math.floor(elapsed/3600000))+" hr ago";return Math.max(1,Math.floor(elapsed/86400000))+" days ago"}
function syncLauncherNodes(nodes){const payload=nodes.map(node=>({name:String(node.name||"").trim(),url:new URL(nodeURL(node),location.origin).toString()})).filter(node=>node.name&&node.url);if(!payload.length)return;try{if(window.MindFSLauncherNodeSync&&typeof window.MindFSLauncherNodeSync.storeRelayNodes==="function"){window.MindFSLauncherNodeSync.storeRelayNodes(JSON.stringify(payload));return}const plugin=window.Capacitor&&window.Capacitor.Plugins&&window.Capacitor.Plugins.LauncherNodeSync;if(plugin&&typeof plugin.storeRelayNodes==="function")plugin.storeRelayNodes({nodes:payload})}catch{}}
function render(nodes){nodesElement.replaceChildren();if(!nodes.length){const empty=document.createElement("div");empty.className="empty";empty.textContent="No nodes are bound.";nodesElement.append(empty);return}for(const node of nodes){const online=node.status==="online";const row=document.createElement("div");row.className="node";const main=document.createElement("div");main.className="node-main";const title=document.createElement("div");title.className="node-title";const dot=document.createElement("span");dot.className="status"+(online?" online":"");const link=document.createElement("a");link.href=nodeURL(node);link.textContent=String(node.name||node.id||"");if(!online){link.className="offline";link.addEventListener("click",event=>{event.preventDefault();errorElement.textContent="This node is offline."})}const meta=document.createElement("div");meta.className="meta";meta.textContent=String(node.id||"")+" · "+relativeTime(node.last_seen_at);const tools=document.createElement("div");tools.className="node-tools";const state=document.createElement("span");state.className="state";state.textContent=online?"Online":"Offline";const rename=document.createElement("button");rename.type="button";rename.className="node-action";rename.textContent="Rename";rename.addEventListener("click",()=>renameNode(node));const remove=document.createElement("button");remove.type="button";remove.className="node-action node-delete";remove.textContent="Delete";remove.addEventListener("click",()=>deleteNode(node));title.append(dot,link);main.append(title,meta);tools.append(state,rename,remove);row.append(main,tools);nodesElement.append(row)}}
async function api(path,init){const response=await fetch(path,{credentials:"include",...init});const body=await response.json().catch(()=>({}));if(response.status===401){location.replace("/login?next="+encodeURIComponent(location.pathname+location.search));throw new Error("unauthorized")}if(!response.ok)throw new Error(body.error||"request_failed");return body}
async function load(){errorElement.textContent="";try{const nodes=await api("/api/nodes");syncLauncherNodes(nodes);render(Array.isArray(nodes)?nodes:[])}catch(error){if(error.message!=="unauthorized")errorElement.textContent="Node list could not be loaded."}}
async function renameNode(node){const name=window.prompt("Node name",String(node.name||""));if(name===null)return;const trimmed=name.trim();if(!trimmed){errorElement.textContent="Node name is required.";return}try{await api("/api/nodes/"+encodeURIComponent(node.id),{method:"PATCH",headers:{"Content-Type":"application/json"},body:JSON.stringify({name:trimmed})});await load()}catch(error){if(error.message!=="unauthorized")errorElement.textContent="Node could not be renamed."}}
async function deleteNode(node){if(!window.confirm("Delete node "+String(node.name||node.id||"")+"?"))return;try{await api("/api/nodes/"+encodeURIComponent(node.id),{method:"DELETE"});await load()}catch(error){if(error.message!=="unauthorized")errorElement.textContent="Node could not be deleted."}}
async function loadUser(){try{const user=await api("/api/auth/me");userElement.textContent=user.name||user.username||"Administrator"}catch{}}
document.getElementById("refresh").addEventListener("click",load);
document.getElementById("logout").addEventListener("click",async()=>{await fetch("/api/auth/logout",{method:"POST",credentials:"include"});location.replace("/login")});
loadUser();load();window.setInterval(load,15000);
</script>
</body>
</html>`))

func (a *App) handleBrowserLogin(w http.ResponseWriter, r *http.Request) {
	setBrowserNoStoreHeaders(w)
	target := safeRelayRedirect(r.URL.Query().Get("next"))
	if target == "" {
		target = "/nodes"
	}
	if _, err := a.authenticateAdmin(r); err == nil {
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	} else if !errors.Is(err, binding.ErrAuthRequired) {
		http.Error(w, "request failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	_ = browserLoginPage.Execute(w, browserLoginPageData{Next: target})
}

func (a *App) handleBrowserNodes(w http.ResponseWriter, r *http.Request) {
	setBrowserNoStoreHeaders(w)
	if _, err := a.authenticateAdmin(r); errors.Is(err, binding.ErrAuthRequired) {
		target := "/login?next=" + url.QueryEscape(r.URL.RequestURI())
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	} else if err != nil {
		http.Error(w, "request failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	if err := browserNodesPage.Execute(w, nil); err != nil {
		return
	}
}

func (a *App) handleBrowserAuthStatus(w http.ResponseWriter, r *http.Request) {
	if _, err := a.authenticateAdmin(r); err != nil {
		if errors.Is(err, binding.ErrAuthRequired) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"id":       "usr_bootstrap",
		"name":     a.config.AdminUsername,
		"username": a.config.AdminUsername,
		"roles":    []string{"owner"},
	})
}

func (a *App) handleBrowserLogout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.PublicURL.Scheme == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}
