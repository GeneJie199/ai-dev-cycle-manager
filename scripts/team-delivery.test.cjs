const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const { test } = require("node:test");
const { runInNewContext } = require("node:vm");

const script = readFileSync(join(__dirname, "../internal/web/static/team.js"), "utf8");

function element(tagName) {
  return {
    tagName,
    children: [],
    append(...children) { this.children.push(...children); },
    replaceChildren(...children) { this.children = children; },
    querySelectorAll() { return []; },
    addEventListener() {},
    scrollIntoView() {},
  };
}

async function openDelivery(state, executions) {
  const elements = new Map();
  const payload = {
    mission: { state, executions, requirementId: "requirement-fixture" },
    summary: "Synthetic mission",
    releaseStatus: "Not ready",
    changedFiles: [],
    completed: [],
    incomplete: [`explorer: ${state}`],
    risks: ["Mission has not completed"],
    report: { verificationRuns: null },
    previewStatus: "Unavailable",
    startGuidance: "Read the project README",
    diffs: {},
  };
  const document = {
    getElementById(id) {
      if (!elements.has(id)) elements.set(id, element("div"));
      return elements.get(id);
    },
    createElement: element,
    createTextNode: text => text,
    querySelectorAll: () => [],
  };
  async function fetch(path) {
    // Leave unrelated onboarding idle while exercising the page's Delivery handler.
    if (path === "/api/product/onboarding") return new Promise(() => {});
    assert.equal(path, "/api/product/missions/mission-fixture/delivery");
    return { ok: true, json: async () => payload };
  }
  const delivery = runInNewContext(script + "\ndelivery;", { document, fetch });
  await delivery("mission-fixture");
  assert.equal(elements.get("delivery").hidden, false);
  const content = elements.get("delivery-content").children;
  const download = content.find(node => node.href === "/api/product/missions/mission-fixture/delivery.zip");
  assert.equal(download?.tagName, "a");
  assert.equal(download.download, "");
  return content;
}

test("pending and escalated missions with no executions still expose Delivery", async () => {
  for (const state of ["pending", "escalated"]) {
    const content = await openDelivery(state, null);
    assert(content.some(node => node.textContent === "Not ready"));
    assert(!content.some(node => node.textContent?.includes("fixture/model")));
  }
});

test("Delivery still displays a recorded reviewer execution", async () => {
  const content = await openDelivery("completed", [
    { agent: "review", reviewResult: "passed", provider: "fixture", model: "model" },
  ]);
  assert(content.some(node => node.textContent?.includes("passed · fixture/model")));
});
