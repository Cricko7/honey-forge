// Тонкий REST-клиент конструктора. Работает против FastAPI-бэкенда того же origin.
const BASE = "";

async function req(method, path, body) {
  const opt = { method, headers: {} };
  if (body !== undefined) {
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body);
  }
  const res = await fetch(BASE + path, opt);
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try { const j = await res.json(); if (j.detail) msg = j.detail; } catch (_) {}
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
  // реальный бэкенд дашборда
  realProfiles: () => req("GET", "/api/real/profiles"),
  realCreateTrap: (data) => req("POST", "/api/real/traps", data),
};
