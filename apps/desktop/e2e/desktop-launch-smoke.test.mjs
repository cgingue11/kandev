import { chmod, mkdir, mkdtemp, readFile, readdir, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";

import {
  HEALTH_REQUESTED_TIMEOUT_MS,
  ROOT_REQUESTED_TIMEOUT_MS,
  waitForFile,
  writeFakeRuntime,
  writeReleaseShapedRuntime,
} from "./desktop-launch-smoke.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));
const backendRsPath = resolve(__dirname, "../src-tauri/src/backend.rs");
const mainRsPath = resolve(__dirname, "../src-tauri/src/main.rs");
const shellRsPath = resolve(__dirname, "../src-tauri/src/shell.rs");

async function withTempDir(run) {
  const dir = await mkdtemp(join(tmpdir(), "wait-for-file-"));
  try {
    return await run(dir);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

test("waitForFile resolves once the target file appears", async () => {
  await withTempDir(async (dir) => {
    const target = join(dir, "marker");
    const write = new Promise((r) => setTimeout(r, 50)).then(() => writeFile(target, "1"));
    await Promise.all([waitForFile(target, 2_000), write]);
  });
});

test("waitForFile throws a timeout error including the describeDetail() text", async () => {
  await withTempDir(async (dir) => {
    const target = join(dir, "never-appears");
    await assert.rejects(
      () => waitForFile(target, 100, undefined, () => "custom diagnostic detail"),
      (err) => {
        assert.match(err.message, /Timed out waiting for/);
        assert.match(err.message, /custom diagnostic detail/);
        return true;
      },
    );
  });
});

test("waitForFile omits the detail block when describeDetail is not provided", async () => {
  await withTempDir(async (dir) => {
    const target = join(dir, "never-appears");
    await assert.rejects(
      () => waitForFile(target, 100),
      (err) => {
        assert.equal(err.message, `Timed out waiting for ${target}`);
        return true;
      },
    );
  });
});

test("waitForFile calls tick on every poll and surfaces a tick failure immediately", async () => {
  await withTempDir(async (dir) => {
    const target = join(dir, "never-appears");
    let calls = 0;
    await assert.rejects(
      () =>
        waitForFile(target, 5_000, () => {
          calls += 1;
          if (calls >= 2) {
            throw new Error("boom");
          }
        }),
      /boom/,
    );
    assert.ok(calls >= 2, `expected at least 2 tick() calls, got ${calls}`);
  });
});

test("desktop smoke runtime uses standard resources without bundled remote helpers", async () => {
  await withTempDir(async (dir) => {
    const runtimeDir = join(dir, "runtime");
    const stateDir = join(dir, "state");
    await mkdir(join(runtimeDir, "bin"), { recursive: true });
    await mkdir(stateDir, { recursive: true });

    await writeFakeRuntime(runtimeDir, stateDir);

    const executables = (await readdir(join(runtimeDir, "bin"))).sort();
    assert.deepEqual(
      executables,
      process.platform === "win32" ? ["agentctl.cmd", "kandev.cmd"] : ["agentctl", "kandev"],
    );
    const manifest = JSON.parse(await readFile(join(runtimeDir, "remote-helpers.json"), "utf8"));
    assert.equal(manifest.variant, "standard");
    assert.equal(manifest.schema_version, 1);
  });
});

test("release-shaped Desktop runtime seeds the verified helper outside the standard bundle", async () => {
  await withTempDir(async (dir) => {
    const sourceBinDir = join(dir, "source-bin");
    const runtimeDir = join(dir, "runtime");
    const homeDir = join(dir, "home");
    await mkdir(sourceBinDir, { recursive: true });
    for (const name of [
      "kandev",
      "agentctl",
      "agentctl-linux-amd64",
      "agentctl-linux-arm64",
      "agentctl-darwin-amd64",
      "agentctl-darwin-arm64",
    ]) {
      await writeFile(join(sourceBinDir, name), `#!/bin/sh\n# ${name}\n`);
      await chmod(join(sourceBinDir, name), 0o755);
    }

    const runtime = await writeReleaseShapedRuntime({
      sourceBinDir,
      runtimeDir,
      homeDir,
      version: "1.2.3",
      commit: "a".repeat(40),
    });

    assert.deepEqual((await readdir(join(runtimeDir, "bin"))).sort(), ["agentctl", "kandev"]);
    assert.equal(runtime.manifest.variant, "standard");
    assert.equal(runtime.manifest.version, "1.2.3");
    assert.equal(runtime.manifest.commit, "a".repeat(40));
    const linuxHelper = runtime.manifest.helpers.find(
      (helper) => helper.platform === "linux/amd64",
    );
    assert.ok(linuxHelper);
    const cachedHelper = await readFile(runtime.cachePath);
    assert.equal(createHash("sha256").update(cachedHelper).digest("hex"), linuxHelper.sha256);
    assert.equal(cachedHelper.length, linuxHelper.size_bytes);
    assert.ok((await stat(runtime.cachePath)).mode & 0o111, "cached helper must be executable");
  });
});

test("health-requested timeout stays above the Rust backend's own HEALTH_TIMEOUT", async () => {
  const source = await readFile(backendRsPath, "utf8");
  const match = source.match(/const HEALTH_TIMEOUT: Duration = Duration::from_secs\((\d+)\);/);
  assert.ok(
    match,
    "could not find HEALTH_TIMEOUT in backend.rs — update this test if it moved or was renamed",
  );

  const rustHealthTimeoutMs = Number(match[1]) * 1_000;
  assert.ok(
    HEALTH_REQUESTED_TIMEOUT_MS > rustHealthTimeoutMs,
    `HEALTH_REQUESTED_TIMEOUT_MS (${HEALTH_REQUESTED_TIMEOUT_MS}ms) must exceed backend.rs's HEALTH_TIMEOUT ` +
      `(${rustHealthTimeoutMs}ms) — otherwise this smoke test can kill a launcher the Rust side would still ` +
      "consider healthy-pending, turning a legitimately slow CI run into a spurious failure.",
  );
  assert.ok(
    ROOT_REQUESTED_TIMEOUT_MS > 0 && Number.isInteger(ROOT_REQUESTED_TIMEOUT_MS),
    "ROOT_REQUESTED_TIMEOUT_MS must be a positive integer",
  );
});

test("Close Context owns Cmd/Ctrl+W without a native window-close fallback", async () => {
  const [mainSource, shellSource] = await Promise.all([
    readFile(mainRsPath, "utf8"),
    readFile(shellRsPath, "utf8"),
  ]);

  assert.match(mainSource, /MENU_CLOSE_CONTEXT, "Close Context"[\s\S]*CmdOrCtrl\+KeyW/);
  assert.match(
    shellSource,
    /MENU_CLOSE_CONTEXT\s*=>\s*Some\(MenuAction::Emit\(CLOSE_CONTEXT_EVENT\)\)/,
  );
  assert.doesNotMatch(mainSource, /PredefinedMenuItem::close_window/);
  assert.doesNotMatch(
    mainSource,
    /\.accelerator\("CmdOrCtrl\+KeyW"\)[\s\S]{0,160}shutdown_and_exit/,
  );
});

test("generic app activation never consumes a pending notification route", async () => {
  const mainSource = await readFile(mainRsPath, "utf8");

  assert.doesNotMatch(mainSource, /emit_pending_notification_route/);
  assert.match(mainSource, /tauri_plugin_single_instance::init\([\s\S]*activate_main_window/);
  assert.match(mainSource, /RunEvent::Reopen[\s\S]*activate_main_window/);
});

test("fullscreen uses the platform-native desktop accelerators", async () => {
  const mainSource = await readFile(mainRsPath, "utf8");

  assert.match(mainSource, /target_os = "macos"[\s\S]*"Ctrl\+Cmd\+F"/);
  assert.match(mainSource, /not\(target_os = "macos"\)[\s\S]*"F11"/);
});
