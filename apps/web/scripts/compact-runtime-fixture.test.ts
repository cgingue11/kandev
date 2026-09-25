import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { prepareCompactRuntimeFixture } from "../e2e/fixtures/compact-runtime";

const roots: string[] = [];

afterEach(() => {
  for (const root of roots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
});

describe("prepareCompactRuntimeFixture", () => {
  it("creates a manifest-only bundle and seeds its digest-verified Linux helper cache", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "kandev-compact-runtime-test-"));
    roots.push(root);
    const sourceBinDir = path.join(root, "source-bin");
    fs.mkdirSync(sourceBinDir);
    for (const name of ["kandev", "agentctl", "agentctl-linux-amd64"]) {
      fs.writeFileSync(path.join(sourceBinDir, name), `${name} test fixture\n`, {
        mode: 0o755,
      });
    }

    const result = prepareCompactRuntimeFixture({
      bundleDir: path.join(root, "bundle"),
      homeDir: path.join(root, "home"),
      sourceBinDir,
      identity: { version: "v1.2.3", commit: "a".repeat(40) },
    });

    expect(fs.readdirSync(path.join(result.bundleDir, "bin")).sort()).toEqual([
      "agentctl",
      "kandev",
    ]);
    const manifest = JSON.parse(
      fs.readFileSync(path.join(result.bundleDir, "remote-helpers.json"), "utf8"),
    ) as {
      variant: string;
      version: string;
      commit: string;
      helpers: Array<{ platform: string; sha256: string }>;
    };
    expect(manifest).toMatchObject({
      variant: "standard",
      version: "v1.2.3",
      commit: "a".repeat(40),
    });
    const linuxRecord = manifest.helpers.find((helper) => helper.platform === "linux/amd64");
    expect(linuxRecord).toBeDefined();
    const cachedBytes = fs.readFileSync(result.cachePath);
    expect(createHash("sha256").update(cachedBytes).digest("hex")).toBe(linuxRecord?.sha256);
    expect(fs.statSync(result.cachePath).mode & 0o111).not.toBe(0);
  });

  it("uses a SemVer-shaped test version when the checkout has no release tag", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "kandev-compact-runtime-identity-"));
    roots.push(root);
    const sourceBinDir = path.join(root, "source-bin");
    fs.mkdirSync(sourceBinDir);
    for (const name of ["kandev", "agentctl", "agentctl-linux-amd64"]) {
      fs.writeFileSync(path.join(sourceBinDir, name), `${name} test fixture\n`, {
        mode: 0o755,
      });
    }

    const result = prepareCompactRuntimeFixture({
      bundleDir: path.join(root, "bundle"),
      homeDir: path.join(root, "home"),
      sourceBinDir,
    });

    expect(result.version).toMatch(/^0\.0\.0-e2e\.[a-f0-9]{12}$/);
    expect(result.commit).toMatch(/^[a-f0-9]{40}$/);
    const manifest = JSON.parse(
      fs.readFileSync(path.join(result.bundleDir, "remote-helpers.json"), "utf8"),
    ) as { version: string; commit: string };
    expect(manifest).toMatchObject({ version: result.version, commit: result.commit });
  });

  it("fails when the Linux remote helper was not built", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "kandev-compact-runtime-missing-"));
    roots.push(root);
    const sourceBinDir = path.join(root, "source-bin");
    fs.mkdirSync(sourceBinDir);
    for (const name of ["kandev", "agentctl"]) {
      fs.writeFileSync(path.join(sourceBinDir, name), `${name} test fixture\n`, {
        mode: 0o755,
      });
    }

    expect(() =>
      prepareCompactRuntimeFixture({
        bundleDir: path.join(root, "bundle"),
        homeDir: path.join(root, "home"),
        sourceBinDir,
        identity: { version: "v1.2.3", commit: "a".repeat(40) },
      }),
    ).toThrow(/agentctl-linux-amd64/);
  });
});
