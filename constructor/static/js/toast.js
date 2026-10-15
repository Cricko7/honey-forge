export function toast(msg, kind = "", issues = null) {
  const t = document.getElementById("toast");
  t.className = "toast " + kind;
  t.textContent = msg;
  if (issues?.length) {
    const list = document.createElement("ul");
    list.className = "toast-list";
    for (const issue of issues) {
      const item = document.createElement("li");
      item.textContent = issue;
      list.appendChild(item);
    }
    t.appendChild(list);
  }
  t.classList.remove("hidden");
  clearTimeout(toast._t);
  toast._t = setTimeout(() => t.classList.add("hidden"), issues?.length ? 6000 : 2600);
}
