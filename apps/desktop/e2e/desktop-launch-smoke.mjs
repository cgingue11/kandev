#!/usr/bin/env node
import { createServer } from "node:http";
import { chmod, copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync, spawn } from "node:child_process";

const __dirname = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(__dirname, "..");
const repoRoot = resolve(desktopRoot, "../..");

// The Rust side (apps/desktop/src-tauri/src/backend.rs) does a two-stage wait before it
// navigates the webview: wait_for_backend polls GET /health every 250ms against a bounded
// HEALTH_TIMEOUT (60s), then wait_for_ready polls GET /ready every 250ms with NO timeout —
// it only gives up on child exit or shutdown. HEALTH_REQUESTED_TIMEOUT_MS must stay above
// HEALTH_TIMEOUT so the fake runtime doesn't get killed here before the Rust side even
// finishes the first stage; desktop-launch-smoke.test.mjs asserts that relationship so the
// two stay in sync. READY_REQUESTED_TIMEOUT_MS only bounds this test — the fake runtime
// answers /ready immediately once it's listening, so the real wait_for_ready being unbounded
// doesn't matter here.
export const HEALTH_REQUESTED_TIMEOUT_MS = 90_000;
export const READY_REQUESTED_TIMEOUT_MS = 60_000;
export const ROOT_REQUESTED_TIMEOUT_MS = 60_000;
export const RELEASE_DESKTOP_STARTUP_TIMEOUT_MS = 120_000;
export const DESKTOP_SMOKE_VERSION = "0.0.0-e2e";

const backendRoot = join(repoRoot, "apps/backend");
const remoteHelperBuilder = join(repoRoot, "scripts/release/remote-helper-assets.mjs");
const helperNames = [
  "agentctl-linux-amd64",
  "agentctl-linux-arm64",
  "agentctl-darwin-amd64",
  "agentctl-darwin-arm64",
];

// Only run the CLI behavior when this file is executed directly (`node desktop-launch-smoke.mjs`
// or the fake-runtime re-exec below) — not when desktop-launch-smoke.test.mjs imports it.
const isEntryPoint = resolve(process.argv[1] ?? "") === fileURLToPath(import.meta.url);
if (isEntryPoint) {
  if (process.argv[2] === "--fake-runtime") {
    await runFakeRuntime(process.argv[3], process.argv.slice(4));
  } else {
    await runSmoke();
  }
}

async function runSmoke() {
  const appBinary = resolve(desktopRoot, "src-tauri/target/release/kandev-desktop");
  if (!existsSync(appBinary)) {
    throw new Error(`Missing desktop binary at ${appBinary}; run pnpm build first`);
  }

  const tmp = await mkdtemp(join(tmpdir(), "kandev-desktop-e2e-"));
  const runtimeDir = join(tmp, "runtime");
  const stateDir = join(tmp, "state");
  await mkdir(join(runtimeDir, "bin"), { recursive: true });
  await mkdir(stateDir, { recursive: true });

  await writeFakeRuntime(runtimeDir, stateDir);

  const launchedViaXvfb = !process.env.DISPLAY && commandExists("xvfb-run");
  const command = launchedViaXvfb ? "xvfb-run" : appBinary;
  const args = launchedViaXvfb ? ["-a", appBinary] : [];
  const child = spawn(command, args, {
    cwd: repoRoot,
    detached: process.platform !== "win32",
    env: {
      ...process.env,
      KANDEV_DESKTOP_RUNTIME_DIR: runtimeDir,
      WEBKIT_DISABLE_COMPOSITING_MODE: "1",
      NO_AT_BRIDGE: "1",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });

  let stdout = "";
  let stderr = "";
  child.stdout?.on("data", (chunk) => {
    stdout += chunk;
  });
  child.stderr?.on("data", (chunk) => {
    stderr += chunk;
  });

  const failIfExited = () => {
    if (child.exitCode !== null) {
      throw new Error(`desktop app exited early with code ${child.exitCode}\n${stdout}\n${stderr}`);
    }
  };
  const describeChild = () => `[stdout]\n${stdout}\n[stderr]\n${stderr}`;

  try {
    await waitForFile(
      join(stateDir, "health-requested"),
      HEALTH_REQUESTED_TIMEOUT_MS,
      failIfExited,
      describeChild,
    );
    await waitForFile(
      join(stateDir, "ready-requested"),
      READY_REQUESTED_TIMEOUT_MS,
      failIfExited,
      describeChild,
    );
    await waitForFile(
      join(stateDir, "root-requested"),
      ROOT_REQUESTED_TIMEOUT_MS,
      failIfExited,
      describeChild,
    );
  } finally {
    await stopProcess(child);
  }

  console.log(
    "Desktop smoke passed: WebView requested / after backend health and readiness succeeded.",
  );

  await runReleaseShapedSmoke(appBinary);
}

async function runReleaseShapedSmoke(appBinary) {
  if (process.platform !== "linux") {
    console.log("Release-shaped Desktop launcher smoke runs on Linux only; skipping.");
    return;
  }

  const commit = execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: repoRoot,
    encoding: "utf8",
  }).trim();
  execFileSync(
    "make",
    [
      "build-kandev",
      "build-agentctl",
      "build-agentctl-remote",
      `VERSION=${DESKTOP_SMOKE_VERSION}`,
      `COMMIT=${commit}`,
    ],
    { cwd: backendRoot, stdio: "inherit" },
  );

  const tmp = await mkdtemp(join(tmpdir(), "kandev-desktop-release-e2e-"));
  const runtimeDir = join(tmp, "runtime");
  const homeDir = join(tmp, "home");
  const port = await findAvailablePort();
  const runtime = await writeReleaseShapedRuntime({
    sourceBinDir: join(backendRoot, "bin"),
    runtimeDir,
    homeDir,
    version: DESKTOP_SMOKE_VERSION,
    commit,
  });

  const launchedViaXvfb = !process.env.DISPLAY && commandExists("xvfb-run");
  const command = launchedViaXvfb ? "xvfb-run" : appBinary;
  const args = launchedViaXvfb ? ["-a", appBinary] : [];
  const child = spawn(command, args, {
    cwd: repoRoot,
    detached: true,
    env: {
      ...process.env,
      HOME: join(tmp, "user-home"),
      KANDEV_DESKTOP_RUNTIME_DIR: runtimeDir,
      KANDEV_DESKTOP_PORT: String(port),
      KANDEV_HOME_DIR: homeDir,
      KANDEV_E2E_MOCK: "true",
      KANDEV_INTERNAL_CONFIG_FILE: "",
      KANDEV_AGENTCTL_LINUX_AMD64_BINARY: "",
      KANDEV_AGENTCTL_LINUX_BINARY: "",
      WEBKIT_DISABLE_COMPOSITING_MODE: "1",
      NO_AT_BRIDGE: "1",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });

  let stdout = "";
  let stderr = "";
  child.stdout?.on("data", (chunk) => {
    stdout += chunk;
  });
  child.stderr?.on("data", (chunk) => {
    stderr += chunk;
  });
  const failIfExited = () => {
    if (child.exitCode !== null) {
      throw new Error(
        `release-shaped desktop app exited early with code ${child.exitCode}\n${stdout}\n${stderr}`,
      );
    }
  };
  const describeChild = () => `[stdout]\n${stdout}\n[stderr]\n${stderr}`;

  try {
    await waitForHttp(
      `http://127.0.0.1:${port}/ready`,
      RELEASE_DESKTOP_STARTUP_TIMEOUT_MS,
      failIfExited,
      describeChild,
    );
    await waitForHttp(
      `http://127.0.0.1:${port}/`,
      RELEASE_DESKTOP_STARTUP_TIMEOUT_MS,
      failIfExited,
      describeChild,
    );
    execFileSync(
      "go",
      [
        "test",
        "-count=1",
        "-run",
        "^TestAgentctlResolverPackagedDesktopBundleUsesPreseededCache$",
        "./internal/agent/runtime/lifecycle",
      ],
      {
        cwd: backendRoot,
        stdio: "inherit",
        env: {
          ...process.env,
          KANDEV_BUNDLE_DIR: runtimeDir,
          KANDEV_HOME_DIR: homeDir,
          KANDEV_AGENTCTL_LINUX_AMD64_BINARY: "",
          KANDEV_AGENTCTL_LINUX_BINARY: "",
          KANDEV_DESKTOP_SMOKE: "1",
          KANDEV_DESKTOP_SMOKE_BUNDLE_DIR: runtimeDir,
          KANDEV_DESKTOP_SMOKE_HOME_DIR: homeDir,
          KANDEV_DESKTOP_SMOKE_VERSION: DESKTOP_SMOKE_VERSION,
          KANDEV_DESKTOP_SMOKE_COMMIT: commit,
          KANDEV_INTERNAL_CONFIG_FILE: "",
        },
      },
    );
  } finally {
    await stopProcess(child);
    await rm(tmp, { recursive: true, force: true });
  }

  console.log(
    `Release-shaped Desktop smoke passed: the actual launcher served / from a standard bundle and the resolver selected the verified cached helper at ${runtime.cachePath}.`,
  );
}

export async function writeReleaseShapedRuntime({
  sourceBinDir,
  runtimeDir,
  homeDir,
  version,
  commit,
}) {
  const baseDir = dirname(runtimeDir);
  const bundleBinDir = join(runtimeDir, "bin");
  const helperInputDir = join(baseDir, "remote-helper-inputs");
  const helperArtifactDir = join(baseDir, "remote-helper-artifact");
  await mkdir(bundleBinDir, { recursive: true });
  await mkdir(helperInputDir, { recursive: true });

  for (const name of ["kandev", "agentctl"]) {
    const target = join(bundleBinDir, name);
    await copyFile(join(sourceBinDir, name), target);
    await chmod(target, 0o755);
  }
  for (const name of helperNames) {
    const target = join(helperInputDir, name);
    await copyFile(join(sourceBinDir, name), target);
    await chmod(target, 0o755);
  }

  execFileSync(
    process.execPath,
    [
      remoteHelperBuilder,
      "build",
      "--bin-dir",
      helperInputDir,
      "--output-dir",
      helperArtifactDir,
      "--version",
      version,
      "--commit",
      commit,
      "--stable",
      "true",
    ],
    { cwd: repoRoot, stdio: "inherit" },
  );

  const manifestPath = join(helperArtifactDir, "manifests", "standard", "remote-helpers.json");
  const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
  await copyFile(manifestPath, join(runtimeDir, "remote-helpers.json"));
  const helper = manifest.helpers.find((record) => record.platform === "linux/amd64");
  if (!helper) {
    throw new Error("Generated Desktop manifest is missing the linux/amd64 helper");
  }
  const cachePath = join(
    homeDir,
    "cache",
    "remote-helpers",
    version,
    "linux-amd64",
    helper.sha256,
    "agentctl",
  );
  await mkdir(dirname(cachePath), { recursive: true });
  await copyFile(join(helperInputDir, "agentctl-linux-amd64"), cachePath);
  await chmod(cachePath, 0o755);

  return { manifest, cachePath, runtimeDir, homeDir };
}

export async function writeFakeRuntime(runtimeDir, stateDir) {
  const fakeRuntime = join(
    runtimeDir,
    "bin",
    process.platform === "win32" ? "kandev.cmd" : "kandev",
  );
  const agentctl = join(
    runtimeDir,
    "bin",
    process.platform === "win32" ? "agentctl.cmd" : "agentctl",
  );

  if (process.platform === "win32") {
    await writeFile(
      fakeRuntime,
      `@echo off\r\nnode "${fileURLToPath(import.meta.url)}" --fake-runtime "${stateDir}" %*\r\n`,
    );
    await writeFile(agentctl, "@echo off\r\necho fake agentctl\r\n");
  } else {
    await writeFile(
      fakeRuntime,
      `#!/usr/bin/env bash\nexec node "${fileURLToPath(import.meta.url)}" --fake-runtime "${stateDir}" "$@"\n`,
    );
    await writeFile(agentctl, "#!/usr/bin/env bash\necho fake agentctl\n");
    await chmod(fakeRuntime, 0o755);
    await chmod(agentctl, 0o755);
  }

  const helpers = [
    ["linux/amd64", "agentctl-linux-amd64.gz"],
    ["linux/arm64", "agentctl-linux-arm64.gz"],
    ["darwin/amd64", "agentctl-darwin-amd64.gz"],
    ["darwin/arm64", "agentctl-darwin-arm64.gz"],
  ].map(([platform, asset]) => ({
    platform,
    asset,
    sha256: "0".repeat(64),
    size_bytes: 1,
  }));
  await writeFile(
    join(runtimeDir, "remote-helpers.json"),
    `${JSON.stringify(
      {
        schema_version: 1,
        version: "0.0.0",
        commit: "0".repeat(40),
        variant: "standard",
        helpers,
      },
      null,
      2,
    )}\n`,
  );
}

async function runFakeRuntime(stateDir, args) {
  const portIndex = args.indexOf("--port");
  const port = portIndex >= 0 ? Number(args[portIndex + 1]) : 0;
  const isHeadless = args.includes("--headless");

  if (!isHeadless || !Number.isInteger(port) || port <= 0) {
    await writeFile(join(stateDir, "invalid-args"), JSON.stringify(args));
    process.exit(2);
  }

  await writeFile(join(stateDir, "launched"), JSON.stringify({ args, port }));

  const server = createServer(async (req, res) => {
    if (req.url === "/health") {
      await writeFile(join(stateDir, "health-requested"), "1");
      const headers = { "content-type": "application/json" };
      if (process.env.KANDEV_DESKTOP_HEALTH_TOKEN) {
        headers["x-kandev-desktop-health-token"] = process.env.KANDEV_DESKTOP_HEALTH_TOKEN;
      }
      res.writeHead(200, headers);
      res.end('{"status":"ok"}');
      return;
    }

    if (req.url === "/ready") {
      await writeFile(join(stateDir, "ready-requested"), "1");
      res.writeHead(200, { "content-type": "application/json" });
      res.end('{"status":"ok"}');
      return;
    }

    if (req.url === "/") {
      await writeFile(join(stateDir, "root-requested"), "1");
      res.writeHead(200, { "content-type": "text/html" });
      res.end("<!doctype html><title>Kandev</title><main>Kandev desktop smoke</main>");
      return;
    }

    res.writeHead(404);
    res.end("not found");
  });

  await new Promise((resolveListen) => server.listen(port, "127.0.0.1", resolveListen));

  const stop = async () => {
    await writeFile(join(stateDir, "terminated"), "1");
    server.close(() => process.exit(0));
  };
  process.on("SIGTERM", stop);
  process.on("SIGINT", stop);
}

export async function waitForFile(path, timeoutMs, tick, describeDetail) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    tick?.();
    if (existsSync(path)) {
      return;
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 250));
  }
  const detail = describeDetail?.();
  throw new Error(`Timed out waiting for ${path}${detail ? `\n\n${detail}` : ""}`);
}

async function waitForHttp(url, timeoutMs, tick, describeDetail) {
  const deadline = Date.now() + timeoutMs;
  let lastError = "no response";
  while (Date.now() < deadline) {
    tick?.();
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(2_000) });
      if (response.ok) {
        return;
      }
      lastError = `HTTP ${response.status}`;
    } catch (error) {
      lastError = error.message;
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 250));
  }
  const detail = describeDetail?.();
  throw new Error(
    `Timed out waiting for ${url} to return success (last result: ${lastError})${detail ? `\n\n${detail}` : ""}`,
  );
}

async function findAvailablePort() {
  const server = createServer();
  await new Promise((resolveListen, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolveListen);
  });
  const address = server.address();
  if (!address || typeof address === "string") {
    throw new Error("Could not determine an available Desktop smoke port");
  }
  await new Promise((resolveClose, reject) => {
    server.close((error) => (error ? reject(error) : resolveClose()));
  });
  return address.port;
}

async function stopProcess(child) {
  if (child.exitCode !== null) {
    return;
  }
  if (process.platform === "win32") {
    child.kill();
  } else {
    process.kill(-child.pid, "SIGTERM");
  }
  await Promise.race([
    new Promise((resolveExit) => child.once("exit", resolveExit)),
    new Promise((resolveTimeout) => setTimeout(resolveTimeout, 5_000)),
  ]);
  if (child.exitCode === null) {
    if (process.platform === "win32") {
      child.kill("SIGKILL");
    } else {
      process.kill(-child.pid, "SIGKILL");
    }
  }
}

function commandExists(command) {
  const path = process.env.PATH ?? "";
  return path
    .split(delimiter)
    .filter(Boolean)
    .some((entry) => existsSync(join(entry, command)));
}
