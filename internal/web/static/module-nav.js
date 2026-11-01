"use strict";
// Keep module navigation on the selected host, including loopback aliases and IPv6.
function updateModuleLinks(ports) {
for (const link of document.querySelectorAll("nav a[data-module-port]")) {
  const target = new URL(window.location.href);
  target.port = String(ports[link.dataset.modulePort] || link.dataset.modulePort);
  target.pathname = "/";
  target.search = "";
  target.hash = "";
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
