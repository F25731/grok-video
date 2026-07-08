package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
		"recent_tasks":            s.tasks.Recent(20),
	})
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
  <title>Grok Video Wrapper 后台</title>
  <style>
    *{box-sizing:border-box}body{margin:0;background:#f5f7fb;color:#172033;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei",sans-serif}.wrap{max-width:1080px;margin:0 auto;padding:28px 18px 48px}.top{display:flex;align-items:center;justify-content:space-between;margin-bottom:18px}.title{font-size:24px;font-weight:750}.sub{color:#687386;font-size:13px;margin-top:5px}.grid{display:grid;grid-template-columns:1.05fr .95fr;gap:16px}.card{background:#fff;border:1px solid #e6ebf2;border-radius:10px;padding:18px;box-shadow:0 8px 24px rgba(20,35,60,.06)}.wide{grid-column:1/-1}h2{font-size:16px;margin:0 0 14px}.row{display:grid;gap:8px;margin:12px 0}label{font-size:13px;color:#475569}input{width:100%;height:40px;border:1px solid #d8e0ea;border-radius:8px;padding:0 11px;font-size:14px;background:#fff}button{height:40px;border:0;border-radius:8px;background:#111827;color:#fff;padding:0 16px;font-weight:650;cursor:pointer}button.secondary{background:#edf1f7;color:#172033}.actions{display:flex;gap:10px;flex-wrap:wrap}.kv{display:grid;grid-template-columns:150px 1fr;gap:9px 12px;font-size:14px}.k{color:#687386}.v{word-break:break-all}.ok{color:#087443;font-weight:700}.bad{color:#c02626;font-weight:700}.stats{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}.stat{background:#f8fafc;border:1px solid #edf1f7;border-radius:8px;padding:12px}.num{font-size:20px;font-weight:760}.name{font-size:12px;color:#687386;margin-top:3px}.model{border:1px solid #edf1f7;border-radius:8px;padding:12px;margin-top:10px;background:#fbfcfe}.model b{display:block;margin-bottom:7px}.tag{display:inline-block;background:#eef2ff;color:#3730a3;border-radius:999px;padding:3px 8px;font-size:12px;margin:3px 4px 0 0}.taskHead,.taskRow{display:grid;grid-template-columns:minmax(220px,1.6fr) minmax(140px,.8fr) 100px 70px 60px;gap:10px;align-items:center}.taskHead{color:#687386;font-size:12px;padding:8px 10px}.taskRow{border-top:1px solid #edf1f7;font-size:13px;padding:10px}.taskRow span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.login{max-width:420px;margin:90px auto}.hide{display:none}.msg{font-size:13px;margin-top:10px}.hint{background:#f8fafc;border:1px solid #edf1f7;border-radius:8px;padding:10px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:13px;white-space:pre-wrap;word-break:break-all}@media(max-width:820px){.grid{grid-template-columns:1fr}.stats{grid-template-columns:repeat(2,1fr)}.kv{grid-template-columns:1fr}.taskHead{display:none}.taskRow{grid-template-columns:1fr;gap:4px}}
  </style>
</head>
<body>
  <main id="login" class="wrap login">
    <div class="card">
      <h2>后台登录</h2>
      <div class="row"><label>用户名</label><input id="username" autocomplete="username" value="admin"></div>
      <div class="row"><label>密码</label><input id="password" type="password" autocomplete="current-password"></div>
      <button onclick="doLogin()">登录</button>
      <div id="loginMsg" class="msg bad"></div>
    </div>
  </main>
  <main id="app" class="wrap hide">
    <div class="top">
      <div><div class="title">Grok Video Wrapper 后台</div><div class="sub">配置上游密钥、查看 NewAPI 接入信息和 worker 状态</div></div>
      <button class="secondary" onclick="refreshAll()">刷新</button>
    </div>
    <div class="grid">
      <section class="card">
        <h2>上游配置</h2>
        <div class="kv">
          <div class="k">上游地址</div><div class="v" id="upstreamBase"></div>
          <div class="k">密钥状态</div><div class="v" id="keyState"></div>
          <div class="k">当前密钥</div><div class="v" id="keyMasked"></div>
        </div>
        <div class="row"><label>上游 API Key</label><input id="upstreamKey" type="password" placeholder="粘贴新的上游密钥，留空保存则保持不变"></div>
        <div class="actions"><button onclick="saveConfig(false)">保存密钥</button><button class="secondary" onclick="saveConfig(true)">清空密钥</button></div>
        <div id="saveMsg" class="msg"></div>
      </section>
      <section class="card">
        <h2>NewAPI 配置</h2>
        <div class="hint" id="newapiHint"></div>
      </section>
      <section class="card">
        <h2>任务状态</h2>
        <div class="stats" id="stats"></div>
        <div class="sub" id="workerHint"></div>
      </section>
      <section class="card">
        <h2>支持模型</h2>
        <div id="models"></div>
      </section>
      <section class="card wide">
        <h2>最近任务</h2>
        <div id="tasks"></div>
      </section>
    </div>
  </main>
  <script>
    const $ = id => document.getElementById(id);
    async function api(url, options){
      const res = await fetch(url, Object.assign({headers:{'Content-Type':'application/json'}}, options || {}));
      const data = await res.json().catch(()=>({}));
      if(!res.ok) throw new Error(data.error?.message || '请求失败');
      return data;
    }
    async function doLogin(){
      $('loginMsg').textContent = '';
      try{
        await api('/api/admin/login',{method:'POST',body:JSON.stringify({username:$('username').value,password:$('password').value})});
        await refreshAll();
      }catch(e){$('loginMsg').textContent = e.message}
    }
    async function refreshAll(){
      try{
        const cfg = await api('/api/admin/config');
        const status = await api('/api/admin/status');
        $('login').classList.add('hide'); $('app').classList.remove('hide');
        $('upstreamBase').textContent = cfg.upstream_base_url;
        $('keyState').innerHTML = cfg.upstream_key_configured ? '<span class="ok">已配置</span>' : '<span class="bad">未配置</span>';
        $('keyMasked').textContent = cfg.upstream_key_masked || '-';
        $('newapiHint').textContent = '渠道类型: OpenAI\nBase URL: '+location.origin+'/v1\nAPI Key: '+(cfg.wrapper_api_key || '(未设置 WRAPPER_API_KEY)')+'\n模型: grok-image-video,grok-video-1.5';
        renderStats(status.tasks || {}, status.worker || {});
        renderTasks(status.recent_tasks || []);
        renderModels(cfg.models || []);
      }catch(e){
        $('app').classList.add('hide'); $('login').classList.remove('hide');
      }
    }
    async function saveConfig(clear){
      $('saveMsg').textContent = '';
      try{
        await api('/api/admin/config',{method:'POST',body:JSON.stringify({upstream_api_key:$('upstreamKey').value,clear})});
        $('upstreamKey').value = '';
        $('saveMsg').className = 'msg ok'; $('saveMsg').textContent = '保存成功，下一次请求立即生效';
        await refreshAll();
      }catch(e){$('saveMsg').className = 'msg bad'; $('saveMsg').textContent = e.message}
    }
    function renderStats(tasks, worker){
      const items = [['total','总任务'],['queued','排队中'],['in_progress','运行中'],['completed','完成'],['failed','失败']];
      $('stats').innerHTML = items.map(([k,n])=>'<div class="stat"><div class="num">'+(tasks[k] ?? 0)+'</div><div class="name">'+n+'</div></div>').join('');
      $('workerHint').textContent = 'worker '+(worker.workers ?? 0)+'；队列 '+(worker.queued ?? 0)+' / '+(worker.queue ?? 0)+'；拒绝 '+(worker.rejected ?? 0)+'。worker 完成数只代表 HTTP 工作次数，不代表任务数量。';
    }
    function renderTasks(list){
      if(!list.length){$('tasks').innerHTML = '<div class="sub">暂无任务</div>'; return}
      $('tasks').innerHTML = '<div class="taskHead"><span>任务</span><span>模型</span><span>状态</span><span>进度</span><span>轮询</span></div>'+list.map(t=>{
        const cls = t.status === 'completed' ? 'ok' : t.status === 'failed' ? 'bad' : '';
        return '<div class="taskRow"><span title="'+esc(t.id)+'">'+esc(t.id)+'</span><span>'+esc(t.model || '-')+'</span><span class="'+cls+'">'+esc(t.status || '-')+'</span><span>'+Number(t.progress || 0)+'%</span><span>'+Number(t.polls || 0)+'</span></div>';
      }).join('');
    }
    function renderModels(list){
      $('models').innerHTML = list.map(m=>'<div class="model"><b>'+m.id+'</b><div>参考图: '+m.max_images+' 张；最长: '+m.max_seconds+' 秒</div><div>'+m.ratios.map(r=>'<span class="tag">'+r+'</span>').join('')+'</div></div>').join('');
    }
    refreshAll();
    setInterval(refreshAll, 3000);
    function esc(v){return String(v ?? '').replace(/[&<>"']/g,m=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[m]))}
  </script>
</body>
</html>`
