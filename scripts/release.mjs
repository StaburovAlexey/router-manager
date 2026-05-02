#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
process.chdir(repoRoot);

const args = process.argv.slice(2);
const dryRun = args.includes("--dry-run");
const versionArg = args.find((arg) => !arg.startsWith("--"));
const tagPattern = /^v(\d+)\.(\d+)\.(\d+)$/;
let createdTag = "";

function fail(message) {
  throw new Error(message);
}

function run(command, commandArgs, options = {}) {
  const result = spawnSync(command, commandArgs, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: options.capture ? ["ignore", "pipe", "pipe"] : "inherit"
  });
  if (result.status !== 0) {
    const details = options.capture ? `\n${result.stderr || result.stdout}`.trimEnd() : "";
    fail(`${command} ${commandArgs.join(" ")} failed${details ? `: ${details}` : ""}`);
  }
  return options.capture ? result.stdout.trim() : "";
}

function tryRun(command, commandArgs) {
  return spawnSync(command, commandArgs, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"]
  });
}

function parseTag(tag) {
  const match = tagPattern.exec(tag);
  if (!match) {
    return null;
  }
  return {
    tag,
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3])
  };
}

function compareTags(a, b) {
  return a.major - b.major || a.minor - b.minor || a.patch - b.patch;
}

function semverTags(tags) {
  return tags.map(parseTag).filter(Boolean).sort(compareTags);
}

function nextPatch(tag) {
  return `v${tag.major}.${tag.minor}.${tag.patch + 1}`;
}

function listTags(commandArgs) {
  const out = run("git", commandArgs, { capture: true });
  return out ? out.split(/\r?\n/).filter(Boolean) : [];
}

function tagCommit(tag) {
  return run("git", ["rev-list", "-n", "1", tag], { capture: true });
}

function remoteTagExists(tag) {
  const result = tryRun("git", ["ls-remote", "--exit-code", "--tags", "origin", tag]);
  if (result.status === 0) {
    return true;
  }
  if (result.status === 2) {
    return false;
  }
  fail(`git ls-remote failed: ${(result.stderr || result.stdout).trim()}`);
}

function githubURL(remote) {
  const clean = remote.trim().replace(/\.git$/, "");
  if (clean.startsWith("git@github.com:")) {
    return `https://github.com/${clean.slice("git@github.com:".length)}`;
  }
  return clean;
}

function resolveVersion() {
  const headTags = semverTags(listTags(["tag", "--points-at", "HEAD", "--list", "v[0-9]*.[0-9]*.[0-9]*"]));
  const allTags = semverTags(listTags(["tag", "--list", "v[0-9]*.[0-9]*.[0-9]*"]));

  if (versionArg) {
    if (!tagPattern.test(versionArg)) {
      fail(`Version must look like v0.1.5, got ${versionArg}`);
    }
    return versionArg;
  }

  if (headTags.length > 0) {
    return headTags.at(-1).tag;
  }
  if (allTags.length === 0) {
    return "v0.1.0";
  }
  return nextPatch(allTags.at(-1));
}

try {
  run("git", ["fetch", "origin", "main", "--tags"]);

  const branch = run("git", ["rev-parse", "--abbrev-ref", "HEAD"], { capture: true });
  if (branch !== "main") {
    fail(`Release must run from main, current branch is ${branch}`);
  }

  const status = run("git", ["status", "--porcelain"], { capture: true });
  if (status !== "") {
    fail(`Working tree must be clean before release:\n${status}`);
  }

  const ancestor = tryRun("git", ["merge-base", "--is-ancestor", "origin/main", "HEAD"]);
  if (ancestor.status !== 0) {
    fail("origin/main is not an ancestor of HEAD. Pull/rebase remote changes before releasing.");
  }

  const version = resolveVersion();
  const head = run("git", ["rev-parse", "HEAD"], { capture: true });
  const localTags = listTags(["tag", "--list", version]);
  const hasLocalTag = localTags.includes(version);

  if (remoteTagExists(version)) {
    fail(`Remote tag ${version} already exists. Choose a newer version.`);
  }
  if (hasLocalTag && tagCommit(version) !== head) {
    fail(`Local tag ${version} does not point at HEAD.`);
  }

  console.log(`Release version: ${version}${dryRun ? " (dry run)" : ""}`);
  run("go", ["test", "./..."]);
  run("make", ["clean", "build-all", `VERSION=${version}`]);

  if (!hasLocalTag) {
    if (dryRun) {
      console.log(`Would create tag ${version}`);
    } else {
      run("git", ["tag", "-a", version, "-m", version]);
      createdTag = version;
    }
  } else {
    console.log(`Using existing local tag ${version} at HEAD.`);
  }

  if (dryRun) {
    console.log(`Would push: git push origin HEAD:main ${version}`);
    process.exit(0);
  }

  run("git", ["push", "origin", "HEAD:main", version]);

  const remote = run("git", ["remote", "get-url", "origin"], { capture: true });
  const repoURL = githubURL(remote);
  console.log(`Release push complete: ${repoURL}/releases/tag/${version}`);
  console.log(`Workflow runs: ${repoURL}/actions/workflows/release.yml`);
} catch (error) {
  console.error(error.message);
  if (createdTag) {
    console.error(`Tag ${createdTag} was created locally. After fixing the problem, rerun npm run release or push it with:`);
    console.error(`git push origin HEAD:main ${createdTag}`);
  }
  process.exit(1);
}
