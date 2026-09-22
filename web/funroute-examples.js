const EXAMPLES_URL = "/funroute-examples.json";

// Examples are data rather than UI code so the Go test suite can compile and
// run the exact programs the browser presents. This loader stays DOM-free and
// is reusable by another console built on funroute-core.js.
export async function loadExamples(request = fetch) {
  const response = await request(EXAMPLES_URL);
  if (!response.ok) throw new Error(`示例清单加载失败（HTTP ${response.status}）`);
  const manifest = await response.json();
  if (manifest.version !== 1 || !Array.isArray(manifest.examples) || !manifest.examples.length) {
    throw new Error("示例清单格式无效");
  }
  return manifest.examples;
}
