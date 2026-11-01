"use strict";
// Keep module navigation on the selected host, including loopback aliases and IPv6.
function updateModuleLinks(ports) {
  const routes = {devcycle: new URL("/", window.location.href).href};
  const names = {"8765":"infrascout","8770":"fleetscope","8771":"releaseguard"};
  for (const [port, name] of Object.entries(names)) {
    const target = new URL("/", window.location.href);
    target.port = String(ports[port] || port);
    routes[name] = target.href;
  }
  for (const link of document.querySelectorAll("nav a[data-module-port]")) {
    const target = new URL(routes[names[link.dataset.modulePort]]);
    // Other module switchers already preserve this explicit Suite context.
    for (const [name, route] of Object.entries(routes)) target.searchParams.set(`suite-${name}`, route);
    link.href = target.href;
  }
}
// A custom-port instance must not briefly link to another default-port instance.
for (const link of document.querySelectorAll("nav a[data-module-port]")) {
  link.removeAttribute("href");
}
fetch("/api/team/module-links").then(response => {
  if (!response.ok) throw new Error("Suite module ports unavailable");
  return response.json();
}).then(updateModuleLinks).catch(() => {
  if (window.location.port === "8766") updateModuleLinks({});
});
