// Writes a consumer-ready package.json into dist/.
//
// By default the output has no devDependencies or optionalDependencies, so
// local out-of-tree projects can `bun add ./path/to/bindings/nodejs/dist`
// without pulling in build tooling or unpublished platform packages.
//
// Set CC_NPM_PUBLISH=1 to include the optionalDependencies (platform-specific
// cc-helper packages) for npm publishing.

import { readFileSync, writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = join(__dirname, "..");
const pkg = JSON.parse(readFileSync(join(root, "package.json"), "utf-8"));

const dist = {
  name: pkg.name,
  version: pkg.version,
  description: pkg.description,
  repository: pkg.repository,
  publishConfig: pkg.publishConfig,
  license: pkg.license,
  type: pkg.type,
  main: "./index.cjs",
  module: "./index.mjs",
  types: "./index.d.mts",
  exports: {
    ".": {
      import: {
        types: "./index.d.mts",
        default: "./index.mjs",
      },
      require: {
        types: "./index.d.cts",
        default: "./index.cjs",
      },
    },
  },
  keywords: pkg.keywords,
  engines: pkg.engines,
};

if (process.env.CC_NPM_PUBLISH) {
  dist.optionalDependencies = pkg.optionalDependencies;
}

writeFileSync(join(root, "dist", "package.json"), JSON.stringify(dist, null, 2) + "\n");
console.log(`wrote dist/package.json${process.env.CC_NPM_PUBLISH ? " (with optionalDependencies)" : ""}`);
