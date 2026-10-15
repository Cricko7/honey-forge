import { api, req, BASE } from "./api.js";
import { toast } from "./toast.js";

const NODE_W = 200;
const HEAD_H = 42;
const PORT_START = HEAD_H + 18;
const PORT_GAP = 22;

const S = {
  registry: null,
  typesById: {},
  catsById: {},
  chains: [],
  chain: null,                 // текущая загруженная цепочка
  vp: { x: 120, y: 80, zoom: 1 },
  selection: null,             // node id
  dirty: false,
  snap: false,                 // привязка к сетке
  runtime: { deployed: false, status: {}, es: null, hits: {} },  // состояние и реальные события
  realOnline: false,           // доступен ли бэкенд дашборда
  realTypes: {},               // type_id -> мета каталога
  realTraps: [],               // список реальных ловушек из дашборда
};
const GRID = 26;

const ICONS = {
  globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.6 2.6 2.6 15.4 0 18M12 3c-2.6 2.6-2.6 15.4 0 18"/>',
  network: '<rect x="9" y="2" width="6" height="6" rx="1"/><rect x="2" y="16" width="6" height="6" rx="1"/><rect x="16" y="16" width="6" height="6" rx="1"/><path d="M12 8v4M12 12H5v4M12 12h7v4"/>',
  terminal: '<path d="m4 7 5 5-5 5M12 19h8"/>',
  monitor: '<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>',
  folder: '<path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7L11 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2Z"/>',
  layout: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/>',
  database: '<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/>',
  laptop: '<path d="M4 5h16v11H4zM2 20h20"/>',
  shield: '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10Z"/>',
  key: '<circle cx="7.5" cy="15.5" r="3.5"/><path d="m10 13 9-9M16 7l2 2M13 10l2 2"/>',
  file: '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z"/><path d="M14 2v6h6"/>',
  "git-branch": '<path d="M6 3v12"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  bell: '<path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9M10 21a2 2 0 0 0 4 0"/>',
  share: '<circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><path d="m8.6 13.5 6.8 4M15.4 6.5 8.6 10.5"/>',
};
function nodeIcon(name) {
  const p = ICONS[name] || ICONS.layout;
  return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${p}</svg>`;
}

const $ = (id) => document.getElementById(id);
const wrap = $("canvasWrap"), nodeLayer = $("nodeLayer"), edgeGroup = $("edgeGroup");
const edgeLayer = $("edgeLayer"), canvasBg = $("canvasBg"), tempEdge = $("tempEdge");

function clientToWorld(cx, cy) {
  const r = wrap.getBoundingClientRect();
  return { x: (cx - r.left - S.vp.x) / S.vp.zoom, y: (cy - r.top - S.vp.y) / S.vp.zoom };
}
function portGroups(type) {
  const spec = S.typesById[type];
  if (!spec) return { ins: [], outs: [] };
  const ins = spec.ports.filter(p => p.kind === "in");
  const outs = spec.ports.filter(p => p.kind === "out");
  return { ins, outs };
}
function portPos(node, portId) {
  const { ins, outs } = portGroups(node.type);
  let i = ins.findIndex(p => p.id === portId);
  if (i >= 0) return { x: node.position.x, y: node.position.y + PORT_START + i * PORT_GAP };
  i = outs.findIndex(p => p.id === portId);
  return { x: node.position.x + NODE_W, y: node.position.y + PORT_START + i * PORT_GAP };
}
function portFlavor(type, portId) {
  const spec = S.typesById[type];
  const p = spec?.ports.find(x => x.id === portId);
  return p ? p.flavor : "flow";
}
function nodeHeight(type) {
  const { ins, outs } = portGroups(type);
  return PORT_START + Math.max(ins.length, outs.length, 1) * PORT_GAP + 6;
}

function applyViewport() {
  const { x, y, zoom } = S.vp;
  nodeLayer.style.transform = `translate(${x}px, ${y}px) scale(${zoom})`;
  edgeGroup.setAttribute("transform", `translate(${x},${y}) scale(${zoom})`);
  canvasBg.style.backgroundSize = `${26 * zoom}px ${26 * zoom}px`;
  canvasBg.style.backgroundPosition = `${x}px ${y}px`;
  $("zoomLabel").textContent = Math.round(zoom * 100) + "%";
  drawMinimap();
}

function renderNodes() {
  nodeLayer.innerHTML = "";
  if (!S.chain) return;
  for (const node of S.chain.nodes) {
    if (!S.typesById[node.type]) continue;  // неизвестный тип (фейковый остаток) — пропускаем
    nodeLayer.appendChild(buildNode(node));
  }
}

function buildNode(node) {
  const spec = S.typesById[node.type];
  const el = document.createElement("div");
  el.className = "node" + (S.selection === node.id ? " selected" : "");
  el.dataset.id = node.id;
  el.style.left = node.position.x + "px";
  el.style.top = node.position.y + "px";
  el.style.minHeight = nodeHeight(node.type) + "px";
  const accent = S.catsById[spec.category]?.accent || "#888";

  const isReal = node.type.startsWith("real/");
  const chips = isReal ? realChips(node) : summaryChips(node, spec);
  el.innerHTML = `
    <div class="node-head">
      <span class="node-ico" style="color:${accent};background:${accent}1f">${nodeIcon(spec.icon)}</span>
      <div style="flex:1;min-width:0">
        <div class="node-title">${esc(node.label || spec.name)}</div>
        <div class="node-sub">${esc(spec.name)}</div>
      </div>
      <span class="risk-dot risk-${spec.risk}" title="риск: ${spec.risk}"></span>
    </div>
    <div class="node-body">${chips}</div>`;

  if (node.type.startsWith("trap/") || node.type.startsWith("endpoint/") || node.type === 'custom/listener' || node.type === 'http/input' || isReal) {
    const st = document.createElement("div");
    const running = isReal
      ? (node.config?._real?.runtime === "running")
      : S.runtime.status[node.id] === "running";
    st.className = "node-status" + (running ? " running" : "");
    st.title = isReal ? ("реальная ловушка · " + (node.config?._real?.runtime || "?"))
                      : (running ? "ловушка развёрнута" : "остановлена");
    el.appendChild(st);
  }

  // счётчик попаданий атак (боевой режим) — восстанавливаем из накопленного
  const hits = S.runtime.hits[node.id] || 0;
  const badge = document.createElement("span");
  badge.className = "node-hits" + (hits ? "" : " hidden");
  badge.dataset.hits = hits;
  badge.textContent = hits;
  badge.title = `Событий узла за текущий просмотр: ${hits}. Начало и завершение блока считаются отдельно.`;
  el.appendChild(badge);

  const { ins, outs } = portGroups(node.type);
  ins.forEach((p, i) => el.appendChild(buildPort(node, p, i, "in")));
  outs.forEach((p, i) => el.appendChild(buildPort(node, p, i, "out")));
  return el;
}

function summaryChips(node, spec) {
  const out = [];
  if (node.config.port) out.push(`порт ${node.config.port}`);
  for (const f of spec.config) {
    if (f.key === "port") continue;
    const v = node.config[f.key];
    if (v === undefined || v === "" || v === false || v === null) continue;
    if (f.type === "secret") continue;
    if (Array.isArray(v)) { if (v.length) out.push(`${v.length} × ${f.label.toLowerCase()}`); }
    else if (f.type === "toggle") out.push(f.label.toLowerCase());
    else if (out.length < 3) out.push(String(v));
    if (out.length >= 3) break;
  }
  return out.slice(0, 3).map(c => `<span class="node-chip">${esc(c)}</span>`).join("") || "";
}

function realChips(node) {
  const r = (node.config && node.config._real) || {};
  return [r.connectivity, r.runtime].filter(Boolean)
    .map(c => `<span class="node-chip">${esc(c)}</span>`).join("");
}

function buildPort(node, p, idx, kind) {
  const d = document.createElement("div");
  d.className = `port flavor-${p.flavor}`;
  d.dataset.node = node.id; d.dataset.port = p.id; d.dataset.kind = kind;
  const y = PORT_START + idx * PORT_GAP;
  d.style.top = (y - 6) + "px";
  d.style[kind === "in" ? "left" : "right"] = "-7px";
  const lab = document.createElement("span");
  lab.className = "port-label";
  lab.textContent = p.label;
  lab.style.top = "50%";
  if (kind === "in") lab.style.left = "18px"; else { lab.style.right = "18px"; }
  d.appendChild(lab);
  return d;
}

function bez(a, b) {
  const dx = Math.max(40, Math.abs(b.x - a.x) * 0.5);
  return `M ${a.x} ${a.y} C ${a.x + dx} ${a.y}, ${b.x - dx} ${b.y}, ${b.x} ${b.y}`;
}

function renderEdges(activeSet) {
  edgeGroup.innerHTML = "";
  if (!S.chain) return;
  const byId = Object.fromEntries(S.chain.nodes.map(n => [n.id, n]));
  for (const e of S.chain.edges) {
    const sn = byId[e.source], tn = byId[e.target];
    if (!sn || !tn) continue;
    if (!S.typesById[sn.type] || !S.typesById[tn.type]) continue;  // неизвестные типы
    const a = portPos(sn, e.source_port), b = portPos(tn, e.target_port);
    const flavor = portFlavor(sn.type, e.source_port);
    const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
    const active = activeSet && activeSet.has(e.id);
    path.setAttribute("class", `edge flavor-${flavor}` + (active ? " active" : ""));
    path.setAttribute("d", bez(a, b));
    const hit = document.createElementNS("http://www.w3.org/2000/svg", "path");
    hit.setAttribute("class", "edge-hit");
    hit.setAttribute("d", bez(a, b));
    hit.addEventListener("click", (ev) => { ev.stopPropagation(); if (confirm("Удалить связь?")) deleteEdge(e.id); });
    edgeGroup.appendChild(path);
    edgeGroup.appendChild(hit);
    if (e.label) {
      const mx = (a.x + b.x) / 2, my = (a.y + b.y) / 2;
      const tx = document.createElementNS("http://www.w3.org/2000/svg", "text");
      tx.setAttribute("class", "edge-label");
      tx.setAttribute("x", mx); tx.setAttribute("y", my - 4);
      tx.setAttribute("text-anchor", "middle");
      tx.textContent = e.label;
      const bg = document.createElementNS("http://www.w3.org/2000/svg", "rect");
      const w = e.label.length * 6 + 8;
      bg.setAttribute("class", "edge-label-bg");
      bg.setAttribute("x", mx - w / 2); bg.setAttribute("y", my - 15);
      bg.setAttribute("width", w); bg.setAttribute("height", 15); bg.setAttribute("rx", 4);
      edgeGroup.appendChild(bg); edgeGroup.appendChild(tx);
    }
  }
}

function renderAll() { renderEdges(); renderNodes(); applyViewport(); }

function drawMinimap() {
  const cv = $("minimap");
  if (!cv) return;
  const ctx = cv.getContext("2d");
  ctx.clearRect(0, 0, cv.width, cv.height);
  if (!S.chain || !S.chain.nodes.length) return;
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const n of S.chain.nodes) {
    minX = Math.min(minX, n.position.x); minY = Math.min(minY, n.position.y);
    maxX = Math.max(maxX, n.position.x + NODE_W); maxY = Math.max(maxY, n.position.y + nodeHeight(n.type));
  }
  const pad = 40; minX -= pad; minY -= pad; maxX += pad; maxY += pad;
  const sc = Math.min(cv.width / (maxX - minX), cv.height / (maxY - minY));
  for (const n of S.chain.nodes) {
    const spec = S.typesById[n.type];
    ctx.fillStyle = S.catsById[spec.category]?.accent || "#888";
    ctx.fillRect((n.position.x - minX) * sc, (n.position.y - minY) * sc, NODE_W * sc, nodeHeight(n.type) * sc);
  }
  const r = wrap.getBoundingClientRect();
  const vx = (-S.vp.x / S.vp.zoom - minX) * sc, vy = (-S.vp.y / S.vp.zoom - minY) * sc;
  const vw = (r.width / S.vp.zoom) * sc, vh = (r.height / S.vp.zoom) * sc;
  ctx.strokeStyle = "rgba(110,86,207,.9)"; ctx.lineWidth = 1.5;
  ctx.strokeRect(vx, vy, vw, vh);
}

wrap.addEventListener("wheel", (e) => {
  e.preventDefault();
  const r = wrap.getBoundingClientRect();
  const sx = e.clientX - r.left, sy = e.clientY - r.top;
  const factor = e.deltaY < 0 ? 1.12 : 1 / 1.12;
  const nz = Math.min(2.5, Math.max(0.2, S.vp.zoom * factor));
  // зум к курсору
  S.vp.x = sx - (sx - S.vp.x) * (nz / S.vp.zoom);
  S.vp.y = sy - (sy - S.vp.y) * (nz / S.vp.zoom);
  S.vp.zoom = nz;
  applyViewport();
  markDirty();
}, { passive: false });

let pan = null;
wrap.addEventListener("mousedown", (e) => {
  if (e.target.closest(".node") || e.target.closest(".port") || e.target.closest(".canvas-toolbar")
      || e.target.closest(".minimap") || e.target.closest(".edge-hit")) return;
  pan = { x: e.clientX, y: e.clientY, vx: S.vp.x, vy: S.vp.y };
  wrap.classList.add("panning");
  select(null);
});
window.addEventListener("mousemove", (e) => {
  if (pan) {
    S.vp.x = pan.vx + (e.clientX - pan.x);
    S.vp.y = pan.vy + (e.clientY - pan.y);
    applyViewport();
  }
});
window.addEventListener("mouseup", () => { if (pan) { pan = null; wrap.classList.remove("panning"); markDirty(); } });

let drag = null, linking = null;
nodeLayer.addEventListener("mousedown", (e) => {
  const portEl = e.target.closest(".port");
  if (portEl) {
    e.stopPropagation();
    if (portEl.dataset.kind === "in") {
      // открепить связь, висящую на этом входе, и взять её «в руку»
      const ex = S.chain.edges.find(x =>
        x.target === portEl.dataset.node && x.target_port === portEl.dataset.port);
      if (!ex) return;
      S.chain.edges = S.chain.edges.filter(x => x.id !== ex.id);
      renderEdges();
      linking = { node: ex.source, port: ex.source_port };
      tempEdge.classList.remove("hidden");
      autosave();
      return;
    }
    // из выхода — тянем новую связь
    linking = { node: portEl.dataset.node, port: portEl.dataset.port };
    tempEdge.classList.remove("hidden");
    return;
  }
  const nodeEl = e.target.closest(".node");
  if (!nodeEl) return;
  const id = nodeEl.dataset.id;
  select(id);
  const node = S.chain.nodes.find(n => n.id === id);
  const w = clientToWorld(e.clientX, e.clientY);
  drag = { id, dx: w.x - node.position.x, dy: w.y - node.position.y, el: nodeEl };
  nodeEl.classList.add("dragging");
});
window.addEventListener("mousemove", (e) => {
  if (drag) {
    const w = clientToWorld(e.clientX, e.clientY);
    const node = S.chain.nodes.find(n => n.id === drag.id);
    let nx = w.x - drag.dx, ny = w.y - drag.dy;
    if (S.snap) { nx = Math.round(nx / GRID) * GRID; ny = Math.round(ny / GRID) * GRID; }
    node.position.x = Math.round(nx);
    node.position.y = Math.round(ny);
    drag.el.style.left = node.position.x + "px";
    drag.el.style.top = node.position.y + "px";
    renderEdges();
  }
  if (linking) {
    const sn = S.chain.nodes.find(n => n.id === linking.node);
    const a = portPos(sn, linking.port);
    const b = clientToWorld(e.clientX, e.clientY);
    tempEdge.setAttribute("transform", edgeGroup.getAttribute("transform"));
    tempEdge.setAttribute("d", bez(a, b));
    document.querySelectorAll(".port.hot").forEach(p => p.classList.remove("hot"));
    const tp = e.target.closest(".port");
    if (tp && tp.dataset.kind === "in") tp.classList.add("hot");
  }
});
window.addEventListener("mouseup", (e) => {
  if (drag) { drag.el.classList.remove("dragging"); drag = null; autosave(); drawMinimap(); }
  if (linking) {
    const tp = e.target.closest(".port");
    if (tp && tp.dataset.kind === "in" && tp.dataset.node !== linking.node) {
      createEdge(linking.node, linking.port, tp.dataset.node, tp.dataset.port);
    }
    tempEdge.classList.add("hidden");
    document.querySelectorAll(".port.hot").forEach(p => p.classList.remove("hot"));
    linking = null;
  }
});

function createEdge(source, source_port, target, target_port) {
  const sf = portFlavor(S.chain.nodes.find(n => n.id === source).type, source_port);
  const tf = portFlavor(S.chain.nodes.find(n => n.id === target).type, target_port);
  if (sf !== tf) toast(`Связь ${sf} → ${tf}: типы портов различаются (разрешено, но проверьте логику)`, "");
  S.chain.edges.push({ id: "edge_" + rid(), source, source_port, target, target_port, label: "" });
  renderEdges(); autosave();
}
function deleteEdge(id) {
  S.chain.edges = S.chain.edges.filter(e => e.id !== id);
  renderEdges(); autosave();
}
function addNode(type, world) {
  const spec = S.typesById[type];
  const config = {};
  for (const f of spec.config) config[f.key] = f.default;
  const node = {
    id: "node_" + rid(), type,
    position: { x: Math.round(world.x), y: Math.round(world.y) },
    label: null, config, notes: "",
  };
  S.chain.nodes.push(node);
  renderNodes(); select(node.id); autosave();
  if ($("canvasHint")) $("canvasHint").style.opacity = "0";
}
function deleteNode(id) {
  S.chain.nodes = S.chain.nodes.filter(n => n.id !== id);
  S.chain.edges = S.chain.edges.filter(e => e.source !== id && e.target !== id);
  select(null); renderAll(); autosave();
}

function select(id) {
  S.selection = id;
  document.querySelectorAll(".node").forEach(n => n.classList.toggle("selected", n.dataset.id === id));
  renderInspector();
}

function renderInspector() {
  const drawer = $("inspector"), body = $("inspectorBody");
  const node = S.selection ? S.chain.nodes.find(n => n.id === S.selection) : null;
  if (!node) { drawer.classList.remove("open"); return; }
  const spec = S.typesById[node.type];
  drawer.classList.add("open");
  const accent = S.catsById[spec.category]?.accent || "#888";

  let html = `
    <div class="insp-head">
      <span class="node-ico" style="color:${accent};background:${accent}1f">${nodeIcon(spec.icon)}</span>
      <strong style="font-size:15px">${esc(spec.name)}</strong>
      <span class="risk-dot risk-${spec.risk}" title="риск: ${spec.risk}" style="margin-left:auto"></span>
    </div>
    <div class="insp-type">${esc(node.type)}</div>
    <div class="field">
      <label class="label">Имя узла</label>
      <input class="input" id="f_label" value="${esc(node.label || "")}" placeholder="${esc(spec.name)}" />
    </div>`;

  if (node.config && node.config._real) {
    const r = node.config._real;
    const stateCls = r.runtime === "running" ? "ok" : r.runtime === "error" ? "err" : "soft";
    html += `<div class="insp-real">
      <div class="ir-row"><span>Статус</span><b class="ir-${stateCls}">${esc(r.runtime || "?")}</b></div>
      <div class="ir-row"><span>Связь</span><b>${esc(r.connectivity || "?")}</b></div>
      <div class="ir-row"><span>Уровень</span><b>${esc(r.level || "?")}</b></div>
      ${r.agent ? `<div class="ir-row"><span>Агент</span><b>${esc(r.agent)}</b></div>` : ""}
      <div class="ir-id">ID ${esc(r.id || "")}</div>
      <div class="insp-real-actions">
        <button class="btn-mini" id="btnRealStart">Запустить</button>
        <button class="btn-mini" id="btnRealStop">Остановить</button>
      </div>
    </div>`;
  } else if (node.type.startsWith("asset/")) {
    const profs = S.realProfiles || [];
    html += `<div class="insp-real insp-materialize">
      <div class="im-hint">Сделать блок боевым: создать ловушку на дашборде по профилю и привязать к узлу.</div>` +
      (profs.length
        ? `<select class="input" id="f_profile">${profs.map(p =>
             `<option value="${esc(p.id)}">${esc(p.name)} · ${esc(p.type_id || "")}</option>`).join("")}</select>
           <button class="btn-mini btn-mini-primary" id="btnMaterialize" style="width:100%;margin-top:8px">＋ Создать ловушку на дашборде</button>`
        : `<div class="ir-soft">Нет доступных профилей (бэкенд офлайн или профили не заданы). Нажмите «Синхронизировать».</div>`) +
      `</div>`;
  }

  for (const f of spec.config) html += renderField(f, node.config[f.key]);
  if (node.type === 'http/send') {
    const target = S.realTraps.find(t => t.id === node.config.target_id);
    const profile = S.realProfiles?.find(p => p.id === target?.profile_id);
    if (profile?.config?.services) {
      const inputs = profile.config.services.map(s => {
        const entry = profile.config.blocks?.find(b => b.id === s.entry);
        return `${s.name}: ${entry?.config?.method || 'ANY'} ${entry?.config?.path || '*'} · порт ${s.port}`;
      });
      html += `<div class="insp-doc">HTTP-входы получателя:<br>${inputs.map(esc).join('<br>')}</div>`;
    }
  }

  html += `<div class="insp-doc">${esc(spec.doc || "")}</div>`;
  html += `<div class="insp-actions">
      <button class="pill pill-danger pill-wide" id="btnDelNode">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m2 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6M10 11v6M14 11v6"/></svg>
        Удалить узел
      </button>
    </div>`;
  body.innerHTML = html;
  bindInspector(node, spec);
}

function renderField(f, val) {
  const id = "f_" + f.key;
  let ctrl = "";
  if (f.type === "trap") {
    ctrl = `<select class="input" id="${id}"><option value="">Выберите HTTP-ловушку</option>${S.realTraps.filter(t => t.type_id === 'custom-http').map(t => `<option value="${esc(t.id)}" ${t.id === val ? 'selected' : ''}>${esc(t.name)} · ${esc(t.runtime_state)}</option>`).join('')}</select>`;
  } else if (f.type === "node") {
    ctrl = `<select class="input" id="${id}"><option value="">Выберите HTTP-вход</option>${S.chain.nodes.filter(n => n.type === 'http/input').map(n => `<option value="${esc(n.id)}" ${n.id === val ? 'selected' : ''}>${esc(n.label || n.config.name || n.id)} · ${esc(n.config.port)}</option>`).join('')}</select>`;
  } else if (f.type === "toggle") {
    ctrl = `<div class="field-row"><div class="toggle ${val ? "on" : ""}" id="${id}" role="switch"></div>
      <span class="hint" style="margin:0">${val ? "включено" : "выключено"}</span></div>`;
  } else if (f.type === "select") {
    ctrl = `<select class="input" id="${id}">${f.options.map(o =>
      `<option ${o === val ? "selected" : ""}>${esc(o)}</option>`).join("")}</select>`;
  } else if (f.type === "textarea") {
    ctrl = `<textarea class="input" id="${id}" rows="3">${esc(val || "")}</textarea>`;
  } else if (f.type === "tags") {
    const tags = (val || []).map((t, i) => `<span class="tag">${esc(t)}<b data-i="${i}">✕</b></span>`).join("");
    ctrl = `<div class="tags-wrap" id="${id}_wrap">${tags}</div>
      <input class="input input-sm" id="${id}" placeholder="добавить + Enter" style="width:100%" />`;
  } else {
    const type = f.type === "number" ? "number" : (f.type === "secret" ? "password" : "text");
    ctrl = `<input class="input" id="${id}" type="${type}" value="${esc(val ?? "")}"
      placeholder="${esc(f.placeholder || "")}" />`;
  }
  return `<div class="field"><label class="label">${esc(f.label)}</label>${ctrl}
    ${f.hint ? `<div class="hint">${esc(f.hint)}</div>` : ""}</div>`;
}

function bindInspector(node, spec) {
  $("f_label").addEventListener("input", (e) => {
    node.label = e.target.value || null;
    const el = nodeLayer.querySelector(`.node[data-id="${node.id}"] .node-title`);
    if (el) el.textContent = node.label || spec.name;
    autosave();
  });
  for (const f of spec.config) {
    const el = $("f_" + f.key);
    if (!el) continue;
    if (f.type === "toggle") {
      el.addEventListener("click", () => {
        node.config[f.key] = !node.config[f.key];
        renderInspector(); refreshNodeCard(node); autosave();
      });
    } else if (f.type === "tags") {
      el.addEventListener("keydown", (e) => {
        if (e.key === "Enter" && e.target.value.trim()) {
          node.config[f.key] = [...(node.config[f.key] || []), e.target.value.trim()];
          e.target.value = ""; renderInspector(); refreshNodeCard(node); autosave();
        }
      });
      $("f_" + f.key + "_wrap").querySelectorAll("b").forEach(b =>
        b.addEventListener("click", () => {
          node.config[f.key].splice(+b.dataset.i, 1);
          renderInspector(); refreshNodeCard(node); autosave();
        }));
    } else {
      el.addEventListener("input", () => {
        node.config[f.key] = f.type === "number" ? (el.value === "" ? null : +el.value) : el.value;
        refreshNodeCard(node); autosave();
        if (f.type === 'trap') renderInspector();
      });
    }
  }
  $("btnDelNode").addEventListener("click", () => deleteNode(node.id));
  $("btnMaterialize")?.addEventListener("click", () => materializeNode(node, spec));
  if (node.config && node.config._real) {
    $("btnRealStart")?.addEventListener("click", () => realNodeCommand(node, "start"));
    $("btnRealStop")?.addEventListener("click", () => realNodeCommand(node, "stop"));
  }
}

async function materializeNode(node, spec) {
  const profileId = $("f_profile")?.value;
  if (!profileId) { toast("Выберите профиль", "err"); return; }
  const btn = $("btnMaterialize");
  if (btn) { btn.disabled = true; btn.textContent = "Создаётся…"; }
  try {
    const name = node.label || spec.name;
    const res = await api.realCreateTrap({ name, profile_id: profileId, description: "Создано из конструктора", request_id: crypto.randomUUID() });
    const t = res.trap;
    node.config._real = {
      id: t.id, runtime: t.runtime_state || t.desired_state || "stopped",
      connectivity: t.connectivity || "offline",
      level: t.interaction_level, agent: t.agent?.hostname,
    };
    toast(`Ловушка «${name}» создана на дашборде`, "ok");
    await loadRealData();            // подтянуть её в список реальных
    renderInspector(); refreshNodeCard(node); autosave();
  } catch (e) {
    toast(e.message || "Не удалось создать ловушку", "err");
    if (btn) { btn.disabled = false; btn.textContent = "＋ Создать ловушку на дашборде"; }
  }
}

async function realNodeCommand(node, action) {
  const r = node.config && node.config._real;
  if (!r || !r.id) return;
  toast(`Команда «${action}» отправляется…`);
  try {
    await req('POST', `/api/real/traps/${r.id}/command`, { action });
    renderNodes(); if (S.selection === node.id) renderInspector(); autosave();
    toast("Команда принята бэкендом", "ok");
    setTimeout(() => refreshRealState(node), 2600);  // подтянуть фактический статус
  } catch (e) { toast("Ошибка команды: " + e.message, "err"); }
}

async function refreshRealState(node) {
  const r = node.config && node.config._real;
  if (!r || !r.id) return;
  try {
    const res = await req('GET', '/api/real/traps');
    const t = (res.items || []).find(x => x.id === r.id);
    if (t) {
      r.runtime = t.runtime_state; r.connectivity = t.connectivity;
      renderNodes(); if (S.selection === node.id) renderInspector(); autosave();
    }
  } catch (_) {}
}

function refreshNodeCard(node) {
  const el = nodeLayer.querySelector(`.node[data-id="${node.id}"] .node-body`);
  if (el) el.innerHTML = node.type.startsWith("real/") ? realChips(node) : summaryChips(node, S.typesById[node.type]);
}

function renderPalette(filter = "") {
  const list = $("paletteList");
  list.innerHTML = "";
  const f = filter.toLowerCase();

  // 1) Проектные блоки защиты (инфраструктура / защита / приманки)
  for (const cat of S.registry.categories) {
    const items = S.registry.node_types.filter(t => t.category === cat.id &&
      (!f || t.name.toLowerCase().includes(f) || (t.subtitle || "").toLowerCase().includes(f)));
    if (!items.length) continue;
    const h = document.createElement("div");
    h.className = "pal-cat"; h.textContent = cat.label; list.appendChild(h);
    for (const t of items) {
      const d = document.createElement("div");
      d.className = "pal-item"; d.draggable = true; d.dataset.type = t.type;
      d.innerHTML = `<span class="pal-ico" style="color:${cat.accent};background:${cat.accent}1f">${nodeIcon(t.icon)}</span>
        <div style="min-width:0"><div class="pi-name">${esc(t.name)}</div>
        <div class="pi-sub">${esc(t.subtitle || "")}</div></div>`;
      d.addEventListener("dragstart", (e) => e.dataTransfer.setData("type", t.type));
      d.addEventListener("dblclick", () => addNode(t.type, centerWorld()));
      list.appendChild(d);
    }
  }

  const hr = document.createElement("div");
  hr.className = "pal-cat"; hr.textContent = "Ловушки из дашборда"; list.appendChild(hr);
  if (!S.realOnline) {
    const e = document.createElement("div"); e.className = "pal-empty";
    e.innerHTML = "Бэкенд дашборда недоступен.<br>Нажмите «Синхронизировать»."; list.appendChild(e); return;
  }
  const traps = (S.realTraps || []).filter(t =>
    !f || t.name.toLowerCase().includes(f) || (t.type_id || "").toLowerCase().includes(f));
  if (!traps.length) {
    const e = document.createElement("div"); e.className = "pal-empty";
    e.textContent = "В дашборде нет ловушек."; list.appendChild(e); return;
  }
  const accent = "#2E9E8F";
  for (const t of traps) {
    const onBoard = S.chain?.nodes.some(n => n.config?._real?.id === t.id);
    const run = t.runtime_state === "running";
    const d = document.createElement("div");
    d.className = "pal-item" + (onBoard ? " on-board" : "");
    d.draggable = true; d.dataset.trapId = t.id;
    d.innerHTML = `<span class="pal-ico" style="color:${accent};background:${accent}1f">${nodeIcon(realIcon(t.type_id))}</span>
      <div style="min-width:0"><div class="pi-name">${esc(t.name)}</div>
      <div class="pi-sub">${esc(t.connectivity || "?")} · ${esc(t.runtime_state || "?")}</div></div>
      <span class="pal-state${run ? " run" : ""}" title="${esc(t.runtime_state || "")}"></span>`;
    d.addEventListener("dragstart", (e) => e.dataTransfer.setData("trapId", t.id));
    d.addEventListener("click", () => {
      const ex = S.chain?.nodes.find(n => n.config?._real?.id === t.id);
      if (ex) select(ex.id); else placeTrap(t, centerWorld());
    });
    list.appendChild(d);
  }
}
function centerWorld() {
  const r = wrap.getBoundingClientRect();
  return clientToWorld(r.left + r.width / 2, r.top + r.height / 2);
}
function placeTrap(t, world) {
  if (!S.chain) return;
  const ex = S.chain.nodes.find(n => n.config?._real?.id === t.id);
  if (ex) { select(ex.id); return; }
  const type = "real/" + t.type_id;
  if (!S.typesById[type]) return;
  S.chain.nodes.push({
    id: "node_" + rid(), type,
    position: { x: Math.round(world.x - 100), y: Math.round(world.y - 60) },
    label: t.name, notes: "",
    config: { _real: { id: t.id, runtime: t.runtime_state, connectivity: t.connectivity,
                       level: t.interaction_level, agent: t.agent?.hostname } },
  });
  renderNodes(); renderPalette($("paletteSearch")?.value || ""); select(S.chain.nodes.at(-1).id);
  autosave(); if ($("canvasHint")) $("canvasHint").style.opacity = "0";
}
wrap.addEventListener("dragover", (e) => e.preventDefault());
wrap.addEventListener("drop", (e) => {
  e.preventDefault();
  const trapId = e.dataTransfer.getData("trapId");
  if (trapId) {
    const t = (S.realTraps || []).find(x => x.id === trapId);
    if (t) placeTrap(t, clientToWorld(e.clientX, e.clientY));
    return;
  }
  const type = e.dataTransfer.getData("type");
  if (type && S.typesById[type]) addNode(type, clientToWorld(e.clientX - NODE_W / 2 * S.vp.zoom, e.clientY - 20));
});

function markDirty() { S.dirty = true; $("saveState").textContent = "не сохранено"; $("saveState").classList.add("dirty"); }
let saveTimer = null;
function autosave() {
  if (!S.canManage) return;
  markDirty();
  clearTimeout(saveTimer);
  saveTimer = setTimeout(doSave, 650);
}
async function doSave() {
  if (!S.chain) return;
  try {
    await api.updateChain(S.chain.id, {
      name: S.chain.name, description: S.chain.description, tags: S.chain.tags,
      nodes: S.chain.nodes, edges: S.chain.edges, viewport: S.vp,
    });
    S.dirty = false;
    $("saveState").textContent = "сохранено"; $("saveState").classList.remove("dirty");
    return true;
  } catch (err) { toast("Ошибка сохранения: " + err.message, "err"); return false; }
}

function zoomBy(f) {
  const r = wrap.getBoundingClientRect(), sx = r.width / 2, sy = r.height / 2;
  const nz = Math.min(2.5, Math.max(0.2, S.vp.zoom * f));
  S.vp.x = sx - (sx - S.vp.x) * (nz / S.vp.zoom);
  S.vp.y = sy - (sy - S.vp.y) * (nz / S.vp.zoom);
  S.vp.zoom = nz; applyViewport(); markDirty();
}
function fit() {
  if (!S.chain || !S.chain.nodes.length) return;
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const n of S.chain.nodes) {
    minX = Math.min(minX, n.position.x); minY = Math.min(minY, n.position.y);
    maxX = Math.max(maxX, n.position.x + NODE_W); maxY = Math.max(maxY, n.position.y + nodeHeight(n.type));
  }
  const r = wrap.getBoundingClientRect(), pad = 80;
  const zoom = Math.min(1.4, (r.width - pad * 2) / (maxX - minX), (r.height - pad * 2) / (maxY - minY));
  S.vp.zoom = Math.max(0.2, zoom);
  S.vp.x = (r.width - (maxX + minX) * S.vp.zoom) / 2;
  S.vp.y = (r.height - (maxY + minY) * S.vp.zoom) / 2;
  applyViewport(); markDirty();
}

function autoLayout() {
  if (!S.chain || !S.chain.nodes.length) return;
  const byId = Object.fromEntries(S.chain.nodes.map(n => [n.id, n]));
  const outAdj = {}, indeg = {};
  for (const n of S.chain.nodes) { outAdj[n.id] = []; indeg[n.id] = 0; }
  for (const e of S.chain.edges) {
    if (byId[e.source] && byId[e.target]) { outAdj[e.source].push(e.target); indeg[e.target]++; }
  }
  // уровни: BFS от узлов без входящих (точки входа)
  const level = {};
  let frontier = S.chain.nodes.filter(n => indeg[n.id] === 0).map(n => n.id);
  if (!frontier.length) frontier = [S.chain.nodes[0].id];
  frontier.forEach(id => level[id] = 0);
  const q = [...frontier];
  while (q.length) {
    const id = q.shift();
    for (const t of outAdj[id]) {
      const nl = level[id] + 1;
      if (level[t] === undefined || nl > level[t]) { level[t] = nl; q.push(t); }
    }
  }
  S.chain.nodes.forEach(n => { if (level[n.id] === undefined) level[n.id] = 0; });
  const cols = {};
  for (const n of S.chain.nodes) (cols[level[n.id]] ||= []).push(n);
  const COL_W = 290, ROW_H = 150, X0 = 90, Y0 = 90;
  Object.keys(cols).sort((a, b) => a - b).forEach(lv => {
    cols[lv].forEach((n, i) => { n.position.x = X0 + lv * COL_W; n.position.y = Y0 + i * ROW_H; });
  });
  renderAll(); autosave(); setTimeout(fit, 30);
  toast("Узлы разложены по attack-path", "ok");
}

function exportJSON() {
  if (!S.chain) return;
  const data = JSON.stringify(S.chain, null, 2);
  const blob = new Blob([data], { type: "application/json" });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = (S.chain.name || "chain").replace(/[^\wа-яё -]+/gi, "_") + ".json";
  a.click();
  URL.revokeObjectURL(a.href);
  toast("Цепочка выгружена в JSON", "ok");
}

async function validate() {
  if (!S.chain) return;
  await doSave();
  const res = await api.validate(S.chain.id);
  if (res.ok && !res.issues.length) return toast("Цепочка валидна — готова к развёртыванию", "ok");
  const items = res.issues.map(i => `[${i.level}] ${i.message}`);
  toast(res.ok ? "Предупреждения:" : "Найдены ошибки:", res.ok ? "" : "err", items);
  document.querySelectorAll(".node.active").forEach(n => n.classList.remove("active"));
  for (const i of res.issues) if (i.node_id) {
    const el = nodeLayer.querySelector(`.node[data-id="${i.node_id}"]`);
    if (el) { el.classList.add("active"); setTimeout(() => el.classList.remove("active"), 3000); }
  }
}

async function simulate() {
  if (!S.chain) return;
  await doSave();
  const res = await api.simulate(S.chain.id, { attacker_profile: $("simProfile").value });
  $("simPanel").classList.remove("hidden");
  $("simVerdict").textContent =
    `достигнуто узлов: ${res.summary.reached} · техник: ${res.summary.techniques.join(", ") || "—"} · ${res.summary.verdict}`;
  const tl = $("simTimeline");
  tl.innerHTML = res.steps.map((s, i) => `
    <div class="sim-step ${s.is_detection ? "detection" : ""}" data-node="${s.node_id}">
      <div class="st-no">${i + 1}</div>
      <div class="st-main">
        <div class="st-title">${esc(s.title)}</div>
        <div class="st-detail">${esc(s.detail)}</div>
        ${s.technique !== "—" ? `<div class="st-meta">${s.tactic} · ${s.technique}</div>` : ""}
      </div>
      <div class="st-time">+${(s.at_ms / 1000).toFixed(1)}s</div>
    </div>`).join("");
  animatePath(res);
}
function animatePath(res) {
  const steps = [...document.querySelectorAll(".sim-step")];
  document.querySelectorAll(".node.active").forEach(n => n.classList.remove("active"));
  const activeEdges = new Set();
  let i = 0;
  clearInterval(animatePath._t);
  animatePath._t = setInterval(() => {
    if (i >= res.steps.length) { clearInterval(animatePath._t); return; }
    const s = res.steps[i];
    steps[i]?.classList.add("shown");
    steps[i]?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    document.querySelectorAll(".node.active").forEach(n => n.classList.remove("active"));
    const el = nodeLayer.querySelector(`.node[data-id="${s.node_id}"]`);
    if (el) el.classList.add("active");
    if (res.path.edges[i]) { activeEdges.add(res.path.edges[i]); renderEdges(activeEdges); }
    i++;
  }, 750);
}

async function toggleDeploy() {
  if (!S.chain) return;
  const action = Object.values(S.runtime.status).some(s => s === 'running') ? 'stop' : 'deploy';
  if (!await doSave()) return;
  const st = await api.deploy(S.chain.id, action);
  applyRuntime(st);
  subscribeLive();
  toast(action === 'deploy' ? 'Развёртывание принято. Дождитесь запуска ловушек.' : 'Команды остановки приняты', 'ok');
}

function applyRuntime(st) {
  const wasDeployed = S.runtime.deployed;
  S.runtime.deployed = st.deployed;
  S.runtime.status = st.node_status || {};
  for (const node of S.chain?.nodes || []) {
    const trap = st.traps?.[node.id];
    if (trap) node.config._real = { id: trap.id, runtime: trap.runtime_state, connectivity: trap.connectivity, agent: trap.agent?.hostname };
  }
  if (!st.deployed && wasDeployed) S.runtime.hits = {};  // сброс счётчиков при остановке
  const btn = $("btnDeploy");
  btn.textContent = st.kpi?.active_traps ? 'Остановить' : 'Развернуть';
  btn.classList.toggle("live", st.deployed);
  $("kpiDock").classList.toggle("hidden", !st.deployed);
  if (st.kpi) updateKPI(st.kpi);
  renderNodes();
}

function subscribeLive() {
  unsubscribeLive();
  const es = new EventSource(`${BASE}/api/chains/${S.chain.id}/stream`);
  S.runtime.es = es;
  es.onmessage = (e) => { try { handleLiveEvent(JSON.parse(e.data)); } catch (_) {} };
  es.onerror = () => { /* EventSource сам переподключится */ };
}
function unsubscribeLive() {
  if (S.runtime.es) { S.runtime.es.close(); S.runtime.es = null; }
}

function handleLiveEvent(ev) {
  if (ev.kind === "hello") { applyRuntime(ev.status); }
  else if (ev.kind === "kpi") { updateKPI(ev.kpi); }
  else if (ev.kind === "hop") {
    if (ev.edge_id) flashAttackEdge(ev.edge_id);
    if (ev.node_id) {
      pulseNode(ev.node_id, ev.is_detection);
      floatLabel(ev.node_id, ev.technique !== "—" ? ev.technique : ev.title);
    }
  }
}

function updateKPI(k) {
  $("kpiTraps").textContent = k.active_traps ?? 0;
}

function edgeGeom(edgeId) {
  const e = S.chain.edges.find(x => x.id === edgeId);
  if (!e) return null;
  const byId = Object.fromEntries(S.chain.nodes.map(n => [n.id, n]));
  const sn = byId[e.source], tn = byId[e.target];
  if (!sn || !tn) return null;
  return { d: bez(portPos(sn, e.source_port), portPos(tn, e.target_port)) };
}

function flashAttackEdge(edgeId) {
  const g = edgeGeom(edgeId);
  if (!g) return;
  const NS = "http://www.w3.org/2000/svg";
  const path = document.createElementNS(NS, "path");
  path.setAttribute("class", "edge attack-flow");
  path.setAttribute("d", g.d);
  edgeGroup.appendChild(path);
  const dot = document.createElementNS(NS, "circle");
  dot.setAttribute("class", "atk-dot"); dot.setAttribute("r", "5");
  edgeGroup.appendChild(dot);

  const total = path.getTotalLength();
  const dur = 850; const t0 = performance.now();
  function step(now) {
    const p = Math.min(1, (now - t0) / dur);
    const pt = path.getPointAtLength(p * total);
    dot.setAttribute("cx", pt.x); dot.setAttribute("cy", pt.y);
    if (p < 1) requestAnimationFrame(step);
    else {
      dot.remove();
      path.style.transition = "opacity .8s"; path.style.opacity = "0";
      setTimeout(() => path.remove(), 850);
    }
  }
  requestAnimationFrame(step);
}

function pulseNode(nodeId, isDetection) {
  const el = nodeLayer.querySelector(`.node[data-id="${nodeId}"]`);
  if (!el) return;
  el.classList.add("under-attack");
  if (isDetection) el.classList.add("compromised");
  clearTimeout(el._atk);
  el._atk = setTimeout(() => el.classList.remove("under-attack"), 2200);
  S.runtime.hits[nodeId] = (S.runtime.hits[nodeId] || 0) + 1;
  const badge = el.querySelector(".node-hits");
  if (badge) {
    badge.dataset.hits = S.runtime.hits[nodeId];
    badge.textContent = S.runtime.hits[nodeId];
    badge.title = `Событий узла за текущий просмотр: ${S.runtime.hits[nodeId]}. Начало и завершение блока считаются отдельно.`;
    badge.classList.remove("hidden");
    badge.classList.remove("bump"); void badge.offsetWidth; badge.classList.add("bump");
  }
}

function floatLabel(nodeId, text) {
  if (!text) return;
  const node = S.chain?.nodes.find(n => n.id === nodeId);
  if (!node) return;

  nodeLayer.querySelector(`.hit-float[data-node-id="${nodeId}"]`)?.remove();

  const d = document.createElement("div");
  d.className = "hit-float";
  d.dataset.nodeId = nodeId;
  d.textContent = text;
  d.style.left = (node.position.x + NODE_W / 2) + "px";
  d.style.top = node.position.y + "px";
  nodeLayer.appendChild(d);
  setTimeout(() => d.remove(), 1400);
}

const REAL_PORTS = [
  { id: "in", label: "вход", kind: "in", flavor: "flow" },
  { id: "out", label: "скомпрометирован", kind: "out", flavor: "flow" },
  { id: "loot", label: "добыча", kind: "out", flavor: "loot" },
];
function realIcon(typeId) {
  if (typeId === "redis-emulator") return "database";
  if (typeId === "tcp-banner") return "terminal";
  return "shield";
}
async function loadRealData() {
  try {
    const [catRes, trapRes, profRes] = await Promise.all([
      req('GET', '/api/real/catalog'),
      req('GET', '/api/real/traps'),
      api.realProfiles(),
    ]);
    S.realOnline = !!(catRes.online && trapRes.online);
    S.realProfiles = profRes.items || [];
    S.realTypes = {};
    for (const e of (catRes.items || [])) {
      S.realTypes[e.type_id] = e;
      const type = "real/" + e.type_id;
      S.typesById[type] = {
        type, category: "real", name: e.title, subtitle: "",
        icon: realIcon(e.type_id),
        risk: e.interaction_level === "high" ? "high" : e.interaction_level === "medium" ? "medium" : "low",
        ports: REAL_PORTS, config: [], doc: e.description || "",
      };
    }
    S.realTraps = trapRes.items || [];
  } catch (_) { S.realOnline = false; S.realTraps = []; }
}
function ensureRoot() {
  if (!S.chain || !S.typesById["entry/internet"]) return;
  if (S.chain.nodes.some(n => n.type === "entry/internet")) return;
  const spec = S.typesById["entry/internet"];
  const config = {};
  for (const fld of spec.config) config[fld.key] = fld.default;
  S.chain.nodes.unshift({
    id: "node_" + rid(), type: "entry/internet",
    position: { x: 80, y: 40 }, label: null, config, notes: "",
  });
}
function autoPlaceAll() {
  const root = S.chain.nodes.find(n => n.type === "entry/internet");
  let n = 0;
  for (const t of S.realTraps) {
    const type = "real/" + t.type_id;
    if (!S.typesById[type]) continue;
    if (S.chain.nodes.some(x => x.config?._real?.id === t.id)) continue;
    const id = "node_" + rid();
    S.chain.nodes.push({
      id, type,
      position: { x: 200 + (n % 4) * 250, y: 220 + Math.floor(n / 4) * 180 },
      label: t.name, notes: "",
      config: { _real: { id: t.id, runtime: t.runtime_state, connectivity: t.connectivity,
                         level: t.interaction_level, agent: t.agent?.hostname } },
    });
    if (root) S.chain.edges.push({
      id: "edge_" + rid(), source: root.id, source_port: "out", target: id, target_port: "in", label: "",
    });
    n++;
  }
  if (n && $("canvasHint")) $("canvasHint").style.opacity = "0";
}
async function syncReal(auto = false) {
  await loadRealData();
  if (S.chain) {
    for (const n of S.chain.nodes) {
      const r = n.config && n.config._real; if (!r) continue;
      const t = S.realTraps.find(x => x.id === r.id);
      if (t) { r.runtime = t.runtime_state; r.connectivity = t.connectivity; r.agent = t.agent?.hostname; }
    }
    renderNodes(); if (S.selection) renderInspector();
  }
  renderPalette($("paletteSearch")?.value || "");
  if (!auto) toast(S.realOnline ? "Синхронизировано с дашбордом" : "Бэкенд дашборда недоступен",
                   S.realOnline ? "ok" : "err");
}

async function refreshChains() {
  S.chains = await api.listChains();
  const menu = $("chainMenu");
  const activeId = S.chain?.id;
  menu.innerHTML = `<div class="dd-head">Мои доски</div>` +
    S.chains.map(c => `<div class="dd-item ${c.id === activeId ? "active" : ""}" data-id="${c.id}">
        <div class="dd-item-main"><div>${esc(c.name)}</div><small>${c.node_count} узлов · ${c.edge_count} связей</small></div>
        <div class="dd-actions">
          <button class="dd-act" data-act="rename" data-id="${c.id}" title="Переименовать">✎</button>
          <button class="dd-act" data-act="dup" data-id="${c.id}" title="Дублировать">⧉</button>
          <button class="dd-act danger" data-act="del" data-id="${c.id}" title="Удалить">🗑</button>
        </div>
      </div>`).join("") +
    `<div class="dd-sep"></div>
     <div class="dd-item" id="ddNew"><strong>+ Новая доска</strong></div>`;
  menu.querySelectorAll(".dd-item[data-id] .dd-item-main").forEach(main =>
    main.addEventListener("click", () => { closeMenu(); loadChain(main.closest("[data-id]").dataset.id); }));
  menu.querySelectorAll(".dd-act").forEach(btn => btn.addEventListener("click", async (e) => {
    e.stopPropagation();
    const id = btn.dataset.id, act = btn.dataset.act;
    const chain = S.chains.find(c => c.id === id);
    if (act === "rename") {
      const name = prompt("Новое название доски:", chain?.name || "");
      if (name === null || !name.trim()) return;
      await api.updateChain(id, { name: name.trim() });
      await refreshChains();
      if (id === activeId) $("chainName").textContent = name.trim();
    } else if (act === "dup") {
      closeMenu(); const c = await api.duplicateChain(id); await refreshChains(); loadChain(c.id);
      toast("Доска продублирована", "ok");
    } else if (act === "del") {
      if (S.chains.length <= 1) { toast("Нельзя удалить единственную доску", "err"); return; }
      if (!confirm(`Удалить доску «${chain?.name}»?`)) return;
      closeMenu(); await api.deleteChain(id); await refreshChains();
      if (id === activeId) loadChain(S.chains[0]?.id);
      toast("Доска удалена", "ok");
    }
  }));
  $("ddNew").addEventListener("click", newChainFlow);
}
async function newChainFlow() {
  closeMenu();
  const name = prompt("Название новой доски:", "Новая доска");
  if (name === null) return;
  const c = await api.createChain({ name: name.trim() || "Новая доска" });
  await refreshChains();
  await loadChain(c.id);   // новая доска создаётся пустой — холст под свою схему
  toast("Доска создана", "ok");
}
function closeMenu() { $("chainMenu").classList.add("hidden"); }

async function loadChain(id) {
  unsubscribeLive();
  if (!id) { S.chain = null; renderAll(); $("chainName").textContent = "—"; return; }
  S.chain = await api.getChain(id);
  S.vp = S.chain.viewport && S.chain.viewport.zoom ? { ...S.chain.viewport } : { x: 120, y: 80, zoom: 1 };
  $("chainName").textContent = S.chain.name;
  S.selection = null;
  renderAll(); renderInspector();
  $("saveState").textContent = "сохранено"; $("saveState").classList.remove("dirty");
  if (S.chain.nodes.length) setTimeout(fit, 30);
  applyRuntime(await api.runtime(id));
  subscribeLive();
}

function toggleTheme() {
  const html = document.documentElement;
  const next = html.dataset.theme === "dark" ? "light" : "dark";
  html.dataset.theme = next;
  localStorage.setItem("hf_theme", next);
  setTimeout(drawMinimap, 50);
}

window.addEventListener("keydown", (e) => {
  if (e.target.matches("input, textarea, select")) return;
  if ((e.key === "Delete" || e.key === "Backspace") && S.selection) { e.preventDefault(); deleteNode(S.selection); }
  if (e.key === "Escape") {
    select(null); closeMenu();
    $("palette").classList.remove("open"); $("paletteTab").classList.remove("hidden-tab");
  }
});

function esc(s) { return String(s ?? "").replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])); }
function rid() { return Math.random().toString(36).slice(2, 12); }

const _params = new URLSearchParams(location.search);
const EMBEDDED = _params.has("embed") || _params.has("theme");
function applyTheme(t) {
  if (t !== "light" && t !== "dark") return;
  document.documentElement.dataset.theme = t;
  setTimeout(() => { try { drawMinimap(); } catch (_) {} }, 50);
}
window.addEventListener("message", (e) => {
  if (e.origin !== location.origin || e.source !== parent) return;
  const d = e.data;
  if (d && d.type === "hf-theme") applyTheme(d.theme);
});

async function init() {
  const session = await fetch('/api/session');
  if (!session.ok) throw new Error('Войдите в HoneyForge');
  S.canManage = (await session.json()).user.role === 'admin';
  applyTheme(_params.get("theme") || localStorage.getItem("hf_theme"));
  if (EMBEDDED) document.body.classList.add("embedded");

  // Проектные блоки защиты (инфраструктура/защита/приманки) — из реестра конструктора.
  // Реальные ловушки дашборда добавляются поверх как типы real/* в syncReal().
  S.registry = await api.registry();
  S.typesById = Object.fromEntries(S.registry.node_types.map(t => [t.type, t]));
  S.catsById = Object.fromEntries(S.registry.categories.map(c => [c.id, c]));

  await refreshChains();
  if (!S.chains.length && S.canManage) { await api.createChain({ name: 'Новая ловушка' }); await refreshChains(); }
  await loadChain(S.chains[0]?.id);
  await syncReal(true);  // подтянуть реальные ловушки, корень «Интернет» и разложить под ключ

  setInterval(() => syncReal(true).catch(() => {}), 120000);

  $("btnTheme").addEventListener("click", toggleTheme);
  $("btnDeploy").addEventListener('click', () => toggleDeploy().catch(e => toast(e.message, 'err')));
  $("btnZoomIn").addEventListener("click", () => zoomBy(1.2));
  $("btnZoomOut").addEventListener("click", () => zoomBy(1 / 1.2));
  $("btnFit").addEventListener("click", fit);
  $("btnAutoLayout").addEventListener("click", autoLayout);
  $("btnExport").addEventListener("click", exportJSON);
  $("btnSync")?.addEventListener("click", () => syncReal(false).catch(e => toast(e.message, "err")));
  $("btnSnap").addEventListener("click", () => {
    S.snap = !S.snap; $("btnSnap").classList.toggle("on", S.snap);
    toast(S.snap ? "Привязка к сетке включена" : "Привязка к сетке выключена");
  });
  $("tdHandle").addEventListener("click", () => $("toolDock").classList.toggle("collapsed"));
  $("tdPalette").addEventListener("click", () => openPalette(!$("palette").classList.contains("open")));
  $("paletteSearch").addEventListener("input", (e) => renderPalette(e.target.value));
  const openPalette = (v) => {
    $("palette").classList.toggle("open", v);
    $("paletteTab").classList.toggle("hidden-tab", v);
    $("tdPalette").classList.toggle("active", v);
  };
  $("paletteTab").addEventListener("click", () => openPalette(true));
  $("paletteClose").addEventListener("click", () => openPalette(false));
  $("chainMenuBtn").addEventListener("click", (e) => {
    e.stopPropagation(); $("chainMenu").classList.toggle("hidden");
  });
  document.addEventListener("click", (e) => { if (!e.target.closest(".chain-switcher")) closeMenu(); });
  window.addEventListener("resize", () => applyViewport());
  window.addEventListener("beforeunload", () => { if (S.dirty) doSave(); });
}

init().catch(err => { console.error(err); toast("Ошибка инициализации: " + err.message, "err"); });
