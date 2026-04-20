package proxy

import (
	"fmt"
	"html"
	"net/http"
	"strings"
)

// errorPage renders a Catppuccin-Mocha, anime-pixel-art-themed error page.
// Inspired by 90s anime "system error" screens — bouncy ASCII mascot, scanlines,
// pixel font, glitchy color stripes.
type errorMascot struct {
	art   string
	color string // CSS color
	mood  string // dialogue
}

var mascots = map[int]errorMascot{
	502: {
		color: "#f38ba8",
		mood:  "Senpai... the upstream isn't answering...",
		art: `        ／＞　 フ
       | 　_　_| 
     ／｀ミ＿xノ 
    /　　　　 |
   /　 ヽ　　 ﾉ
  │　　|　|　|
／￣|　　 |　|　|
( ￣ヽ＿_ヽ_)__)
＼二)`,
	},
	404: {
		color: "#f9e2af",
		mood:  "I searched everywhere... nothing here, nya~",
		art: `      /\_____/\
     /  o   o  \
    ( ==  ^  == )
     )         (
    (           )
   ( (  )   (  ) )
  (__(__)___(__)__)`,
	},
	503: {
		color: "#fab387",
		mood:  "Z z z... five more minutes...",
		art: `       ／l、    
     （ﾟ､ ｡ ７   
       l  ~ヽ   
       じしf_,)ノ`,
	},
	500: {
		color: "#f38ba8",
		mood:  "S-something exploded inside me... gomen!",
		art: `   (\  /)
  ( o.o )    *kaboom*
   > ^ <`,
	},
}

const errorPageTpl = `<!doctype html>
<html><head><meta charset="utf-8"><title>%d — ring0</title>
<style>
  :root { --bg:#1e1e2e; --panel:#181825; --text:#cdd6f4; --dim:#7f849c; --accent:%s; }
  * { box-sizing: border-box; }
  html, body { margin:0; padding:0; background:var(--bg); color:var(--text); height:100%%;
    font-family: ui-monospace, "SF Mono", Consolas, monospace; }
  body { display:flex; align-items:center; justify-content:center; padding:2rem;
    background:
      repeating-linear-gradient(0deg, rgba(0,0,0,0.18) 0px, rgba(0,0,0,0.18) 1px, transparent 1px, transparent 3px),
      radial-gradient(circle at 20%% 20%%, rgba(243,139,168,0.08), transparent 40%%),
      radial-gradient(circle at 80%% 70%%, rgba(137,180,250,0.08), transparent 40%%),
      var(--bg);
  }
  .frame { border:2px solid var(--accent); padding:1.5rem 2rem; max-width:640px; width:100%%;
    background:var(--panel); position:relative; box-shadow: 0 0 0 4px var(--bg), 0 0 0 6px var(--accent);
    image-rendering: pixelated; }
  .frame::before, .frame::after { content:""; position:absolute; left:-2px; right:-2px; height:6px;
    background: repeating-linear-gradient(90deg, var(--accent) 0 8px, transparent 8px 14px); }
  .frame::before { top:-10px; } .frame::after { bottom:-10px; }
  .code { font-size:5rem; font-weight:900; color:var(--accent); letter-spacing:0.2em; line-height:1;
    text-shadow: 3px 0 0 #89b4fa, -3px 0 0 #f9e2af; margin-bottom:0.25rem; }
  .label { color:var(--dim); text-transform:uppercase; letter-spacing:0.3em; font-size:0.8rem;
    margin-bottom:1.5rem; }
  pre.mascot { color:var(--accent); font-size:0.95rem; line-height:1.05; margin:0 0 1rem 0;
    animation: bob 2s ease-in-out infinite; }
  @keyframes bob { 0%%,100%% { transform: translateY(0); } 50%% { transform: translateY(-3px); } }
  .bubble { background:#313244; border-left:3px solid var(--accent); padding:0.6rem 0.9rem;
    margin:1rem 0; color:var(--text); font-style:italic; }
  .meta { color:var(--dim); font-size:0.8rem; margin-top:1.2rem; word-break: break-all; }
  .meta b { color:var(--text); font-weight:normal; }
  .footer { margin-top:1.5rem; color:var(--dim); font-size:0.75rem; letter-spacing:0.15em;
    text-transform:uppercase; }
  .footer span { color:var(--accent); }
  a { color:#89b4fa; }
</style></head>
<body>
  <div class="frame">
    <div class="code">%d</div>
    <div class="label">%s</div>
    <pre class="mascot">%s</pre>
    <div class="bubble">「 %s 」</div>
    <div class="meta">
      <div><b>path:</b> %s</div>
      <div><b>upstream:</b> %s</div>
      %s
    </div>
    <div class="footer">— served by <span>ring0</span> ✦</div>
  </div>
</body></html>`

func renderErrorPage(w http.ResponseWriter, code int, label, path, upstream, detail string) {
	m, ok := mascots[code]
	if !ok {
		m = mascots[500]
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	detailHTML := ""
	if detail != "" {
		detailHTML = "<div><b>detail:</b> " + html.EscapeString(detail) + "</div>"
	}
	fmt.Fprintf(w, errorPageTpl,
		code, m.color,
		code, html.EscapeString(label),
		html.EscapeString(m.art),
		html.EscapeString(m.mood),
		html.EscapeString(path),
		html.EscapeString(upstream),
		detailHTML,
	)
}

// renderIndex shows the ring0 proxy landing page when no route matched.
// It's the 404 mascot + a list of configured routes.
func renderIndex(w http.ResponseWriter, host, path string, routes []routeEntry) {
	m := mascots[404]
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)

	var routesHTML strings.Builder
	if len(routes) == 0 {
		routesHTML.WriteString(`<li style="color:#7f849c"><em>(no routes configured — press 'a' in the Routes panel)</em></li>`)
	}
	for _, e := range routes {
		h := e.host
		if h == "" {
			h = "*"
		}
		var dst string
		if e.redirect != "" {
			dst = "→ " + e.redirect + " (308)"
		} else {
			dst = "→ " + e.target.String()
			if e.stripPrefix {
				dst += " (strip)"
			}
		}
		fmt.Fprintf(&routesHTML, `<li><code>%s%s</code> %s</li>`,
			html.EscapeString(h), html.EscapeString(e.prefix), html.EscapeString(dst))
	}

	body := `<ul style="list-style:none;padding:0;margin:0;font-size:0.9rem;line-height:1.7">` + routesHTML.String() + `</ul>`
	fmt.Fprintf(w, errorPageTpl,
		404, m.color,
		404, "no route matched",
		html.EscapeString(m.art),
		html.EscapeString(m.mood),
		html.EscapeString(path),
		"none",
		"<div><b>routes:</b></div>"+body,
	)
}
