export const BASE = location.pathname.startsWith('/constructor/') ? '/constructor' : '';
let csrf = '';

export async function req(method, path, body) {
  const opt = { method, headers: {} };
  if (method !== 'GET') {
    if (!csrf) {
      const session = await fetch('/api/session');
      if (!session.ok) throw new Error('Войдите в HoneyForge');
      csrf = (await session.json()).csrf_token;
    }
    opt.headers['X-CSRF-Token'] = csrf;
  }
  if (body !== undefined) {
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body);
  }
  const res = await fetch(BASE + path, opt);
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try { const j = await res.json(); msg = j.error?.message || j.detail || msg; } catch (_) {}
    throw new Error(msg);
  }
  if (res.status === 204) return null;
  return res.json();
}

export const api = {
  registry: () => req("GET", "/api/registry"),
  templates: () => req("GET", "/api/templates"),
  listChains: () => req("GET", "/api/chains"),
  getChain: (id) => req("GET", `/api/chains/${id}`),
  createChain: (data) => req("POST", "/api/chains", data),
  updateChain: (id, data) => req("PATCH", `/api/chains/${id}`, data),
  deleteChain: (id) => req("DELETE", `/api/chains/${id}`),
  duplicateChain: (id) => req("POST", `/api/chains/${id}/duplicate`),
  validate: (id) => req("POST", `/api/chains/${id}/validate`, {}),
  simulate: (id, data) => req("POST", `/api/chains/${id}/simulate`, data),
  runtime: (id) => req("GET", `/api/chains/${id}/runtime`),
  deploy: (id, action) => req("POST", `/api/chains/${id}/deploy`, { action }),
  realProfiles: () => req("GET", "/api/real/profiles"),
  realCreateTrap: (data) => req("POST", "/api/real/traps", data),
};
