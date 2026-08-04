package app

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strings"

	"mindfs-cloud/internal/binding"
	"mindfs-cloud/internal/store"
)

const adminSessionCookie = "mindfs_cloud_session"

var bindPage = template.Must(template.New("bind").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>MindFS Cloud Relay</title>
<style>
body{font-family:system-ui,sans-serif;max-width:520px;margin:48px auto;padding:0 20px;color:#17202a}h1{font-size:24px}form{display:grid;gap:12px;margin-top:20px}input,button{font:inherit;padding:10px 12px;border:1px solid #aeb6bf;border-radius:6px}button{cursor:pointer;background:#1769aa;color:white;border-color:#1769aa}button.secondary{background:white;color:#922b21;border-color:#922b21}.actions{display:flex;gap:10px}.actions button{flex:1}#message{min-height:24px;color:#566573}.hidden{display:none}
</style>
</head>
<body>
<h1>MindFS Cloud Relay</h1>
<p id="message">Checking binding status...</p>
<form id="login" class="hidden">
<input id="username" autocomplete="username" placeholder="Username" required>
<input id="password" type="password" autocomplete="current-password" placeholder="Password" required>
<button type="submit">Sign in</button>
</form>
<form id="confirm" class="hidden">
<label>Node name <input id="nodeName" required></label>
<div class="actions"><button type="submit">Confirm</button><button type="button" id="reject" class="secondary">Reject</button></div>
</form>
<p><a id="nodeLink" class="hidden">Open node</a></p>
<script>
const params=new URLSearchParams(location.search);const code=params.get('code')||'';const root=params.get('root')||'';const hintedName=params.get('node_name')||'';let csrf='';
const message=document.getElementById('message'),login=document.getElementById('login'),confirmForm=document.getElementById('confirm'),nodeName=document.getElementById('nodeName'),nodeLink=document.getElementById('nodeLink');nodeName.value=hintedName;
async function status(){const q=new URLSearchParams({code});if(hintedName)q.set('node_name',hintedName);if(root)q.set('root',root);const r=await fetch('/api/bind/status?'+q);if(r.status===401){login.classList.remove('hidden');message.textContent='Administrator sign-in required.';return}const body=await r.json();if(!r.ok){message.textContent=body.message||body.error;return}login.classList.add('hidden');if(body.node_name&&!nodeName.value)nodeName.value=body.node_name;if(body.status==='waiting_for_device'){message.textContent='Waiting for the MindFS node...';setTimeout(status,1500);return}if(body.status==='pending'){message.textContent='Node is ready for confirmation.';confirmForm.classList.remove('hidden');return}message.textContent='Binding status: '+body.status;confirmForm.classList.add('hidden')}
login.addEventListener('submit',async e=>{e.preventDefault();const r=await fetch('/api/cloud/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:username.value,password:password.value})});const body=await r.json();if(!r.ok){message.textContent=body.message||body.error;return}csrf=body.csrf_token;await status()});
async function decide(action){const r=await fetch('/api/bind/confirm',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify({code,action,node_name:nodeName.value})});const body=await r.json();if(!r.ok){message.textContent=body.message||body.error;return}confirmForm.classList.add('hidden');message.textContent='Binding '+body.status+'.';if(body.node_url){const u=new URL(body.node_url,location.origin);if(root)u.searchParams.set('root',root);nodeLink.href=u;nodeLink.classList.remove('hidden')}}
confirmForm.addEventListener('submit',e=>{e.preventDefault();decide('confirm')});document.getElementById('reject').addEventListener('click',()=>decide('reject'));status();
</script>
</body>
</html>`))

func (a *App) handleBindPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := bindPage.Execute(w, nil); err != nil {
		return
	}
}

func (a *App) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON request")
		return
	}
	sessionToken, csrfToken, err := a.auth.Login(r.Context(), input.Username, input.Password)
	if errors.Is(err, binding.ErrAuthRequired) {
		respondError(w, r, http.StatusUnauthorized, "auth_required", "authentication required")
		return
	}
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "internal_error", "unable to create session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.PublicURL.Scheme == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.config.AdminSessionTTL.Seconds()),
	})
	respondJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":       "usr_bootstrap",
			"username": a.config.AdminUsername,
			"roles":    []string{"owner"},
		},
		"csrf_token": csrfToken,
	})
}

func (a *App) handleBindPoll(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.URL.Query().Get("purpose")) != "" {
		respondError(w, r, http.StatusBadRequest, "invalid_request", "binding purpose is not supported")
		return
	}
	response, err := a.binding.Poll(r.Context(), r.URL.Query().Get("code"), r.Header.Get("X-MindFS-Device-ID"))
	if errors.Is(err, binding.ErrInvalidCode) {
		respondError(w, r, http.StatusBadRequest, "invalid_bind_code", "invalid binding code or device ID")
		return
	}
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "internal_error", "binding poll failed")
		return
	}
	respondJSON(w, http.StatusOK, response)
}

func (a *App) handleBindStatus(w http.ResponseWriter, r *http.Request) {
	if _, err := a.authenticateAdmin(r); err != nil {
		respondError(w, r, http.StatusUnauthorized, "auth_required", "authentication required")
		return
	}
	response, err := a.binding.Status(r.Context(), r.URL.Query().Get("code"))
	if errors.Is(err, binding.ErrInvalidCode) {
		respondError(w, r, http.StatusBadRequest, "invalid_bind_code", "invalid binding code")
		return
	}
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "internal_error", "binding status failed")
		return
	}
	if response.NodeName == "" {
		response.NodeName = strings.TrimSpace(r.URL.Query().Get("node_name"))
	}
	respondJSON(w, http.StatusOK, response)
}

func (a *App) handleBindConfirm(w http.ResponseWriter, r *http.Request) {
	session, err := a.authenticateAdmin(r)
	if err != nil {
		respondError(w, r, http.StatusUnauthorized, "auth_required", "authentication required")
		return
	}
	if err := a.auth.AuthorizeCSRF(session, r.Header.Get("X-CSRF-Token")); err != nil {
		respondError(w, r, http.StatusForbidden, "access_denied", "invalid CSRF token")
		return
	}
	var input struct {
		Code     string `json:"code"`
		Action   string `json:"action"`
		NodeName string `json:"node_name"`
	}
	if err := decodeJSON(r, &input); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON request")
		return
	}
	switch strings.TrimSpace(input.Action) {
	case "confirm":
		response, err := a.binding.Confirm(r.Context(), input.Code, input.NodeName)
		if handleBindingDecisionError(w, r, err) {
			return
		}
		respondJSON(w, http.StatusOK, response)
	case "reject":
		if handleBindingDecisionError(w, r, a.binding.Revoke(r.Context(), input.Code)) {
			return
		}
		respondJSON(w, http.StatusOK, map[string]string{"status": string(store.BindRevoked)})
	default:
		respondError(w, r, http.StatusBadRequest, "invalid_request", "action must be confirm or reject")
	}
}

func (a *App) authenticateAdmin(r *http.Request) (store.AdminSession, error) {
	cookie, err := r.Cookie(adminSessionCookie)
	if err != nil {
		return store.AdminSession{}, binding.ErrAuthRequired
	}
	return a.auth.Authenticate(r.Context(), cookie.Value)
}

func handleBindingDecisionError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, binding.ErrInvalidCode):
		respondError(w, r, http.StatusBadRequest, "invalid_bind_code", "invalid binding code")
	case errors.Is(err, binding.ErrClaimed):
		respondError(w, r, http.StatusConflict, "bind_claimed", "binding code already claimed")
	case errors.Is(err, binding.ErrExpired):
		respondError(w, r, http.StatusConflict, "bind_expired", "binding code expired")
	case errors.Is(err, binding.ErrRevoked):
		respondError(w, r, http.StatusConflict, "bind_revoked", "binding code revoked")
	default:
		respondError(w, r, http.StatusInternalServerError, "internal_error", "binding operation failed")
	}
	return true
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}
