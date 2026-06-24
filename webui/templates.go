package webui

import (
	"encoding/hex"
	"html/template"
)

// shortHex renders the first 4 bytes of a root as 0x-prefixed hex (8 chars).
func shortHex(b []byte) string {
	if len(b) == 0 {
		return "—"
	}
	n := 4
	if len(b) < n {
		n = len(b)
	}
	return "0x" + hex.EncodeToString(b[:n])
}

// fullHex renders a byte slice as a 0x-prefixed hex string.
func fullHex(b []byte) string {
	if len(b) == 0 {
		return "—"
	}
	return "0x" + hex.EncodeToString(b)
}

var funcMap = template.FuncMap{
	"shortHex": shortHex,
	"fullHex":  fullHex,
	"statusLabel": func(s uint8) string {
		switch s {
		case 0:
			return "missed"
		case 1:
			return "proposed"
		case 2:
			return "orphaned"
		default:
			return "unknown"
		}
	},
	"statusClass": func(s uint8) string {
		switch s {
		case 1:
			return "ok"
		case 2:
			return "warn"
		default:
			return "muted"
		}
	},
}

const layout = `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · lean-dora</title>
<style>
  :root { color-scheme: dark; }
  body { background:#16161e; color:#d8d8e0; font-family:-apple-system,Segoe UI,Roboto,monospace; margin:0; }
  header { background:#1f1f2e; padding:12px 24px; border-bottom:1px solid #33334a; display:flex; align-items:center; gap:20px; }
  header h1 { font-size:18px; margin:0; color:#8ab4f8; }
  nav a { color:#b8b8d0; text-decoration:none; margin-right:16px; font-size:14px; }
  nav a:hover { color:#fff; }
  main { padding:24px; max-width:1100px; margin:0 auto; }
  .cards { display:flex; gap:16px; flex-wrap:wrap; margin-bottom:24px; }
  .card { background:#1f1f2e; border:1px solid #33334a; border-radius:8px; padding:16px 20px; min-width:160px; }
  .card .k { color:#8888a0; font-size:12px; text-transform:uppercase; }
  .card .v { color:#fff; font-size:26px; font-weight:600; margin-top:4px; }
  table { width:100%; border-collapse:collapse; font-size:14px; }
  th,td { text-align:left; padding:8px 10px; border-bottom:1px solid #2a2a3c; }
  th { color:#8888a0; font-weight:500; text-transform:uppercase; font-size:11px; }
  td a { color:#8ab4f8; text-decoration:none; }
  .badge { padding:2px 8px; border-radius:10px; font-size:12px; }
  .badge.ok { background:#1b3a1b; color:#7ee787; }
  .badge.warn { background:#3a2a1b; color:#f0a868; }
  .badge.muted { background:#2a2a3c; color:#8888a0; }
  .mono { font-family:monospace; }
  .pager { margin-top:16px; }
  .pager a { color:#8ab4f8; margin-right:12px; text-decoration:none; }
  iframe { width:100%; height:80vh; border:1px solid #33334a; border-radius:8px; background:#1a1a2e; }
</style>
</head><body>
<header>
  <h1>lean-dora</h1>
  <nav>
    <a href="/">Dashboard</a>
    <a href="/slots">Slots</a>
    <a href="/finality">Finality</a>
    <a href="/forkchoice">Fork Choice</a>
    <a href="/validators">Validators</a>
  </nav>
</header>
<main>{{template "body" .}}</main>
</body></html>`

const dashboardBody = `{{define "body"}}
<div class="cards">
  <div class="card"><div class="k">Head slot</div><div class="v">{{.HeadSlot}}</div></div>
  <div class="card"><div class="k">Justified slot</div><div class="v">{{.JustifiedSlot}}</div></div>
  <div class="card"><div class="k">Finalized slot</div><div class="v">{{.FinalizedSlot}}</div></div>
  <div class="card"><div class="k">Validators</div><div class="v">{{.ValidatorCount}}</div></div>
  <div class="card"><div class="k">Slot time</div><div class="v">{{.SlotSeconds}}s</div></div>
</div>
<h2>Recent slots</h2>
<table>
  <tr><th>Slot</th><th>Status</th><th>Proposer</th><th>Root</th><th>Attestations</th><th>Final</th></tr>
  {{range .Slots}}
  <tr>
    <td><a href="/slot/{{.Slot}}">{{.Slot}}</a></td>
    <td><span class="badge {{statusClass .Status}}">{{statusLabel .Status}}</span></td>
    <td>{{.Proposer}}</td>
    <td class="mono"><a href="/slot/{{fullHex .Root}}">{{shortHex .Root}}</a></td>
    <td>{{.AttestationCount}}</td>
    <td>{{if .Finalized}}✓{{else}}—{{end}}</td>
  </tr>
  {{end}}
</table>
{{end}}`

const slotsBody = `{{define "body"}}
<h2>Slots {{.MinSlot}}–{{.MaxSlot}}</h2>
<table>
  <tr><th>Slot</th><th>Status</th><th>Proposer</th><th>Root</th><th>Parent</th><th>Attestations</th><th>Just</th><th>Final</th></tr>
  {{range .Slots}}
  <tr>
    <td><a href="/slot/{{.Slot}}">{{.Slot}}</a></td>
    <td><span class="badge {{statusClass .Status}}">{{statusLabel .Status}}</span></td>
    <td>{{.Proposer}}</td>
    <td class="mono"><a href="/slot/{{fullHex .Root}}">{{shortHex .Root}}</a></td>
    <td class="mono">{{shortHex .ParentRoot}}</td>
    <td>{{.AttestationCount}}</td>
    <td>{{if .Justified}}✓{{else}}—{{end}}</td>
    <td>{{if .Finalized}}✓{{else}}—{{end}}</td>
  </tr>
  {{end}}
</table>
<div class="pager">
  {{if .HasNewer}}<a href="/slots?max={{.NewerMax}}">← newer</a>{{end}}
  {{if .HasOlder}}<a href="/slots?max={{.OlderMax}}">older →</a>{{end}}
</div>
{{end}}`

const slotDetailBody = `{{define "body"}}
{{if .Slot}}
<h2>Slot {{.Slot.Slot}}</h2>
<table>
  <tr><th>Status</th><td><span class="badge {{statusClass .Slot.Status}}">{{statusLabel .Slot.Status}}</span></td></tr>
  <tr><th>Proposer</th><td>{{.Slot.Proposer}}</td></tr>
  <tr><th>Block root</th><td class="mono">{{fullHex .Slot.Root}}</td></tr>
  <tr><th>Parent root</th><td class="mono">{{fullHex .Slot.ParentRoot}}</td></tr>
  <tr><th>State root</th><td class="mono">{{fullHex .Slot.StateRoot}}</td></tr>
  <tr><th>Attestations</th><td>{{.Slot.AttestationCount}}</td></tr>
  <tr><th>Votes recorded</th><td>{{.VoteCount}}</td></tr>
  <tr><th>Justified</th><td>{{if .Slot.Justified}}✓{{else}}—{{end}}</td></tr>
  <tr><th>Finalized</th><td>{{if .Slot.Finalized}}✓{{else}}—{{end}}</td></tr>
  <tr><th>Block size</th><td>{{.Slot.BlockSize}} bytes</td></tr>
  <tr><th>Recv delay</th><td>{{.Slot.RecvDelay}} ms</td></tr>
</table>
{{if .Votes}}
<h3>Votes</h3>
<table>
  <tr><th>Validator</th><th>Source slot</th><th>Target slot</th><th>Head</th></tr>
  {{range .Votes}}
  <tr><td>{{.ValidatorIndex}}</td><td>{{.SourceSlot}}</td><td>{{.TargetSlot}}</td><td class="mono">{{shortHex .HeadRoot}}</td></tr>
  {{end}}
</table>
{{end}}
{{else}}
<h2>Slot not found</h2>
<p>No block recorded at this id.</p>
{{end}}
{{end}}`

const finalityBody = `{{define "body"}}
<div class="cards">
  <div class="card"><div class="k">Justified slot</div><div class="v">{{.JustifiedSlot}}</div></div>
  <div class="card"><div class="k">Finalized slot</div><div class="v">{{.FinalizedSlot}}</div></div>
</div>
<h2>Finalized checkpoints</h2>
<table>
  <tr><th>Slot</th><th>Root</th></tr>
  {{range .Finalized}}<tr><td>{{.Slot}}</td><td class="mono">{{fullHex .Root}}</td></tr>{{end}}
</table>
<h2>Justified checkpoints</h2>
<table>
  <tr><th>Slot</th><th>Root</th></tr>
  {{range .Justified}}<tr><td>{{.Slot}}</td><td class="mono">{{fullHex .Root}}</td></tr>{{end}}
</table>
{{end}}`

const validatorsBody = `{{define "body"}}
<div class="cards">
  <div class="card"><div class="k">Validators</div><div class="v">{{.ValidatorCount}}</div></div>
</div>
<h2>Validators</h2>
{{if .Validators}}
<table>
  <tr><th>Index</th><th>Attestation pubkey</th><th>Proposal pubkey</th></tr>
  {{range .Validators}}
  <tr><td>{{.Index}}</td><td class="mono">{{shortHex .AttestationPubkey}}</td><td class="mono">{{shortHex .ProposalPubkey}}</td></tr>
  {{end}}
</table>
{{else}}
<p>Validator registry not indexed (ethlambda exposes the count via /fork_choice;
the full registry endpoint is a follow-up). Count: {{.ValidatorCount}}.</p>
{{end}}
{{end}}`

const forkChoiceBody = `{{define "body"}}
<h2>Fork choice</h2>
<p>Live tree proxied from the node's <code>/lean/v0/fork_choice/ui</code>.</p>
<iframe src="{{.NodeUIURL}}"></iframe>
{{end}}`

func mustTemplate(name, body string) *template.Template {
	return template.Must(template.New(name).Funcs(funcMap).Parse(layout + body))
}

var (
	tmplDashboard  = mustTemplate("dashboard", dashboardBody)
	tmplSlots      = mustTemplate("slots", slotsBody)
	tmplSlotDetail = mustTemplate("slotDetail", slotDetailBody)
	tmplFinality   = mustTemplate("finality", finalityBody)
	tmplValidators = mustTemplate("validators", validatorsBody)
	tmplForkChoice = mustTemplate("forkchoice", forkChoiceBody)
)
