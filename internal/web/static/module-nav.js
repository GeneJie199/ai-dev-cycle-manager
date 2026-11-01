"use strict";
// Keep module navigation on the selected host, including loopback aliases and IPv6.
for (const link of document.querySelectorAll("nav a[data-module-port]")) {
  const target = new URL(window.location.href);
  target.port = link.dataset.modulePort;
  target.pathname = "/";
  target.search = "";
  target.hash = "";
  link.href = target.href;
}
