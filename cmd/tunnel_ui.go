package main

import (
	"fmt"
	"html"
	"strings"
	"time"
)

func renderAWSConfigPage(cfg AWSConfig) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: AWS Config</title>")
	b.WriteString("<style>body{font-family:system-ui,sans-serif;margin:24px}input,textarea,button{font:inherit;padding:8px 10px;margin:4px 0}textarea{width:100%;min-height:140px}label{display:block;font-weight:600;margin-top:10px}.row{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px}</style>")
	b.WriteString("</head><body>")
	b.WriteString(renderPageHeader("AWS Config"))
	b.WriteString("<p>Paste the exact shell command(s) to run in the current container user. This is the source of truth; profile and region are supplied inside the command itself.</p>")
	b.WriteString("<p><a href=\"/command-generator\">Open command generator</a> | <a href=\"/tunnels\">Open tunnel page</a></p>")
	b.WriteString("<form id=\"aws-form\">")
	b.WriteString("<label>Credential bootstrap commands</label><textarea name=\"bootstrapCommand\">" + html.EscapeString(cfg.BootstrapCommand) + "</textarea>")
	b.WriteString("<div><button type=\"submit\">Save AWS config</button><a href=\"/tunnels\" style=\"margin-left:12px\">Open tunnel page</a></div>")
	b.WriteString("</form>")
	b.WriteString("<pre style=\"white-space:pre-wrap;background:#f5f5f5;padding:12px;margin-top:12px\">" + html.EscapeString(cfg.Output) + "</pre>")
	b.WriteString("<div style=\"margin-top:8px;color:#555\">Exit code: " + html.EscapeString(fmt.Sprintf("%d", cfg.ExitCode)) + "</div>")
	b.WriteString("<script>")
	b.WriteString("document.getElementById('aws-form').addEventListener('submit', async function(e){e.preventDefault();const form=new FormData(e.target);const payload={bootstrapCommand:(form.get('bootstrapCommand')||'').toString()};const res=await fetch('/api/aws-config',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});if(!res.ok){alert((await res.text())||'failed');return;}location.reload();});")
	b.WriteString("</script>")
	b.WriteString("</body></html>")
	return b.String()
}

func renderTunnelPage(cfg AWSConfig, tunnels []TunnelEntry) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Tunnels</title>")
	b.WriteString("<style>body{font-family:system-ui,sans-serif;margin:24px}table{border-collapse:collapse;width:100%}th,td{border:1px solid #ccc;padding:8px;vertical-align:top;text-align:left}th{background:#f5f5f5}input,textarea,select,button{font:inherit;padding:8px 10px;margin:4px 0}textarea{width:100%;min-height:120px}label{display:block;font-weight:600;margin-top:10px}.row{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px}.muted{color:#666}.status-ok{color:#0a7}.status-failed{color:#b00}.status-pending{color:#a60}.checkbox-row{display:flex;flex-wrap:wrap;gap:12px;align-items:center;padding-top:6px}.checkbox-row label{display:flex;align-items:center;gap:8px;font-weight:600;margin-top:0}</style>")
	b.WriteString("</head><body>")
	b.WriteString(renderPageHeader("Tunnels"))
	b.WriteString("<p><a href=\"/aws-config\">AWS config</a> | <a href=\"/command-generator\">Command generator</a></p>")
	b.WriteString("<form id=\"tunnel-form\" method=\"post\">")
	b.WriteString("<div class=\"row\"><div><label>Name</label><input name=\"name\" placeholder=\"myapp\" /></div><div><label>Profile</label><input name=\"profile\" placeholder=\"frontend\" /></div><div><label>Region</label><input name=\"region\" placeholder=\"us-east-1\" list=\"region-options\" /></div></div>")
	b.WriteString("<div class=\"row\" style=\"margin-top:8px\"><div><label>Stack</label><input name=\"stack\" placeholder=\"frontend\" /></div><div><label>Stage</label><input name=\"stage\" placeholder=\"CODE\" list=\"stage-options\" /></div><div><label>App</label><input name=\"app\" placeholder=\"frontend\" /></div></div>")
	b.WriteString("<div class=\"row\" style=\"margin-top:8px\"><div><label>ALB</label><input name=\"alb\" placeholder=\"myapp-alb\" /></div></div>")
	b.WriteString("<div class=\"checkbox-row\" style=\"margin-top:8px\"><label><input type=\"checkbox\" name=\"https\" checked /> Use HTTPS upstream</label><label><input type=\"checkbox\" name=\"verifySSL\" checked /> Respect upstream SSL certificate</label></div>")
	b.WriteString("<datalist id=\"stage-options\"><option value=\"CODE\"></option><option value=\"PROD\"></option><option value=\"INFRA\"></option></datalist>")
	b.WriteString("<datalist id=\"region-options\"><option value=\"us-east-1\"></option><option value=\"eu-west-1\"></option><option value=\"us-east-2\"></option></datalist>")
	b.WriteString("<div><button type=\"submit\">Create tunnel</button></div>")
	b.WriteString("</form>")
	b.WriteString("<h2>Existing tunnels</h2>")
	b.WriteString("<table><thead><tr><th>ID</th><th>Name</th><th>Profile</th><th>Region</th><th>Local</th><th>Status</th><th>Output</th><th>Actions</th></tr></thead><tbody>")
	if len(tunnels) == 0 {
		b.WriteString("<tr><td colspan=\"8\">No tunnels configured yet.</td></tr>")
	} else {
		for _, tunnel := range tunnels {
			statusClass := "status-pending"
			switch tunnel.Status {
			case "running":
				statusClass = "status-ok"
			case "failed":
				statusClass = "status-failed"
			}
			b.WriteString("<tr>")
			b.WriteString("<td>" + html.EscapeString(tunnel.ID) + "</td>")
			b.WriteString("<td>" + html.EscapeString(tunnel.Alias) + "</td>")
			b.WriteString("<td>" + html.EscapeString(tunnel.Profile) + "</td>")
			b.WriteString("<td>" + html.EscapeString(tunnel.Region) + "</td>")
			b.WriteString("<td>" + html.EscapeString(tunnel.LocalEndpoint) + "</td>")
			b.WriteString("<td class=\"" + statusClass + "\">" + html.EscapeString(tunnel.Status) + "</td>")
			b.WriteString("<td>" + html.EscapeString(tunnel.LastOutput) + "</td>")
			b.WriteString("<td><button data-delete=\"" + html.EscapeString(tunnel.ID) + "\">Delete</button>")
			b.WriteString(" <button data-stop=\"" + html.EscapeString(tunnel.ID) + "\">Stop</button></td>")
			b.WriteString("</tr>")
		}
	}
	b.WriteString("</tbody></table>")
	b.WriteString("<script>")
	b.WriteString("document.getElementById('tunnel-form').addEventListener('submit', async function(e){e.preventDefault();const form=new FormData(e.target);const payload={name:(form.get('name')||'').toString(),profile:(form.get('profile')||'').toString(),region:(form.get('region')||'').toString(),stack:(form.get('stack')||'').toString(),stage:(form.get('stage')||'').toString(),app:(form.get('app')||'').toString(),alb:(form.get('alb')||'').toString(),upstreamProtocol:(form.get('https') === 'on' ? 'https' : 'http'),verifySSL:(form.get('verifySSL') === 'on')};const res=await fetch('/api/tunnels',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});if(!res.ok){alert((await res.text())||'failed');return;}location.reload();});")
	b.WriteString("document.querySelectorAll('[data-delete]').forEach(btn=>btn.addEventListener('click', async function(){const id=this.getAttribute('data-delete');const res=await fetch('/api/tunnels/'+encodeURIComponent(id),{method:'DELETE'});if(!res.ok){alert((await res.text())||'failed');return;}location.reload();}));")
	b.WriteString("document.querySelectorAll('[data-stop]').forEach(btn=>btn.addEventListener('click', async function(){const id=this.getAttribute('data-stop');const res=await fetch('/api/tunnels/stop/'+encodeURIComponent(id),{method:'POST'});if(!res.ok){alert((await res.text())||'failed');return;}location.reload();}));")
	b.WriteString("</script>")
	b.WriteString("</body></html>")
	return b.String()
}

func renderTunnelSummary(tunnels []TunnelEntry) string {
	if len(tunnels) == 0 {
		return "No SSM tunnels configured."
	}
	var lines []string
	for _, tunnel := range tunnels {
		lines = append(lines, fmt.Sprintf("%s: %s %s (%s)", tunnel.Alias, tunnel.LocalEndpoint, tunnel.Status, tunnel.Region))
	}
	return strings.Join(lines, "<br>") + "<br><br><a href=\"/tunnels\">Manage tunnels</a>"
}

func tunnelTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return "n/a"
	}
	return ts.Format(time.RFC3339)
}

func renderCommandGeneratorPage() string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Command Generator</title>")
	b.WriteString("<style>body{font-family:system-ui,sans-serif;margin:24px}input,select,button{font:inherit;padding:8px 10px;margin:4px 0}label{display:block;font-weight:600;margin-top:10px}.row{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px}.muted{color:#666}.command-box{background:#f5f5f5;border:1px solid #ddd;padding:12px;border-radius:8px;white-space:pre-wrap;font-family:ui-monospace,monospace}.hidden{display:none}</style>")
	b.WriteString("</head><body>")
	b.WriteString(renderPageHeader("Command Generator"))
	b.WriteString("<p class=\"muted\">Generate copy-ready Docker exec commands for AWS SSM sessions.</p>")
	b.WriteString("<p><a href=\"/\">Home</a> | <a href=\"/aws-config\">AWS config</a> | <a href=\"/tunnels\">Tunnels</a></p>")
	b.WriteString("<form id=\"command-form\">")
	b.WriteString("<div class=\"row\"><div><label>Container</label><input name=\"container\" value=\"devcontainer-nginx\" /></div>")
	b.WriteString("<div><label>Run as</label><select name=\"user\"><option value=\"root\" selected>root</option><option value=\"current\">current user</option></select></div>")
	b.WriteString("<div><label>Command</label><select name=\"command\"><option value=\"shell\" selected>shell</option><option value=\"host-tunnel\">host-tunnel</option></select></div>")
	b.WriteString("<div><label>Profile</label><input name=\"profile\" value=\"\" placeholder=\"frontend\" /></div>")
	b.WriteString("<div><label>Stack</label><input name=\"stack\" value=\"\" placeholder=\"frontend\" /></div>")
	b.WriteString("<div><label>Stage</label><input name=\"stage\" value=\"\" placeholder=\"INFRA\" /></div>")
	b.WriteString("<div><label>App</label><input name=\"app\" value=\"\" placeholder=\"socks-proxy\" /></div>")
	b.WriteString("<div><label>Region</label><input name=\"region\" value=\"\" placeholder=\"eu-west-1\" /></div>")
	b.WriteString("<div id=\"host-tunnel-fields\" class=\"hidden\"><label>ALB</label><input name=\"alb\" value=\"\" placeholder=\"my-app-alb\" /></div>")
	b.WriteString("<div id=\"host-tunnel-fields-2\" class=\"hidden\"><label>Local port</label><input name=\"localPort\" value=\"\" placeholder=\"40000\" /></div>")
	b.WriteString("<div id=\"host-tunnel-fields-3\" class=\"hidden\"><label>Name</label><input name=\"name\" value=\"\" placeholder=\"renderer\" /></div>")
	b.WriteString("</div>")
	b.WriteString("<div style=\"margin-top:12px\"><button type=\"button\" id=\"copy-button\">Copy command</button></div>")
	b.WriteString("</form>")
	b.WriteString("<h2>Command</h2>")
	b.WriteString("<pre id=\"generated-command\" class=\"command-box\"></pre>")
	b.WriteString("<script>")
	b.WriteString("const form = document.getElementById('command-form');")
	b.WriteString("const output = document.getElementById('generated-command');")
	b.WriteString("const copyButton = document.getElementById('copy-button');")
	b.WriteString("const tunnelFields = document.querySelectorAll('[id^=\"host-tunnel-fields\"]');")
	b.WriteString("function updateVisibility(){const command = form.elements.command.value; const show = command === 'host-tunnel'; tunnelFields.forEach(el => { el.classList.toggle('hidden', !show); });}")
	b.WriteString("function render(){")
	b.WriteString("const data = new FormData(form);")
	b.WriteString("const container = (data.get('container') || 'devcontainer-nginx').toString().trim() || 'devcontainer-nginx';")
	b.WriteString("const user = data.get('user') || 'root';")
	b.WriteString("const command = (data.get('command') || 'shell').toString().trim();")
	b.WriteString("const profile = (data.get('profile') || '').toString().trim();")
	b.WriteString("const stack = (data.get('stack') || '').toString().trim();")
	b.WriteString("const stage = (data.get('stage') || '').toString().trim();")
	b.WriteString("const app = (data.get('app') || '').toString().trim();")
	b.WriteString("const region = (data.get('region') || '').toString().trim();")
	b.WriteString("const alb = (data.get('alb') || '').toString().trim();")
	b.WriteString("const localPort = (data.get('localPort') || '').toString().trim();")
	b.WriteString("const name = (data.get('name') || '').toString().trim();")
	b.WriteString("let parts = ['docker', 'exec'];")
	b.WriteString("if (user === 'root') parts.push('-u', 'root');")
	b.WriteString("parts.push('-it', container, command);")
	b.WriteString("if (profile) parts.push('--profile', profile);")
	b.WriteString("if (command === 'host-tunnel') { if (stack) parts.push('--stack', stack); if (stage) parts.push('--stage', stage); if (app) parts.push('--app', app); if (region) parts.push('--region', region); if (alb) parts.push('--alb', alb); if (localPort) parts.push('--local-port', localPort); if (name) parts.push('--name', name); }")
	b.WriteString("else { if (stack) parts.push('--stack', stack); if (stage) parts.push('--stage', stage); if (app) parts.push('--app', app); if (region) parts.push('--region', region); }")
	b.WriteString("const cmd = parts.join(' '); output.textContent = cmd;")
	b.WriteString("}")
	b.WriteString("form.addEventListener('input', () => { updateVisibility(); render(); });")
	b.WriteString("form.addEventListener('change', () => { updateVisibility(); render(); });")
	b.WriteString("copyButton.addEventListener('click', async () => { try { await navigator.clipboard.writeText(output.textContent); copyButton.textContent = 'Copied'; setTimeout(() => copyButton.textContent = 'Copy command', 1200); } catch (e) { copyButton.textContent = 'Copy failed'; } });")
	b.WriteString("updateVisibility(); render();")
	b.WriteString("</script>")
	b.WriteString("</body></html>")
	return b.String()
}
