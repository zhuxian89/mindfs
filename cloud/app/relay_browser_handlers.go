package app

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"

	"mindfs-cloud/internal/identity"
)

type browserLoginPageData struct {
	Next string
}

var browserLoginPage = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>MindFS Relay</title>
<style>
:root{color-scheme:light dark}*{box-sizing:border-box}body{margin:0;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#111827;color:#f8fafc}main{width:min(440px,100%);margin:0 auto;padding:48px 20px}h1{margin:0 0 24px;font-size:24px}.modes{display:grid;grid-template-columns:repeat(3,1fr);gap:6px}.modes button{background:#111827;border-color:#475569}.modes button.active{background:#0369a1;border-color:#0284c7}form{display:grid;gap:12px;margin-top:20px}.hidden{display:none}label{display:grid;gap:6px;color:#cbd5e1;font-size:13px}.inline{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px}input,button{font:inherit;border:1px solid #475569;border-radius:6px;padding:11px 12px}input{min-width:0;background:#0f172a;color:#f8fafc}button{background:#0284c7;color:white;border-color:#0284c7;cursor:pointer}button:disabled{cursor:default;opacity:.65}.error,.hint{min-height:20px;margin:12px 0 0;font-size:13px}.error{color:#fca5a5}.hint{color:#86efac}
</style>
</head>
<body>
<main>
<h1>MindFS Relay</h1>
<div class="modes"><button type="button" data-mode="login" class="active">登录</button><button type="button" data-mode="register">注册</button><button type="button" data-mode="reset">忘记密码</button></div>
<form id="login-form">
<label>QQ 邮箱<input name="email" type="email" autocomplete="email" required></label>
<label>Relay 密码<input name="password" type="password" autocomplete="current-password" minlength="8" maxlength="128" required></label>
<button type="submit">登录</button>
</form>
<form id="register-form" class="hidden">
<label>QQ 邮箱<input name="email" type="email" autocomplete="email" required></label>
<label>Relay 密码<input name="password" type="password" autocomplete="new-password" minlength="8" maxlength="128" required></label>
<label>确认密码<input name="confirm_password" type="password" autocomplete="new-password" minlength="8" maxlength="128" required></label>
<label>邮箱验证码<div class="inline"><input name="code" inputmode="numeric" maxlength="6" required><button type="button" data-send="register">发送验证码</button></div></label>
<button type="submit">注册并登录</button>
</form>
<form id="reset-form" class="hidden">
<label>QQ 邮箱<input name="email" type="email" autocomplete="email" required></label>
<label>新 Relay 密码<input name="new_password" type="password" autocomplete="new-password" minlength="8" maxlength="128" required></label>
<label>邮箱验证码<div class="inline"><input name="code" inputmode="numeric" maxlength="6" required><button type="button" data-send="reset">发送验证码</button></div></label>
<button type="submit">重置密码</button>
</form>
<p id="error" class="error" role="alert"></p><p id="hint" class="hint" role="status"></p>
</main>
<script>
const next={{.Next}},errorElement=document.getElementById("error"),hintElement=document.getElementById("hint");
const forms={login:document.getElementById("login-form"),register:document.getElementById("register-form"),reset:document.getElementById("reset-form")};
function show(mode){for(const [name,form] of Object.entries(forms))form.classList.toggle("hidden",name!==mode);for(const button of document.querySelectorAll("[data-mode]"))button.classList.toggle("active",button.dataset.mode===mode);errorElement.textContent="";hintElement.textContent=""}
for(const button of document.querySelectorAll("[data-mode]"))button.addEventListener("click",()=>show(button.dataset.mode));
async function request(path,body){const response=await fetch(path,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});const payload=await response.json().catch(()=>({}));if(!response.ok)throw new Error(payload.error||"request_failed");return payload}
function message(code){return {email_not_allowed:"只允许使用 @qq.com 邮箱。",email_taken:"该邮箱已经注册。",email_code_invalid:"验证码无效或已过期。",email_code_rate_limited:"操作过于频繁，请稍后再试。",email_sender_not_configured:"邮件发送失败，请稍后再试。",invalid_password:"密码长度必须为 8-128 个字符。",invalid_credentials:"邮箱或密码错误。",forbidden:"请求来源无效。"}[code]||"请求失败，请稍后再试。"}
function countdown(button,seconds){button.disabled=true;let left=seconds;button.textContent=left+" 秒";const timer=setInterval(()=>{left--;if(left<=0){clearInterval(timer);button.disabled=false;button.textContent="发送验证码"}else button.textContent=left+" 秒"},1000)}
for(const button of document.querySelectorAll("[data-send]"))button.addEventListener("click",async()=>{errorElement.textContent="";hintElement.textContent="";const form=forms[button.dataset.send];const email=new FormData(form).get("email");try{const path=button.dataset.send==="register"?"/api/auth/register/request-code":"/api/auth/password/request-code";const body=await request(path,{email});hintElement.textContent="验证码已发送。";countdown(button,body.resend_after_seconds||60)}catch(error){errorElement.textContent=message(error.message)}});
forms.login.addEventListener("submit",async event=>{event.preventDefault();const data=new FormData(forms.login);try{await request("/api/auth/login",{email:data.get("email"),password:data.get("password")});location.assign(next)}catch(error){errorElement.textContent=message(error.message)}});
forms.register.addEventListener("submit",async event=>{event.preventDefault();const data=new FormData(forms.register);if(data.get("password")!==data.get("confirm_password")){errorElement.textContent="两次密码输入不一致。";return}try{await request("/api/auth/register",{email:data.get("email"),password:data.get("password"),code:data.get("code")});location.assign(next)}catch(error){errorElement.textContent=message(error.message)}});
forms.reset.addEventListener("submit",async event=>{event.preventDefault();const data=new FormData(forms.reset);try{await request("/api/auth/password/reset",{email:data.get("email"),new_password:data.get("new_password"),code:data.get("code")});show("login");forms.login.elements.email.value=data.get("email");hintElement.textContent="密码已重置，请登录。"}catch(error){errorElement.textContent=message(error.message)}});
</script>
</body>
</html>`))

var browserNodesPage = template.Must(template.New("nodes").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>MindFS Nodes</title>
<style>:root{color-scheme:light dark}*{box-sizing:border-box}body{margin:0;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#0b1220;color:#f8fafc}main{width:min(760px,100%);margin:0 auto;padding:36px 20px 56px}header{display:flex;align-items:center;justify-content:space-between;gap:16px;border-bottom:1px solid #334155;padding-bottom:18px}.title{min-width:0}h1{margin:0;font-size:24px}.user{margin:5px 0 0;color:#94a3b8;font-size:13px}.actions,.node-tools{display:flex;align-items:center;gap:8px;flex:0 0 auto}button{font:inherit;border:1px solid #475569;border-radius:6px;padding:9px 11px;background:#111827;color:#e2e8f0;cursor:pointer}.node-action{padding:6px 9px;font-size:12px}.node-delete{color:#fca5a5;border-color:#7f1d1d}.nodes{display:grid;margin-top:18px;border-top:1px solid #1f2937}.node{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:18px;align-items:center;padding:16px 0;border-bottom:1px solid #1f2937}.node-main{min-width:0}.node-title{display:flex;align-items:center;gap:9px}.status{width:9px;height:9px;border-radius:50%;background:#64748b;flex:0 0 auto}.status.online{background:#22c55e}.node a{color:#f8fafc;text-decoration:none;font-weight:600;overflow-wrap:anywhere}.node a.offline{color:#94a3b8;cursor:not-allowed}.meta{margin-top:5px;color:#94a3b8;font-size:12px}.state{color:#94a3b8;font-size:12px;text-transform:uppercase}.empty,.error{padding:28px 0;color:#94a3b8}.error{color:#fca5a5}@media(max-width:600px){header{align-items:flex-start}.actions{flex-wrap:wrap;justify-content:flex-end}.node{grid-template-columns:1fr}.node-tools{justify-content:flex-start}}</style></head>
<body><main><header><div class="title"><h1>MindFS Nodes</h1><p id="user" class="user"></p></div><div class="actions"><button id="change-password" type="button">修改密码</button><button id="refresh" type="button">刷新</button><button id="logout" type="button">退出</button></div></header><div id="nodes" class="nodes"></div><div id="error" class="error" role="alert"></div></main>
<script>
const nodesElement=document.getElementById("nodes"),errorElement=document.getElementById("error"),userElement=document.getElementById("user");
function nodeURL(node){return "/n/"+encodeURIComponent(String(node&&node.id||"").trim())+"/"}function relativeTime(value){if(!value)return "从未连接";const elapsed=Date.now()-new Date(value).getTime();if(!Number.isFinite(elapsed)||elapsed<60000)return "刚刚";if(elapsed<3600000)return Math.max(1,Math.floor(elapsed/60000))+" 分钟前";if(elapsed<86400000)return Math.max(1,Math.floor(elapsed/3600000))+" 小时前";return Math.max(1,Math.floor(elapsed/86400000))+" 天前"}
function syncLauncherNodes(nodes){const payload=nodes.map(node=>({name:String(node.name||"").trim(),url:new URL(nodeURL(node),location.origin).toString()})).filter(node=>node.name&&node.url);if(!payload.length)return;try{if(window.MindFSLauncherNodeSync&&typeof window.MindFSLauncherNodeSync.storeRelayNodes==="function"){window.MindFSLauncherNodeSync.storeRelayNodes(JSON.stringify(payload));return}const plugin=window.Capacitor&&window.Capacitor.Plugins&&window.Capacitor.Plugins.LauncherNodeSync;if(plugin&&typeof plugin.storeRelayNodes==="function")plugin.storeRelayNodes({nodes:payload})}catch{}}
function render(nodes){nodesElement.replaceChildren();if(!nodes.length){const empty=document.createElement("div");empty.className="empty";empty.textContent="还没有绑定节点。";nodesElement.append(empty);return}for(const node of nodes){const online=node.status==="online",row=document.createElement("div");row.className="node";const main=document.createElement("div");main.className="node-main";const title=document.createElement("div");title.className="node-title";const dot=document.createElement("span");dot.className="status"+(online?" online":"");const link=document.createElement("a");link.href=nodeURL(node);link.textContent=String(node.name||node.id||"");if(!online){link.className="offline";link.addEventListener("click",event=>{event.preventDefault();errorElement.textContent="节点当前离线。"})}const meta=document.createElement("div");meta.className="meta";meta.textContent=String(node.id||"")+" · "+relativeTime(node.last_seen_at);const tools=document.createElement("div");tools.className="node-tools";const state=document.createElement("span");state.className="state";state.textContent=online?"Online":"Offline";const rename=document.createElement("button");rename.type="button";rename.className="node-action";rename.textContent="重命名";rename.addEventListener("click",()=>renameNode(node));const remove=document.createElement("button");remove.type="button";remove.className="node-action node-delete";remove.textContent="删除";remove.addEventListener("click",()=>deleteNode(node));title.append(dot,link);main.append(title,meta);tools.append(state,rename,remove);row.append(main,tools);nodesElement.append(row)}}
async function api(path,init){const response=await fetch(path,{credentials:"include",...init});const body=await response.json().catch(()=>({}));if(response.status===401){location.replace("/login?next="+encodeURIComponent(location.pathname+location.search));throw new Error("unauthorized")}if(!response.ok)throw new Error(body.error||"request_failed");return body}
async function load(){errorElement.textContent="";try{const nodes=await api("/api/nodes");syncLauncherNodes(nodes);render(Array.isArray(nodes)?nodes:[])}catch(error){if(error.message!=="unauthorized")errorElement.textContent="节点列表加载失败。"}}async function renameNode(node){const name=window.prompt("节点名称",String(node.name||""));if(name===null)return;const trimmed=name.trim();if(!trimmed)return;await api("/api/nodes/"+encodeURIComponent(node.id),{method:"PATCH",headers:{"Content-Type":"application/json"},body:JSON.stringify({name:trimmed})});await load()}async function deleteNode(node){if(!window.confirm("删除节点 "+String(node.name||node.id||"")+"？"))return;await api("/api/nodes/"+encodeURIComponent(node.id),{method:"DELETE"});await load()}
async function loadUser(){try{const user=await api("/api/auth/me");userElement.textContent=user.email||user.name||""}catch{}}document.getElementById("refresh").addEventListener("click",load);document.getElementById("logout").addEventListener("click",async()=>{try{await api("/api/auth/logout",{method:"POST"});location.replace("/login")}catch(error){if(error.message!=="unauthorized")errorElement.textContent="退出失败，请重试。"}});document.getElementById("change-password").addEventListener("click",async()=>{const current=window.prompt("当前 Relay 密码");if(current===null)return;const next=window.prompt("新 Relay 密码（8-128 个字符）");if(next===null)return;try{await api("/api/auth/password/change",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({current_password:current,new_password:next})});errorElement.textContent="密码已修改。"}catch(error){if(error.message!=="unauthorized")errorElement.textContent="密码修改失败。"}});loadUser();load();window.setInterval(load,15000);
</script></body></html>`))

func (a *App) handleBrowserLogin(w http.ResponseWriter, r *http.Request) {
	setBrowserNoStoreHeaders(w)
	target := safeRelayRedirect(r.URL.Query().Get("next"))
	if target == "" {
		target = "/nodes"
	}
	if _, _, err := a.authenticateUser(r); err == nil {
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	} else if !errors.Is(err, identity.ErrAuthRequired) {
		http.Error(w, "request failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	_ = browserLoginPage.Execute(w, browserLoginPageData{Next: target})
}

func (a *App) handleBrowserNodes(w http.ResponseWriter, r *http.Request) {
	setBrowserNoStoreHeaders(w)
	if _, _, err := a.authenticateUser(r); errors.Is(err, identity.ErrAuthRequired) {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	} else if err != nil {
		http.Error(w, "request failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	_ = browserNodesPage.Execute(w, nil)
}

func (a *App) handleBrowserAuthStatus(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.authenticateUser(r)
	if err != nil {
		if errors.Is(err, identity.ErrAuthRequired) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"id": user.ID, "email": user.Email, "name": user.Email})
}

func (a *App) handleBrowserLogout(w http.ResponseWriter, r *http.Request) {
	if !a.requireSameOrigin(w, r) {
		return
	}
	if cookie, err := r.Cookie(userSessionCookie); err == nil {
		if err := a.identity.Logout(r.Context(), cookie.Value); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
			return
		}
	}
	a.clearUserSessionCookie(w)
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}
