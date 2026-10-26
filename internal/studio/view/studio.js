// studio.js — the 3D Studio viewer page. Read-only: it fetches ONE
// schema'd topology DTO from the same loopback origin that served this
// page, using the launch token from the URL fragment (never sent to the
// server by the browser). No other network origin is contacted (CSP
// 'self' only). All dynamic text enters the DOM via textContent — never
// innerHTML — so the DTO cannot inject markup.
import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';

const stateEl = document.getElementById('state');
const stageEl = document.getElementById('stage');
const metaEl = document.getElementById('meta');
const fallbackEl = document.getElementById('fallback');
const fallbackReasonEl = document.getElementById('fallback-reason');
const textualEl = document.getElementById('textual');
const nodeListEl = document.getElementById('node-list');
const edgeListEl = document.getElementById('edge-list');

const KIND_COLORS = {
  host: 0x5aa9e6, cpu: 0x7fbf7f, ram: 0x9aa3b2, gpu: 0xe0a458,
  npu: 0xc084fc, engine: 0xd66a6a, governor: 0x64c7c7,
  sessions: 0x8fd18f, tools: 0xd0b060, model: 0xe6e9ef,
};
const KIND_GEOMETRY = {
  host: new THREE.BoxGeometry(1.6, 1.6, 1.6),
  engine: new THREE.ConeGeometry(0.8, 1.5, 24),
  default: new THREE.SphereGeometry(0.55, 24, 18),
};

let renderer = null, scene = null, camera = null, controls = null;
let nodeMeshes = [], edgeLines = [], disposeList = [];
let topology = null;

function token() {
  const m = location.hash.match(/t=([0-9a-f]+)/);
  return m ? m[1] : '';
}

function setState(text) { stateEl.textContent = text; }

async function fetchTopology() {
  const res = await fetch('/api/view', {
    headers: { 'Authorization': 'Bearer ' + token() },
  });
  if (!res.ok) {
    let detail = '';
    try { detail = (await res.json()).error || ''; } catch { /* body optional */ }
    throw new Error('topology request failed (' + res.status + ')' + (detail ? ': ' + detail : ''));
  }
  return res.json();
}

function webglAvailable() {
  try {
    const c = document.createElement('canvas');
    return !!(c.getContext('webgl2') || c.getContext('webgl'));
  } catch {
    return false;
  }
}

function buildScene() {
  scene = new THREE.Scene();
  scene.background = new THREE.Color(0x14171c);

  camera = new THREE.PerspectiveCamera(55, 1, 0.1, 200);
  camera.position.set(6, 5, 8);

  renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: 'low-power' });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  stageEl.appendChild(renderer.domElement);

  controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;
  controls.dampingFactor = 0.12;
  controls.addEventListener('change', () => scheduleRender());

  const light = new THREE.HemisphereLight(0xdfe7f2, 0x20242c, 1.15);
  scene.add(light);
  disposeList.push(light);

  layoutGraph();
  fitCamera();
}

function layoutGraph() {
  // Deterministic radial layout: host at center, others ringed by kind.
  const byKind = {};
  topology.nodes.forEach(n => (byKind[n.kind] ||= []).push(n));
  const kinds = Object.keys(byKind).sort();
  const host = topology.nodes.find(n => n.kind === 'host');
  let placed = 0;

  topology.nodes.forEach(n => {
    let x = 0, z = 0;
    if (host && n.id === host.id) {
      x = 0; z = 0;
    } else {
      const ring = host ? 1 : 0;
      const angle = (placed / Math.max(1, topology.nodes.length - (host ? 1 : 0))) * Math.PI * 2;
      const radius = 4.2 + ring;
      x = Math.cos(angle) * radius;
      z = Math.sin(angle) * radius;
      placed++;
    }
    const geo = KIND_GEOMETRY[n.kind] || KIND_GEOMETRY.default;
    const mat = new THREE.MeshLambertMaterial({ color: KIND_COLORS[n.kind] || 0x9aa3b2 });
    const mesh = new THREE.Mesh(geo, mat);
    mesh.position.set(x, 0, z);
    mesh.userData = n;
    scene.add(mesh);
    nodeMeshes.push(mesh);
    disposeList.push(geo, mat);
  });

  const lineMat = new THREE.LineBasicMaterial({ color: 0x3f4a58, transparent: true, opacity: 0.9 });
  disposeList.push(lineMat);
  topology.edges.forEach(e => {
    const a = nodeMeshes.find(m => m.userData.id === e.from);
    const b = nodeMeshes.find(m => m.userData.id === e.to);
    if (!a || !b) return;
    const g = new THREE.BufferGeometry().setFromPoints([a.position.clone(), b.position.clone()]);
    const line = new THREE.Line(g, lineMat);
    line.userData = e;
    scene.add(line);
    edgeLines.push(line);
    disposeList.push(g);
  });
}

function fitCamera() {
  if (!nodeMeshes.length) return;
  const box = new THREE.Box3().setFromObject(nodeMeshes[0]);
  nodeMeshes.forEach(m => box.expandByObject(m));
  const center = box.getCenter(new THREE.Vector3());
  const radius = Math.max(2, box.getSize(new THREE.Vector3()).length() / 2);
  const dist = radius / Math.sin((camera.fov * Math.PI / 180) / 2) * 0.9;
  const dir = new THREE.Vector3(0.7, 0.6, 1).normalize();
  camera.position.copy(center).add(dir.multiplyScalar(dist));
  controls.target.copy(center);
  controls.update();
  scheduleRender();
}

function resetCamera() { fitCamera(); }

// Render-on-demand: no continuous animation loop. A single frame is drawn
// on load, on control interaction, on resize and on topology refresh; the
// tab-hidden state skips rendering entirely.
let frameQueued = false;
function scheduleRender() {
  if (frameQueued || document.hidden) return;
  frameQueued = true;
  requestAnimationFrame(() => {
    frameQueued = false;
    if (controls) controls.update();
    if (renderer && scene && camera) renderer.render(scene, camera);
  });
}

function resize() {
  if (!renderer) return;
  const w = stageEl.clientWidth || 1, h = stageEl.clientHeight || 1;
  camera.aspect = w / h;
  camera.updateProjectionMatrix();
  renderer.setSize(w, h);
  scheduleRender();
}
window.addEventListener('resize', resize);
document.addEventListener('visibilitychange', () => { if (!document.hidden) scheduleRender(); });

function disposeScene() {
  disposeList.forEach(d => {
    if (d && typeof d.dispose === 'function') d.dispose();
  });
  disposeList = [];
  nodeMeshes = [];
  edgeLines = [];
  if (renderer) {
    renderer.dispose();
    if (renderer.domElement && renderer.domElement.parentNode) {
      renderer.domElement.parentNode.removeChild(renderer.domElement);
    }
    renderer = null;
  }
  scene = null; camera = null; controls = null;
}

function renderTextual() {
  nodeListEl.replaceChildren();
  edgeListEl.replaceChildren();
  topology.nodes.forEach(n => {
    const li = document.createElement('li');
    const kind = document.createElement('span');
    kind.className = 'kind';
    kind.textContent = '[' + n.kind + '] ';
    li.appendChild(kind);
    li.appendChild(document.createTextNode(n.label + (n.detail ? ' — ' + n.detail : '')));
    if (n.status) {
      const st = document.createElement('span');
      st.className = 'status-' + n.status;
      st.textContent = '  (' + n.status + ')';
      li.appendChild(st);
    }
    if (n.metrics) {
      const parts = Object.entries(n.metrics).map(([k, v]) => k + ': ' + v);
      if (parts.length) {
        const m = document.createElement('span');
        m.className = 'status-unknown';
        m.textContent = '  {' + parts.join(', ') + '}';
        li.appendChild(m);
      }
    }
    nodeListEl.appendChild(li);
  });
  topology.edges.forEach(e => {
    const li = document.createElement('li');
    li.textContent = e.from + ' —' + e.relation + '→ ' + e.to;
    edgeListEl.appendChild(li);
  });
}

async function load() {
  if (!webglAvailable()) {
    fallbackEl.hidden = false;
    fallbackReasonEl.textContent =
      'WebGL is not available in this browser/profile (hardware acceleration may be disabled).';
    setState('textual fallback active');
  }
  try {
    topology = await fetchTopology();
  } catch (err) {
    fallbackEl.hidden = false;
    fallbackReasonEl.textContent = String(err && err.message ? err.message : err);
    setState('topology unavailable');
    renderTextual();
    textualEl.hidden = false;
    return;
  }
  metaEl.textContent = 'v' + topology.appVersion + ' · ' + topology.generatedAt +
    ' · ' + topology.nodes.length + ' nodes / ' + topology.edges.length + ' edges';
  renderTextual();
  if (webglAvailable()) {
    disposeScene();
    buildScene();
    resize();
    setState('3D topology rendered (read-only)');
  } else {
    textualEl.hidden = false;
    setState('textual fallback active');
  }
}

document.getElementById('btn-reset').addEventListener('click', resetCamera);
document.getElementById('btn-fit').addEventListener('click', fitCamera);
document.getElementById('btn-reload').addEventListener('click', load);
document.getElementById('btn-textual').addEventListener('click', (ev) => {
  textualEl.hidden = !textualEl.hidden;
  ev.currentTarget.setAttribute('aria-pressed', textualEl.hidden ? 'false' : 'true');
  ev.currentTarget.textContent = textualEl.hidden ? 'Show textual view' : 'Hide textual view';
  resize();
});

window.addEventListener('beforeunload', disposeScene);
load();
