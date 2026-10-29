package main

import (
	"fmt"
	"html"
	"strconv"
	"strings"
)

func renderAWSConfigPage(cfg AWSConfig) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: AWS Config</title>")
	b.WriteString(renderStylesheetLink())
	b.WriteString(renderFaviconLink())
	b.WriteString("</head><body data-page=\"aws-config\">")
	b.WriteString(renderPageHeader("AWS Config"))
	b.WriteString("<p>Paste the exact shell command(s) to run in the current container user. This is the source of truth; profile and region are supplied inside the command itself.</p>")
	b.WriteString("<div class=\"form-actions\"><button type=\"button\" id=\"paste-and-save-button\">Paste and Save</button><button type=\"button\" id=\"quick-paste-and-shell-button\" data-action=\"quick-paste-and-shell\">Quick paste, save, shell</button></div>")
	b.WriteString("<form id=\"aws-form\" method=\"post\" action=\"/aws-config\">")
	b.WriteString("<label>Credential bootstrap commands</label><textarea name=\"bootstrapCommand\">" + html.EscapeString(cfg.BootstrapCommand) + "</textarea>")
	b.WriteString("<div class=\"form-actions\"><button type=\"submit\">Save</button></div>")
	b.WriteString("</form>")
	b.WriteString("<pre class=\"aws-output\">" + html.EscapeString(cfg.Output) + "</pre>")
	b.WriteString("<div class=\"aws-exit-status\">Exit code: " + html.EscapeString(fmt.Sprintf("%d", cfg.ExitCode)) + "</div>")
	b.WriteString(renderHelpSection("aws-config"))
	b.WriteString("<script src=\"/static/app.js\"></script>")
	b.WriteString("</body></html>")
	return b.String()
}

func renderTunnelPage(cfg AWSConfig, tunnels []TunnelEntry) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Tunnels</title>")
	b.WriteString(renderStylesheetLink())
	b.WriteString(renderFaviconLink())
	b.WriteString("</head><body data-page=\"tunnel-page\">")
	b.WriteString(renderPageHeader("Tunnels"))
	b.WriteString("<form id=\"tunnel-form\" method=\"post\">")
	b.WriteString("<div class=\"row\"><div><label>Name</label><input name=\"name\" placeholder=\"myapp\" /></div><div><label>Profile</label>" + renderProfileSelect("profile", cfg.Profile) + "</div><div><label>Region</label>" + renderRegionSelect("region", cfg.Region) + "</div></div>")
	b.WriteString("<div class=\"row form-row-spaced\"><div><label>Stack</label><input name=\"stack\" placeholder=\"frontend\" /></div><div><label>Stage</label><input name=\"stage\" placeholder=\"CODE\" list=\"stage-options\" /></div><div><label>App</label><input name=\"app\" placeholder=\"frontend\" /></div></div>")
	b.WriteString("<div class=\"row form-row-spaced\"><div><label>ALB</label><input name=\"alb\" placeholder=\"myapp-alb\" /></div></div>")
	b.WriteString("<div class=\"checkbox-row form-row-spaced\"><label><input type=\"checkbox\" name=\"https\" checked /> Use HTTPS upstream</label><label><input type=\"checkbox\" name=\"verifySSL\" checked /> Respect upstream SSL certificate</label></div>")
	b.WriteString("<datalist id=\"stage-options\"><option value=\"CODE\"></option><option value=\"PROD\"></option><option value=\"INFRA\"></option></datalist>")
	b.WriteString("<div><button type=\"submit\">Create tunnel</button></div>")
	b.WriteString("</form>")
	b.WriteString("<h2>Existing tunnels</h2>")
	b.WriteString("<div class=\"table-wrap\"><table><thead><tr><th style=\"width: 7%;\">ID</th><th style=\"width: 12%;\">Name</th><th style=\"width: 10%;\">Profile</th><th style=\"width: 10%;\">Region</th><th style=\"width: 14%;\">Local</th><th style=\"width: 8%;\">Status</th><th style=\"width: 24%;\">Output</th><th style=\"width: 15%;\">Actions</th></tr></thead><tbody>")
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
			case "stopped":
				statusClass = "status-stopped"
			case "starting", "pending", "stopping":
				statusClass = "status-pending"
			}

			b.WriteString("<tr>")
			b.WriteString("<td>" + html.EscapeString(tunnel.ID) + "</td>")
			b.WriteString("<td>" + html.EscapeString(tunnel.Alias) + "</td>")
			b.WriteString("<td>" + html.EscapeString(tunnel.Profile) + "</td>")
			b.WriteString("<td>" + html.EscapeString(tunnel.Region) + "</td>")
			b.WriteString("<td>" + html.EscapeString(tunnel.LocalEndpoint) + "</td>")
			b.WriteString("<td class=\"" + statusClass + "\">" + html.EscapeString(tunnel.Status) + "</td>")
			b.WriteString("<td class=\"table-output\">" + html.EscapeString(tunnel.LastOutput) + "</td>")
			b.WriteString("<td class=\"table-actions\">")
			if tunnel.Status == "running" {
				b.WriteString("<button data-stop=\"" + html.EscapeString(tunnel.ID) + "\">Stop</button>")
			} else {
				b.WriteString("<button data-start=\"" + html.EscapeString(tunnel.ID) + "\">Start</button>")
			}
			b.WriteString(" <button data-delete=\"" + html.EscapeString(tunnel.ID) + "\">Delete</button>")
			b.WriteString("</td>")
			b.WriteString("</tr>")
		}
	}
	b.WriteString("</tbody></table></div>")
	b.WriteString(renderHelpSection("tunnels"))
	b.WriteString("<script src=\"/static/app.js\"></script>")
	b.WriteString("</body></html>")
	return b.String()
}

func renderCommandGeneratorPage() string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Command Generator</title>")
	b.WriteString(renderStylesheetLink())
	b.WriteString(renderFaviconLink())
	b.WriteString("</head><body data-page=\"command-generator\">")
	b.WriteString(renderPageHeader("Command Generator"))
	b.WriteString("<p class=\"muted\">Generate copy-ready Docker exec commands for AWS SSM sessions.</p>")
	b.WriteString("<p><a href=\"/\">Home</a> | <a href=\"/aws-config\">AWS config</a> | <a href=\"/tunnels\">Tunnels</a></p>")
	b.WriteString("<form id=\"command-form\">")
	b.WriteString("<div class=\"row\"><div><label>Container</label><input name=\"container\" value=\"devcontainer-nginx\" /></div>")
	b.WriteString("<div><label>Run as</label><select name=\"user\"><option value=\"root\" selected>root</option><option value=\"current\">current user</option></select></div>")
	b.WriteString("<div><label>Command</label><select name=\"command\"><option value=\"shell\" selected>shell</option><option value=\"bash\">bash</option><option value=\"host-tunnel\">host-tunnel</option></select></div>")
	b.WriteString("<div><label>Profile</label>" + renderProfileSelect("profile", "") + "</div>")
	b.WriteString("<div><label>Region</label>" + renderRegionSelect("region", "eu-west-1") + "</div>")
	b.WriteString("</div>")
	b.WriteString("<div class=\"row command-secondary-row\"><div class=\"command-field\"><label>App</label><input name=\"app\" value=\"\" placeholder=\"socks-proxy\" data-command-secondary /></div>")
	b.WriteString("<div class=\"command-field\"><label>Stack</label><input name=\"stack\" value=\"\" placeholder=\"frontend\" data-command-secondary /></div>")
	b.WriteString("<div class=\"command-field\"><label>Stage</label><input name=\"stage\" value=\"\" placeholder=\"INFRA\" data-command-secondary /></div>")
	b.WriteString("</div>")
	b.WriteString("<div class=\"row\"><div id=\"host-tunnel-fields\" class=\"hidden\"><label>ALB</label><input name=\"alb\" value=\"\" placeholder=\"my-app-alb\" /></div>")
	b.WriteString("<div id=\"host-tunnel-fields-2\" class=\"hidden\"><label>Local port</label><input name=\"localPort\" value=\"\" placeholder=\"40000\" /></div>")
	b.WriteString("<div id=\"host-tunnel-fields-3\" class=\"hidden\"><label>Name</label><input name=\"name\" value=\"\" placeholder=\"renderer\" /></div>")
	b.WriteString("</div>")
	b.WriteString("<div class=\"command-actions\"><button type=\"button\" id=\"copy-button\">Copy command</button></div>")
	b.WriteString("</form>")
	b.WriteString("<h2>Command</h2>")
	b.WriteString("<pre id=\"generated-command\" class=\"command-box\"></pre>")
	b.WriteString(renderHelpSection("command-generator"))
	b.WriteString("<script src=\"/static/app.js\"></script>")
	b.WriteString("</body></html>")
	return b.String()
}

func renderRemoteShellPage(profile, region, stack, stage, app string) string {
	profile = html.EscapeString(profile)
	region = html.EscapeString(region)
	stack = html.EscapeString(stack)
	stage = html.EscapeString(stage)
	app = html.EscapeString(app)
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Remote Bastion Shell</title>")
	b.WriteString("<link rel=\"stylesheet\" href=\"https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css\" />")
	b.WriteString(renderStylesheetLink())
	b.WriteString(renderFaviconLink())
	b.WriteString("</head><body data-page=\"shell-terminal\">")
	b.WriteString(renderPageHeader("Remote Bastion Shell (" + profile + "/" + region + ")"))
	b.WriteString("<p class=\"muted\">Connected to the running EC2 instance matching app=\"" + app + "\" stack=\"" + stack + "\" stage=\"" + stage + "\".</p>")
	b.WriteString("<div id=\"terminal\" style=\"height: 520px; background: #000; border-radius: 8px; padding: 10px;\"></div>")
	b.WriteString(renderHelpSection("remote-shell"))
	b.WriteString("<script src=\"https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.js\"></script>")

	b.WriteString("<script>")
	b.WriteString("const protocol = location.protocol === 'https:' ? 'wss' : 'ws';")
	b.WriteString("const profile = " + strconv.Quote(profile) + ";")
	b.WriteString("const region = " + strconv.Quote(region) + ";")
	b.WriteString("const stack = " + strconv.Quote(stack) + ";")
	b.WriteString("const stage = " + strconv.Quote(stage) + ";")
	b.WriteString("const app = " + strconv.Quote(app) + ";")
	b.WriteString("const socket = new WebSocket(protocol + '://' + location.host + '/remote-shell/ws?profile=' + encodeURIComponent(profile) + '&region=' + encodeURIComponent(region) + '&stack=' + encodeURIComponent(stack) + '&stage=' + encodeURIComponent(stage) + '&app=' + encodeURIComponent(app));")
	b.WriteString("const term = new Terminal({ cursorBlink: true, theme: { background: '#000000' }, fontSize: 14, scrollback: 5000 });")
	b.WriteString("const terminalElement = document.getElementById('terminal');")
	b.WriteString("terminalElement.style.width = '100%';")
	b.WriteString("term.open(terminalElement);")
	b.WriteString("const resizeTerminal = () => { const width = terminalElement.clientWidth || 1200; const height = terminalElement.clientHeight || 520; const cols = Math.max(80, Math.floor(width / 8.5)); const rows = Math.max(24, Math.floor(height / 18)); term.resize(cols, rows); socket.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows })); };")
	b.WriteString("term.focus();")
	b.WriteString("term.onData((data) => socket.send(data));")
	b.WriteString("term.onResize((size) => socket.send(JSON.stringify({ type: 'resize', cols: size.cols, rows: size.rows })));")
	b.WriteString("socket.addEventListener('open', () => { term.write('\\r\\n'); resizeTerminal(); });")
	b.WriteString("window.addEventListener('resize', resizeTerminal);")
	b.WriteString("socket.addEventListener('message', (event) => { const data = event.data; if (typeof data === 'string') { term.write(data); } else { const reader = new FileReader(); reader.onload = function () { term.write(new Uint8Array(reader.result)); }; reader.readAsArrayBuffer(data); } });")
	b.WriteString("socket.addEventListener('close', () => { term.write('\\r\\nConnection closed.'); });")
	b.WriteString("socket.addEventListener('error', () => { term.write('\\r\\nConnection error.'); });")
	b.WriteString("</script>")
	b.WriteString("</body></html>")
	return b.String()
}

func renderRemoteShellLaunchPage() string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Open Remote Bastion Shell</title>")
	b.WriteString(renderStylesheetLink())
	b.WriteString(renderFaviconLink())
	b.WriteString("</head><body data-page=\"remote-shell-launch\">")
	b.WriteString(renderPageHeader("Open Remote Bastion Shell"))
	b.WriteString("<p class=\"muted\">Open an interactive shell on the bastion EC2 instance selected by app, stack, and stage.</p>")
	b.WriteString("<form id=\"remote-shell-launch-form\" action=\"/remote-shell\" method=\"get\" target=\"_blank\">")
	b.WriteString("<div class=\"row\"><div><label>Profile</label>" + renderProfileSelect("profile", "") + "</div>")
	b.WriteString("<div><label>Region</label>" + renderRegionSelect("region", "eu-west-1") + "</div></div>")
	b.WriteString("<div class=\"row\"><div><label>Stack</label><input name=\"stack\" placeholder=\"frontend\" /></div>")
	b.WriteString("<div><label>Stage</label><input name=\"stage\" placeholder=\"INFRA\" list=\"stage-options\" /></div>")
	b.WriteString("<div><label>App</label><input name=\"app\" placeholder=\"socks-proxy\" /></div></div>")
	b.WriteString("<datalist id=\"stage-options\"><option value=\"CODE\"></option><option value=\"PROD\"></option><option value=\"INFRA\"></option></datalist>")
	b.WriteString("<div class=\"command-actions\"><button type=\"submit\">Open bastion shell in new tab</button></div>")
	b.WriteString("</form>")
	b.WriteString(renderHelpSection("remote-shell-launch"))
	b.WriteString("<script src=\"/static/app.js\"></script>")
	b.WriteString("</body></html>")
	return b.String()
}

func renderShellPage(profile, region string) string {
	profile = html.EscapeString(profile)
	region = html.EscapeString(region)
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Local Container Shell</title>")
	b.WriteString("<link rel=\"stylesheet\" href=\"https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css\" />")
	b.WriteString(renderStylesheetLink())
	b.WriteString(renderFaviconLink())
	b.WriteString("</head><body data-page=\"shell-terminal\">")
	b.WriteString(renderPageHeader("Local Container Shell (" + profile + "/" + region + ")"))
	b.WriteString("<div id=\"terminal\" style=\"height: 520px; background: #000; border-radius: 8px; padding: 10px;\"></div>")
	b.WriteString(renderHelpSection("shell-timeout"))
	b.WriteString("<script src=\"https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.js\"></script>")

	b.WriteString("<script>")
	b.WriteString("const protocol = location.protocol === 'https:' ? 'wss' : 'ws';")
	b.WriteString("const profile = " + strconv.Quote(profile) + ";")
	b.WriteString("const region = " + strconv.Quote(region) + ";")
	b.WriteString("const socket = new WebSocket(protocol + '://' + location.host + '/shell/ws?profile=' + encodeURIComponent(profile) + '&region=' + encodeURIComponent(region));")
	b.WriteString("const term = new Terminal({ cursorBlink: true, theme: { background: '#000000' }, fontSize: 14, scrollback: 5000 });")
	b.WriteString("const terminalElement = document.getElementById('terminal');")
	b.WriteString("terminalElement.style.width = '100%';")
	b.WriteString("term.open(terminalElement);")
	b.WriteString("const resizeTerminal = () => { const width = terminalElement.clientWidth || 1200; const height = terminalElement.clientHeight || 520; const cols = Math.max(80, Math.floor(width / 8.5)); const rows = Math.max(24, Math.floor(height / 18)); term.resize(cols, rows); socket.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows })); };")
	b.WriteString("term.focus();")
	b.WriteString("term.onData((data) => socket.send(data));")
	b.WriteString("term.onResize((size) => socket.send(JSON.stringify({ type: 'resize', cols: size.cols, rows: size.rows })));")
	b.WriteString(`socket.addEventListener('open', () => { term.write('\r\n'); resizeTerminal(); });`)
	b.WriteString(`window.addEventListener('resize', resizeTerminal);`)
	b.WriteString(`socket.addEventListener('message', (event) => { const data = event.data; if (typeof data === 'string') { term.write(data); } else { const reader = new FileReader(); reader.onload = function () { term.write(new Uint8Array(reader.result)); }; reader.readAsArrayBuffer(data); } });`)
	b.WriteString(`socket.addEventListener('close', () => { term.write('\r\nConnection closed.'); });`)
	b.WriteString(`socket.addEventListener('error', () => { term.write('\r\nConnection error.'); });`)
	b.WriteString("</script>")
	b.WriteString("</body></html>")
	return b.String()
}

func renderProfileOptions(selected string, profiles []string) string {
	var b strings.Builder
	if len(profiles) == 0 {
		b.WriteString("<option value=\"\" selected>No valid AWS profiles found</option>")
		return b.String()
	}

	selectedFound := false
	for _, profile := range profiles {
		if profile == selected {
			selectedFound = true
			break
		}
	}
	if !selectedFound && selected != "" {
		profiles = append([]string{selected}, profiles...)
	}

	for _, profile := range profiles {
		if profile == "" {
			continue
		}
		b.WriteString("<option value=\"" + html.EscapeString(profile) + "\"")
		if profile == selected {
			b.WriteString(" selected")
		}
		b.WriteString(">" + html.EscapeString(profile) + "</option>")
	}
	return b.String()
}

func renderProfileSelect(name, value string) string {
	profiles := KnownGoodAWSProfiles()
	if len(profiles) == 0 {
		profiles = RefreshAWSProfiles()
	}
	var b strings.Builder
	b.WriteString("<select name=\"" + html.EscapeString(name) + "\">")
	if len(profiles) == 0 {
		b.WriteString("<option value=\"\" selected>No valid AWS profiles found</option>")
		b.WriteString("</select>")
		return b.String()
	}
	if value == "" {
		b.WriteString("<option value=\"\" selected>Select profile</option>")
	}
	b.WriteString(renderProfileOptions(value, profiles))
	b.WriteString("</select>")
	return b.String()
}

func renderRegionField(name, value string) string {
	selected := strings.TrimSpace(value)
	if selected == "" {
		selected = "eu-west-1"
	}
	options := []string{"eu-west-1", "us-east-1", "us-east-2", "eu-west-2", "us-west-2"}
	seen := map[string]bool{}
	datalistID := "region-options"
	if name != "" {
		datalistID = "region-options-" + strings.NewReplacer(" ", "-", "/", "-", ":", "-", "[", "-", "]", "-").Replace(name)
	}
	var b strings.Builder
	b.WriteString("<div class=\"region-control\">")
	b.WriteString("<input class=\"region-input\" type=\"text\" name=\"" + html.EscapeString(name) + "\" value=\"" + html.EscapeString(selected) + "\" list=\"" + html.EscapeString(datalistID) + "\" placeholder=\"eu-west-1\" autocomplete=\"off\" />")
	b.WriteString("<datalist id=\"" + html.EscapeString(datalistID) + "\">")
	for _, option := range options {
		if option == "" || seen[option] {
			continue
		}
		seen[option] = true
		b.WriteString("<option value=\"" + html.EscapeString(option) + "\"></option>")
	}
	if selected != "" && !seen[selected] {
		b.WriteString("<option value=\"" + html.EscapeString(selected) + "\"></option>")
	}
	b.WriteString("</datalist>")
	b.WriteString("</div>")
	return b.String()
}

func renderRegionOptions(selected string) string {
	options := []string{"eu-west-1", "us-east-1", "us-east-2", "eu-west-2", "us-west-2"}
	seen := map[string]bool{}
	var out strings.Builder
	if selected != "" && !seen[selected] {
		options = append([]string{selected}, options...)
	}
	for _, region := range options {
		if region == "" {
			continue
		}
		if seen[region] {
			continue
		}
		seen[region] = true
		out.WriteString("<option value=\"" + html.EscapeString(region) + "\"")
		if region == selected {
			out.WriteString(" selected")
		}
		out.WriteString(">" + html.EscapeString(region) + "</option>")
	}
	return out.String()
}

func renderRegionSelect(name, value string) string {
	return renderRegionField(name, value)
}

func renderShellLaunchPage() string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Devcontainer Nginx: Open Shell</title>")
	b.WriteString(renderStylesheetLink())
	b.WriteString(renderFaviconLink())
	b.WriteString("</head><body data-page=\"shell-launch\">")
	b.WriteString(renderPageHeader("Open Shell"))
	b.WriteString("<p class=\"muted\">Open a shell inside the devcontainer-nginx container with AWS environment variables pre-set.</p>")
	b.WriteString("<form id=\"shell-launch-form\" action=\"/shell\" method=\"get\" target=\"_blank\">")
	b.WriteString("<div class=\"row\"><div><label>Profile</label>" + renderProfileSelect("profile", "") + "</div>")
	b.WriteString("<div><label>Region</label>" + renderRegionSelect("region", "eu-west-1") + "</div></div>")
	b.WriteString("<div class=\"command-actions\"><button type=\"submit\">Open shell in new tab</button></div>")
	b.WriteString("</form>")
	b.WriteString(renderHelpSection("shell-launch"))
	b.WriteString("<script src=\"/static/app.js\"></script>")
	b.WriteString("</body></html>")
	return b.String()
}
