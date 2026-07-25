import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const limit = 78;
const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const skippedDirectories = new Set([".git", ".next", "node_modules", "target"]);

function markdownFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    if (entry.isDirectory()) {
      return skippedDirectories.has(entry.name)
        ? []
        : markdownFiles(join(directory, entry.name));
    }
    return entry.name.endsWith(".md") ? [join(directory, entry.name)] : [];
  });
}

function lineLength(line) {
  return Array.from(line).length;
}

function punctuationTail(content) {
  return (
    content.match(/^[,.;:!?%…，。；：！？、)\]}”’」』】》〉）]+/u)?.[0] ?? ""
  );
}

function firstBreakUnit(content) {
  const link = content.match(/^!?\[[^\]]*\]\([^)]*\)/u)?.[0];
  if (link) return link + punctuationTail(content.slice(link.length));
  const strong =
    content.match(/^\*\*[^*]+\*\*/u)?.[0] ??
    content.match(/^__[^_]+__/u)?.[0];
  if (strong) return strong + punctuationTail(content.slice(strong.length));
  const emphasis =
    content.match(/^\*[^*]+\*/u)?.[0] ?? content.match(/^_[^_]+_/u)?.[0];
  if (emphasis) {
    return emphasis + punctuationTail(content.slice(emphasis.length));
  }
  const ticks = content.match(/^`+/u)?.[0];
  if (ticks) {
    const end = content.indexOf(ticks, ticks.length);
    if (end >= 0) {
      const close = end + ticks.length;
      return (
        content.slice(0, close) + punctuationTail(content.slice(close))
      );
    }
  }
  const opener = content.match(/^[({“‘「『【《〈（]+/u)?.[0];
  if (opener) {
    const rest = content.slice(opener.length);
    const unit = firstBreakUnit(rest);
    const closer = punctuationTail(rest.slice(unit.length));
    return opener + unit + closer;
  }
  return (
    content.match(
      /^[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]/u,
    )?.[0] ??
    content.match(
      /^[^\s\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]+/u,
    )?.[0] ??
    ""
  );
}

function breakSeparator(previous, unit) {
  if (previous.endsWith("-")) return "";
  if (
    /^[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]/u.test(
      unit,
    )
  ) {
    return /[A-Za-z0-9`)\]]$/u.test(previous) ? " " : "";
  }
  return /^[,.;:!?%…，。；：！？、)\]}”’「『【《〈（」』】》〉）]/u.test(unit)
    ? ""
    : " ";
}

function proseLine(line) {
  let content = line.trimStart();
  let quoteDepth = 0;
  while (content.startsWith(">")) {
    quoteDepth += 1;
    content = content.slice(1).trimStart();
  }
  const listItem = /^(?:[-*+]|\d+[.)])\s+/u.test(content);
  const startsItem = listItem || /^\*\*[^*]+:\*\*/u.test(content);
  if (listItem) {
    content = content.replace(/^(?:[-*+]|\d+[.)])\s+/u, "");
  }
  return { content, line, quoteDepth, startsItem };
}

function standaloneLine(line) {
  return (
    /^#{1,6}\s/u.test(line) ||
    /^(?:-{3,}|\*{3,}|_{3,})$/u.test(line) ||
    /^\*\*[^*]+\*\*$/u.test(line) ||
    /^!\[[^\]]*\]\([^)]*\)$/u.test(line) ||
    /^\[[^\]]+\]:\s/u.test(line)
  );
}

function unbreakableLine(line) {
  let content = line
    .trim()
    .replace(/^(?:>\s*)?(?:(?:[-*+]|\d+[.)])\s+)?/u, "");
  content = content
    .replace(/\[!\[[^\]]*\]\([^)]*\)\]\([^)]*\)/gu, "")
    .replace(/!?\[[^\]]*\]\([^)]*\)/gu, "")
    .replace(/`[^`]+`/gu, "")
    .trim();
  return (
    !content ||
    (!/\s/u.test(content) &&
      !/[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}]/u.test(
        content,
      ))
  );
}

function checkText(text) {
  const failures = [];
  let fence = "";
  let frontmatter = false;
  let htmlTag = false;
  let markdownLink = false;
  let previous = null;
  for (const [index, line] of text.split(/\r?\n/u).entries()) {
    const trimmed = line.trim();
    if (index === 0 && trimmed === "---") {
      frontmatter = true;
      continue;
    }
    if (frontmatter) {
      if (trimmed === "---") frontmatter = false;
      continue;
    }
    if (markdownLink) {
      if (trimmed.includes(")")) markdownLink = false;
      previous = null;
      continue;
    }
    if (/^!?\[[^\]]*$/u.test(trimmed)) {
      markdownLink = true;
      previous = null;
      continue;
    }
    const marker = trimmed.match(/^(`{3,}|~{3,})/u)?.[1] ?? "";
    if (fence) {
      if (marker.startsWith(fence[0])) fence = "";
      previous = null;
      continue;
    }
    if (marker) {
      fence = marker;
      previous = null;
      continue;
    }
    if (htmlTag) {
      if (trimmed.includes(">")) htmlTag = false;
      previous = null;
      continue;
    }
    if (trimmed.startsWith("<")) {
      htmlTag = !trimmed.includes(">");
      previous = null;
      continue;
    }
    if (!trimmed || trimmed.startsWith("|")) {
      previous = null;
      continue;
    }
    const length = lineLength(line);
    if (length > limit && !unbreakableLine(trimmed)) {
      failures.push({
        line: index + 1,
        message: `${length} source characters`,
      });
    }
    if (standaloneLine(trimmed)) {
      previous = null;
      continue;
    }
    const current = proseLine(line);
    if (
      previous &&
      !current.startsItem &&
      current.quoteDepth === previous.quoteDepth &&
      !previous.line.endsWith("  ") &&
      !previous.line.endsWith("\\")
    ) {
      const unit = firstBreakUnit(current.content);
      const separator = breakSeparator(previous.line, unit);
      const filled =
        lineLength(previous.line) + separator.length + lineLength(unit);
      if (unit && filled <= limit) {
        failures.push({
          line: index,
          message: `premature wrap; next unit fits at ${filled}/${limit}`,
        });
      }
    }
    previous = length > limit && unbreakableLine(trimmed) ? null : current;
  }
  return failures;
}

assert.deepEqual(checkText("a".repeat(79)), []);
assert.deepEqual(checkText(`word ${"a".repeat(74)}`), [
  { line: 1, message: "79 source characters" },
]);
assert.deepEqual(checkText(`| ${"a".repeat(79)} |`), []);
assert.deepEqual(checkText(`\`\`\`\n${"a ".repeat(50)}\n\`\`\``), []);
assert.deepEqual(checkText(`- [long label](${"a".repeat(79)})`), []);
assert.deepEqual(checkText("中".repeat(79)), [
  { line: 1, message: "79 source characters" },
]);
assert.deepEqual(checkText(`中文 ${"中".repeat(76)}`), [
  { line: 1, message: "79 source characters" },
]);
assert.deepEqual(checkText(`${"word ".repeat(13)}one\ntwo`), [
  { line: 1, message: "premature wrap; next unit fits at 72/78" },
]);
assert.deepEqual(checkText(`${"word ".repeat(15)}one\ntwo`), []);
assert.deepEqual(checkText(`${"中".repeat(77)}\n文`), [
  { line: 1, message: "premature wrap; next unit fits at 78/78" },
]);
assert.deepEqual(checkText(`${"中".repeat(78)}\n文`), []);
assert.deepEqual(checkText("- short\n- next"), []);
assert.deepEqual(checkText(`${"word ".repeat(13)}\n**next**`), []);
assert.deepEqual(checkText("short\n[link](target)"), [
  { line: 1, message: "premature wrap; next unit fits at 20/78" },
]);
assert.deepEqual(checkText(`${"中".repeat(74)}AI\n文`), [
  { line: 1, message: "premature wrap; next unit fits at 78/78" },
]);
assert.deepEqual(checkText("- [ ] short\n      (`path`)"), [
  { line: 1, message: "premature wrap; next unit fits at 20/78" },
]);
assert.deepEqual(checkText("---\nname: Test\nabout: Test\n---"), []);
assert.deepEqual(checkText("![long alt\ntext](image.png)"), []);
assert.deepEqual(checkText("**Prompt**\nAnswer here."), []);

const paths = markdownFiles(root).filter(
  (path) => !path.endsWith("THIRD_PARTY_NOTICES.md"),
);

const failures = paths
  .flatMap((path) =>
    checkText(readFileSync(path, "utf8")).map((failure) => ({
      path: relative(root, path),
      ...failure,
    })),
  );

for (const failure of failures) {
  console.error(`${failure.path}:${failure.line}: ${failure.message}`);
}
if (failures.length) process.exit(1);
