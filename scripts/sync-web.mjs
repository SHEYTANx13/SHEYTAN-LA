import { cp, mkdir, readdir, rm } from "node:fs/promises";
import { resolve } from "node:path";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const projectRoot = resolve(scriptDir, "..");

const sourceDir = resolve(projectRoot, "dist");
const stagingDir = resolve(projectRoot, "web", "static");

async function main() {
  await mkdir(sourceDir, { recursive: true });

  const sourceEntries = await readdir(sourceDir, {
    withFileTypes: true,
  });

  if (sourceEntries.length === 0) {
    throw new Error(
      "Vite produced an empty dist directory. Run the frontend build first.",
    );
  }

  await rm(stagingDir, {
    recursive: true,
    force: true,
  });

  await mkdir(stagingDir, {
    recursive: true,
  });

  await cp(sourceDir, stagingDir, {
    recursive: true,
    force: true,
  });

  console.log(`Node/Vite frontend embedded at: ${stagingDir}`);
}

await main();
