package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminHTML))
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if strings.TrimSpace(req.Username) != s.cfg.AdminUsername || req.Password != s.cfg.AdminPassword {
		writeError(w, http.StatusUnauthorized, "invalid admin account")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "grok_admin_session",
		Value:    s.adminSessionToken(),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) withAdminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("grok_admin_session")
		if err != nil || !hmac.Equal([]byte(cookie.Value), []byte(s.adminSessionToken())) {
			writeError(w, http.StatusUnauthorized, "admin login required")
			return
		}
		next(w, r)
	}
}

func (s *Server) adminConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.runtime.Get()
		writeJSON(w, http.StatusOK, map[string]any{
			"upstream_base_url":       s.cfg.UpstreamBaseURL,
			"upstream_key_configured": cfg.UpstreamAPIKey != "",
			"upstream_key_masked":     maskKey(cfg.UpstreamAPIKey),
			"wrapper_api_key":         s.cfg.WrapperAPIKey,
			"models":                  adminModels(),
		})
	case http.MethodPost:
		var req struct {
			UpstreamAPIKey string `json:"upstream_api_key"`
			Clear          bool   `json:"clear"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		next := s.runtime.Get()
		if req.Clear {
			next.UpstreamAPIKey = ""
		} else if strings.TrimSpace(req.UpstreamAPIKey) != "" {
			next.UpstreamAPIKey = req.UpstreamAPIKey
		}
		if err := s.runtime.Save(next); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "upstream_key_masked": maskKey(next.UpstreamAPIKey)})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cfg := s.runtime.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                      true,
		"upstream_key_configured": cfg.UpstreamAPIKey != "",
		"worker":                  s.pool.Stats(),
		"tasks":                   s.tasks.Stats(),
		"recent_tasks":            s.tasks.Recent(80),
	})
}

func (s *Server) adminTaskStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	ch, unsubscribe := s.tasks.Subscribe(80)
	defer unsubscribe()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case list := <-ch:
			payload, _ := json.Marshal(map[string]any{
				"tasks":        s.tasks.Stats(),
				"worker":       s.pool.Stats(),
				"recent_tasks": list,
			})
			_, _ = fmt.Fprintf(w, "event: tasks\ndata: %s\n\n", payload)
			flusher.Flush()
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) adminTaskPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	taskID := strings.TrimSpace(r.URL.Query().Get("id"))
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task id is required")
		return
	}
	for _, task := range s.tasks.Recent(1000) {
		if task.ID == taskID && strings.TrimSpace(task.ResultURL) != "" {
			http.Redirect(w, r, task.ResultURL, http.StatusFound)
			return
		}
	}
	writeError(w, http.StatusNotFound, "task result url not found")
}

func (s *Server) adminSessionToken() string {
	mac := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	mac.Write([]byte(s.cfg.AdminUsername + ":" + s.cfg.AdminPassword))
	return hex.EncodeToString(mac.Sum(nil))
}

func maskKey(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 10 {
		if value == "" {
			return ""
		}
		return "******"
	}
	return value[:6] + "..." + value[len(value)-4:]
}

func adminModels() []map[string]any {
	specs := modelSpecs()
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		ratios := make([]string, 0, len(spec.Ratios))
		for ratio := range spec.Ratios {
			ratios = append(ratios, ratio)
		}
		out = append(out, map[string]any{
			"id":            spec.ID,
			"max_images":    spec.MaxImages,
			"require_image": spec.RequireImage,
			"text_to_video": spec.TextToVideo,
			"max_seconds":   spec.MaxSeconds,
			"multi_max_sec": spec.MultiMaxSec,
			"ratios":        ratios,
		})
	}
	return out
}

const adminHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Grok Video Wrapper Admin</title>
  <style>
    *{box-sizing:border-box}body{margin:0;background:#f4f6fb;color:#172033;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei",sans-serif}.wrap{max-width:1380px;margin:0 auto;padding:24px 18px 44px}.top{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-bottom:18px}.title{font-size:24px;font-weight:760}.sub{color:#687386;font-size:13px;margin-top:5px}.grid{display:grid;grid-template-columns:320px minmax(0,1fr);gap:16px;align-items:start}.side{display:grid;gap:14px;position:sticky;top:16px}.main{display:grid;gap:14px}.card{background:#fff;border:1px solid #e6ebf2;border-radius:10px;padding:16px;box-shadow:0 8px 24px rgba(20,35,60,.06)}h2{font-size:16px;margin:0 0 12px}.row{display:grid;gap:8px;margin:12px 0}label{font-size:13px;color:#475569}input{width:100%;height:40px;border:1px solid #d8e0ea;border-radius:8px;padding:0 11px;font-size:14px;background:#fff}button,.btn{display:inline-flex;align-items:center;justify-content:center;height:34px;border:0;border-radius:8px;background:#111827;color:#fff;padding:0 12px;font-size:13px;font-weight:650;cursor:pointer;text-decoration:none}button.secondary,.btn.secondary{background:#edf1f7;color:#172033}.actions{display:flex;gap:8px;flex-wrap:wrap}.kv{display:grid;grid-template-columns:88px 1fr;gap:8px 10px;font-size:13px}.k{color:#687386}.v{word-break:break-all}.ok{color:#087443;font-weight:700}.bad{color:#c02626;font-weight:700}.run{color:#2563eb;font-weight:700}.stats{display:grid;grid-template-columns:repeat(2,1fr);gap:8px}.stat{background:#f8fafc;border:1px solid #edf1f7;border-radius:8px;padding:10px}.num{font-size:20px;font-weight:760}.name{font-size:12px;color:#687386;margin-top:3px}.hint{background:#f8fafc;border:1px solid #edf1f7;border-radius:8px;padding:10px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px;white-space:pre-wrap;word-break:break-all}.login{max-width:420px;margin:90px auto}.hide{display:none}.msg{font-size:13px;margin-top:10px}.pill{display:inline-flex;align-items:center;border-radius:999px;padding:2px 8px;font-size:12px;background:#eef2ff;color:#3730a3}.taskList{display:grid;gap:10px}.taskCard{border:1px solid #e6ebf2;border-radius:10px;background:#fff;padding:12px}.taskTop{display:flex;justify-content:space-between;gap:10px;align-items:flex-start}.taskId{font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px;word-break:break-all}.prompt{margin:8px 0 0;color:#374151;font-size:13px;line-height:1.45;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}.meta{display:flex;gap:6px;flex-wrap:wrap;margin-top:8px}.media{display:grid;grid-template-columns:120px 1fr;gap:10px;margin-top:10px;align-items:center}.thumbs{display:flex;gap:6px;overflow:hidden}.thumbs img{width:42px;height:42px;border-radius:6px;object-fit:cover;border:1px solid #e5e7eb}.videoBox{height:86px;border-radius:8px;background:#0f172a;display:grid;place-items:center;overflow:hidden}.videoBox video{width:100%;height:100%;object-fit:contain}.empty{color:#94a3b8;font-size:13px;text-align:center;padding:18px}.table{display:grid;gap:8px}.record{display:grid;grid-template-columns:minmax(190px,1.15fr) 120px 90px minmax(160px,1fr) 128px;gap:10px;align-items:center;border:1px solid #e6ebf2;border-radius:10px;background:#fff;padding:10px;font-size:13px}.record .url{font-family:ui-monospace,Menlo,Consolas,monospace;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.toolbar{display:flex;align-items:center;justify-content:space-between;gap:10px}.search{max-width:360px}.model{border:1px solid #edf1f7;border-radius:8px;padding:10px;margin-top:8px;background:#fbfcfe}.tag{display:inline-block;background:#eef2ff;color:#3730a3;border-radius:999px;padding:3px 8px;font-size:12px;margin:3px 4px 0 0}@media(max-width:980px){.grid{grid-template-columns:1fr}.side{position:static}.record{grid-template-columns:1fr}.media{grid-template-columns:1fr}}
  </style>
</head>
<body>
  <main id="login" class="wrap login">
    <div class="card">
      <h2>Admin Login</h2>
      <div class="row"><label>Username</label><input id="username" autocomplete="username" value="admin"></div>
      <div class="row"><label>Password</label><input id="password" type="password" autocomplete="current-password"></div>
      <button onclick="doLogin()">Login</button>
      <div id="loginMsg" class="msg bad"></div>
    </div>
  </main>
  <main id="app" class="wrap hide">
    <div class="top">
      <div><div class="title">Grok Video Wrapper Admin</div><div class="sub">Left: live status. Right: persisted task records with preview links.</div></div>
      <div class="actions"><button class="secondary" onclick="refreshAll()">Refresh</button></div>
    </div>
    <div class="grid">
      <aside class="side">
        <section class="card"><h2>Live Status</h2><div class="stats" id="stats"></div><div class="sub" id="workerHint"></div><div class="msg" id="streamState">SSE disconnected</div></section>
        <section class="card"><h2>Upstream Config</h2><div class="kv"><div class="k">Base URL</div><div class="v" id="upstreamBase"></div><div class="k">Key</div><div class="v" id="keyState"></div><div class="k">Masked</div><div class="v" id="keyMasked"></div></div><div class="row"><label>Upstream API Key</label><input id="upstreamKey" type="password" placeholder="Paste a new upstream key; empty keeps current key"></div><div class="actions"><button onclick="saveConfig(false)">Save Key</button><button class="secondary" onclick="saveConfig(true)">Clear Key</button></div><div id="saveMsg" class="msg"></div></section>
        <section class="card"><h2>NewAPI Config</h2><div class="hint" id="newapiHint"></div></section>
        <section class="card"><h2>Models</h2><div id="models"></div></section>
      </aside>
      <section class="main">
        <div class="card"><div class="toolbar"><h2>Live Task Cards</h2><span class="sub" id="liveCount"></span></div><div id="liveTasks" class="taskList"></div></div>
        <div class="card"><div class="toolbar"><h2>Records</h2><input id="search" class="search" placeholder="Search task id, model, prompt, result URL" oninput="renderAll()"></div><div id="records" class="table"></div></div>
      </section>
    </div>
  </main>
  <script>
    const $ = id => document.getElementById(id);
    let tasks = [];
    let source = null;
    async function api(url, options){
      const res = await fetch(url, Object.assign({headers:{'Content-Type':'application/json'}}, options || {}));
      const data = await res.json().catch(()=>({}));
      if(!res.ok) throw new Error(data.error?.message || 'Request failed');
      return data;
    }
    async function doLogin(){
      $('loginMsg').textContent = '';
      try{ await api('/api/admin/login',{method:'POST',body:JSON.stringify({username:$('username').value,password:$('password').value})}); await refreshAll(); connectStream(); }catch(e){$('loginMsg').textContent = e.message}
    }
    async function refreshAll(){
      try{
        const cfg = await api('/api/admin/config');
        const status = await api('/api/admin/status');
        $('login').classList.add('hide'); $('app').classList.remove('hide');
        $('upstreamBase').textContent = cfg.upstream_base_url;
        $('keyState').innerHTML = cfg.upstream_key_configured ? '<span class="ok">configured</span>' : '<span class="bad">missing</span>';
        $('keyMasked').textContent = cfg.upstream_key_masked || '-';
        $('newapiHint').textContent = 'Channel: OpenAI\nBase URL: '+location.origin+'/v1\nAPI Key: '+(cfg.wrapper_api_key || '(WRAPPER_API_KEY missing)')+'\nModels: grok-image-video,grok-video-1.5';
        tasks = status.recent_tasks || [];
        renderStats(status.tasks || {}, status.worker || {});
        renderModels(cfg.models || []);
        renderAll();
      }catch(e){ $('app').classList.add('hide'); $('login').classList.remove('hide'); }
    }
    function connectStream(){
      if(source) source.close();
      source = new EventSource('/api/admin/tasks/stream');
      source.addEventListener('open',()=>{$('streamState').className='msg ok';$('streamState').textContent='SSE connected. Task changes refresh automatically.'});
      source.addEventListener('error',()=>{$('streamState').className='msg bad';$('streamState').textContent='SSE disconnected. Browser will retry.'});
      source.addEventListener('tasks',event=>{ const data = JSON.parse(event.data || '{}'); tasks = data.recent_tasks || []; renderStats(data.tasks || {}, data.worker || {}); renderAll(); });
    }
    async function saveConfig(clear){
      $('saveMsg').textContent = '';
      try{ await api('/api/admin/config',{method:'POST',body:JSON.stringify({upstream_api_key:$('upstreamKey').value,clear})}); $('upstreamKey').value = ''; $('saveMsg').className = 'msg ok'; $('saveMsg').textContent = 'Saved. Next request uses the new config.'; await refreshAll(); }catch(e){$('saveMsg').className = 'msg bad'; $('saveMsg').textContent = e.message}
    }
    function renderStats(t, worker){
      const items = [['total','Total'],['queued','Queued'],['in_progress','Running'],['completed','Done'],['failed','Failed']];
      $('stats').innerHTML = items.map(([k,n])=>'<div class="stat"><div class="num">'+(t[k] ?? 0)+'</div><div class="name">'+n+'</div></div>').join('');
      $('workerHint').textContent = 'workers '+(worker.workers ?? 0)+'; queue '+(worker.queued ?? 0)+' / '+(worker.queue ?? 0)+'; rejected '+(worker.rejected ?? 0);
    }
    function renderAll(){ const q = $('search') ? $('search').value.trim().toLowerCase() : ''; const list = q ? tasks.filter(t => [t.id,t.model,t.prompt,t.result_url,t.status].some(v => String(v || '').toLowerCase().includes(q))) : tasks; renderLive(list); renderRecords(list); }
    function renderLive(list){ const live = list.filter(t => t.status !== 'completed' && t.status !== 'failed').slice(0, 12); $('liveCount').textContent = live.length ? live.length+' running' : 'No running tasks'; $('liveTasks').innerHTML = live.length ? live.map(taskCard).join('') : '<div class="empty">No running tasks</div>'; }
    function renderRecords(list){ const records = list.slice(0, 80); $('records').innerHTML = records.length ? records.map(recordRow).join('') : '<div class="empty">No records</div>'; }
    function taskCard(t){ const cls = statusClass(t.status); return '<div class="taskCard"><div class="taskTop"><div><div class="taskId">'+esc(t.id)+'</div><div class="prompt">'+esc(t.prompt || 'No prompt recorded')+'</div></div><span class="'+cls+'">'+statusText(t.status)+'</span></div><div class="meta">'+meta(t).join('')+'</div>'+media(t)+'</div>'; }
    function recordRow(t){ const url = t.result_url || ''; const preview = url ? '<a class="btn" target="_blank" href="/api/admin/tasks/preview?id='+encodeURIComponent(t.id)+'">Preview</a>' : '<button class="secondary" disabled>No result</button>'; const copy = url ? '<button class="secondary copyBtn" data-url="'+escAttr(url)+'">Copy URL</button>' : ''; return '<div class="record"><div><div class="taskId">'+esc(t.id)+'</div><div class="prompt">'+esc(t.prompt || '-')+'</div></div><div>'+esc(t.model || '-')+'</div><div class="'+statusClass(t.status)+'">'+statusText(t.status)+'</div><div class="url" title="'+escAttr(url)+'">'+esc(url || t.error || '-')+'</div><div class="actions">'+preview+copy+'</div></div>'; }
    function media(t){ const imgs = (t.image_urls || []).slice(0,4).map(url=>'<img src="'+escAttr(url)+'" alt="reference">').join(''); const video = t.result_url ? '<div class="videoBox"><video muted preload="metadata" src="'+escAttr(t.result_url)+'"></video></div>' : '<div class="videoBox"><span class="empty">Waiting</span></div>'; return '<div class="media"><div class="thumbs">'+(imgs || '<span class="sub">No refs</span>')+'</div>'+video+'</div>'; }
    function meta(t){ return ['model '+(t.model||'-'),(t.seconds||'-')+'s',t.aspect_ratio||'-',t.resolution||'-','progress '+Number(t.progress||0)+'%'].map(v=>'<span class="pill">'+esc(v)+'</span>'); }
    function renderModels(list){ $('models').innerHTML = list.map(m=>'<div class="model"><b>'+m.id+'</b><div class="sub">refs '+m.max_images+'; max '+m.max_seconds+'s</div><div>'+m.ratios.map(r=>'<span class="tag">'+r+'</span>').join('')+'</div></div>').join(''); }
    async function copyText(text){await navigator.clipboard.writeText(text)}
    function statusText(s){return ({completed:'Done',failed:'Failed',in_progress:'Running',queued:'Queued'}[s] || s || '-')}
    function statusClass(s){return s === 'completed' ? 'ok' : s === 'failed' ? 'bad' : 'run'}
    function esc(v){return String(v ?? '').replace(/[&<>"']/g,m=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]))}
    function escAttr(v){return esc(v)}
    document.addEventListener('click',event=>{ const btn = event.target.closest('.copyBtn'); if(btn) copyText(btn.dataset.url || ''); });
    refreshAll().then(connectStream);
  </script>
</body>
</html>`
