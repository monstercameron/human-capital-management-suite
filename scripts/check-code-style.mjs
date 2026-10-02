#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import ts from "typescript";

const sourceExtensions = new Set([".cjs", ".js", ".jsx", ".mjs", ".ts", ".tsx"]);
const ignoredPathSegments = new Set([
  ".git",
  ".husky",
  "coverage",
  "dist",
  "node_modules",
]);

// .artifacts is the repository's disposable output root. Only that exact
// root is ignored; a source directory named .artifacts remains production
// code and must still be checked.
const repositoryArtifactsRoot = path.resolve(".artifacts");

const allowedTryCatchFiles = new Set([
  // Vendored Go toolchain shim copied verbatim from $(go env GOROOT)/lib/wasm.
  "internal/humanwork/workspace/assets/wasm_exec.js",
  // Planning design demo and its test harness; not application code.
  "planning/design/demo/workspace.js",
  "planning/design/demo/workspace.test.cjs",
  // Playwright browser specs: test harness code whose try blocks release
  // browser contexts or attach page state to a failed expectation.
  "tools/uxqual/browser/uxblind057_popovers.spec.mjs",
  "tools/uxqual/browser/uxblind089_cold_load.spec.mjs",
  "tools/uxqual/browser/uxblind098_people_filter.spec.mjs",
  "tools/uxqual/browser/uxblind122_agents_task_reload.spec.mjs",
  "tools/uxqual/browser/uxblind125_header_responsive.spec.mjs",
  // Repository policy script; the catch is its own reporting boundary.
  "scripts/check-race-policy.mjs",
  "src/platform/client-device-state/device-state.ts",
  "src/platform/data-store/client/transaction.ts",
  "src/platform/foundation/result/from-promise.ts",
  "src/platform/foundation/result/from-throwable.ts",
]);

const allowedProcessEnvFiles = new Set([
  "tools/uxqual/browser/playwright.config.mjs",
  "tools/uxqual/persona_admin_browser/playwright.config.mjs",
  "src/api/dependencies.ts",
  "src/api/main.ts",
  "src/platform/data-store/client/database-client.ts",
  "src/third-party-apis/compensation-decision/main.ts",
  "src/third-party-apis/compensation-market/main.ts",
]);

const allowedFetchFiles = new Set([
  "src/api/compensation-decision-client.ts",
  "src/api/executor-client.ts",
  "src/platform/foundation/executor/executor-client.ts",
  "src/platform/workflow-runtime/executor/executor-client.ts",
  "src/tests/support/workflow-api-client.ts",
]);

const allowedDomainLiteralFiles = new Set([
  "src/platform/data-store/types.ts",
  "src/platform/ui-contracts/widgets.ts",
  "src/platform/ui-runtime/pages.ts",
  "src/platform/workflow-runtime/domain.ts",
  "src/workflows/shared/workflow-config.ts",
  "src/workflows/shared/workflow-config-validation.ts",
]);

const restrictedDomainLiterals = new Map([
  [
    "approved",
    "Use a typed constant such as APPROVAL_TASK_STATUSES.APPROVED, CHANGE_REQUEST_STATUSES.APPROVED, or WORKFLOW_STATES.APPROVED.",
  ],
  [
    "rejected",
    "Use a typed constant such as APPROVAL_TASK_STATUSES.REJECTED, CHANGE_REQUEST_STATUSES.REJECTED, or WORKFLOW_STATES.REJECTED.",
  ],
  [
    "executed",
    "Use a typed constant such as CHANGE_REQUEST_STATUSES.EXECUTED or WORKFLOW_STATES.EXECUTED.",
  ],
  ["approve", "Use WORKFLOW_TRANSITIONS.APPROVE."],
  ["reject", "Use WORKFLOW_TRANSITIONS.REJECT."],
  ["execute", "Use WORKFLOW_TRANSITIONS.EXECUTE."],
  ["request_more_info", "Use WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO."],
  ["submit_input", "Use WORKFLOW_TRANSITIONS.SUBMIT_INPUT."],
  ["waiting_approval", "Use WORKFLOW_STATES.WAITING_APPROVAL."],
  ["hr_admin", "Use ACTOR_ROLES.HR_ADMIN."],
  ["finance_admin", "Use ACTOR_ROLES.FINANCE_ADMIN."],
  ["compensation_admin", "Use ACTOR_ROLES.COMPENSATION_ADMIN."],
  ["clinic_ops_admin", "Use ACTOR_ROLES.CLINIC_OPS_ADMIN."],
  ["system", "Use ACTOR_ROLES.SYSTEM or ACTOR_TYPES.SYSTEM."],
]);

const normalizePath = (filePath) =>
  filePath.replaceAll("\\", "/").replace(/^\.\//u, "");

const isSourceFile = (filePath) => sourceExtensions.has(path.extname(filePath));

const isIgnoredPath = (filePath) =>
  normalizePath(filePath)
    .split("/")
    .some((segment) => ignoredPathSegments.has(segment)) ||
  (() => {
    const resolved = path.resolve(filePath);
    const relative = path.relative(repositoryArtifactsRoot, resolved);
    return (
      relative === "" ||
      (relative !== ".." &&
        !relative.startsWith(`..${path.sep}`) &&
        !path.isAbsolute(relative))
    );
  })();

const scriptKindForPath = (filePath) => {
  switch (path.extname(filePath)) {
    case ".js":
    case ".mjs":
    case ".cjs":
      return ts.ScriptKind.JS;
    case ".jsx":
      return ts.ScriptKind.JSX;
    case ".tsx":
      return ts.ScriptKind.TSX;
    default:
      return ts.ScriptKind.TS;
  }
};

const locationForNode = (sourceFile, node) => {
  const position = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile));

  return {
    column: position.character + 1,
    line: position.line + 1,
  };
};

const gitFiles = (args) =>
  execFileSync("git", args, { encoding: "utf8" })
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter(Boolean);

const stagedFiles = () =>
  gitFiles(["diff", "--cached", "--name-only", "--diff-filter=ACMR"]);

const workspaceFiles = (directory = ".") =>
  readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const filePath = normalizePath(path.join(directory, entry.name));

    if (entry.isDirectory()) {
      return isIgnoredPath(filePath) ? [] : workspaceFiles(filePath);
    }

    return entry.isFile() ? [filePath] : [];
  });

const candidateFiles = (args) => {
  const shouldCheckStagedFiles = args.includes("--staged");
  const explicitFiles = args.filter((arg) => !arg.startsWith("--"));
  const files =
    explicitFiles.length > 0
      ? explicitFiles
      : shouldCheckStagedFiles
        ? stagedFiles()
        : workspaceFiles();

  return [...new Set(files.map(normalizePath))]
    .filter(isSourceFile)
    .filter((filePath) => !isIgnoredPath(filePath))
    .filter((filePath) => existsSync(filePath));
};

const sourceFileForPath = (filePath) => {
  const sourceText = readFileSync(filePath, "utf8");

  return ts.createSourceFile(
    filePath,
    sourceText,
    ts.ScriptTarget.Latest,
    true,
    scriptKindForPath(filePath),
  );
};

const isTestFile = (filePath) => {
  const normalizedFilePath = normalizePath(filePath);

  return (
    normalizedFilePath.includes("/tests/") ||
    /\.test\.[cm]?[jt]sx?$/u.test(normalizedFilePath) ||
    /\.spec\.[cm]?[jt]sx?$/u.test(normalizedFilePath)
  );
};

const isAllowedProcessEnvFile = (filePath) =>
  allowedProcessEnvFiles.has(filePath) || filePath.startsWith("scripts/");

const isAllowedFetchFile = (filePath) =>
  allowedFetchFiles.has(filePath) || isTestFile(filePath);

const isAllowedSqlFile = (filePath) =>
  isTestFile(filePath) || filePath.startsWith("src/platform/data-store/");

const isWorkflowOrPlatformLayer = (filePath) =>
  filePath.startsWith("src/workflows/") || filePath.startsWith("src/platform/");

const isCheckedDomainLiteralFile = (filePath) =>
  (filePath.startsWith("src/workflows/") ||
    filePath.startsWith("src/platform/workflow-runtime/")) &&
  !isTestFile(filePath) &&
  !filePath.includes("/constants") &&
  !allowedDomainLiteralFiles.has(filePath);

const isProcessEnvExpression = (node) =>
  ts.isPropertyAccessExpression(node) &&
  ts.isIdentifier(node.expression) &&
  node.expression.text === "process" &&
  node.name.text === "env";

const isProcessEnvAccess = (node) => {
  if (ts.isPropertyAccessExpression(node) && isProcessEnvExpression(node.expression)) {
    return true;
  }

  return ts.isElementAccessExpression(node) && isProcessEnvExpression(node.expression);
};

const isDirectFetchCall = (node) =>
  ts.isCallExpression(node) &&
  ts.isIdentifier(node.expression) &&
  node.expression.text === "fetch";

const isQueryCall = (node) =>
  ts.isCallExpression(node) &&
  ts.isPropertyAccessExpression(node.expression) &&
  node.expression.name.text === "query";

const moduleSpecifierText = (node) => {
  if (
    !("moduleSpecifier" in node) ||
    node.moduleSpecifier === undefined ||
    !ts.isStringLiteral(node.moduleSpecifier)
  ) {
    return undefined;
  }

  return node.moduleSpecifier.text;
};

const resolvedImportTarget = (filePath, specifier) => {
  if (!specifier.startsWith(".")) {
    return specifier;
  }

  return normalizePath(path.join(path.dirname(filePath), specifier));
};

const targetsForbiddenLayer = (filePath, specifier) => {
  if (!isWorkflowOrPlatformLayer(filePath)) {
    return false;
  }

  const target = resolvedImportTarget(filePath, specifier);

  return (
    target === "@human-capital-management-suite/api" ||
    target.startsWith("@human-capital-management-suite/api/") ||
    target.startsWith("src/api/")
  );
};

const isStringLiteralExpression = (node) =>
  ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node);

const isTypeOnlyLiteral = (node) => {
  let currentNode = node.parent;

  while (currentNode !== undefined) {
    if (
      ts.isLiteralTypeNode(currentNode) ||
      ts.isTypeAliasDeclaration(currentNode) ||
      ts.isInterfaceDeclaration(currentNode)
    ) {
      return true;
    }

    if (
      ts.isExpressionStatement(currentNode) ||
      ts.isCallExpression(currentNode) ||
      ts.isVariableDeclaration(currentNode) ||
      ts.isPropertyAssignment(currentNode) ||
      ts.isBinaryExpression(currentNode) ||
      ts.isReturnStatement(currentNode) ||
      ts.isArrayLiteralExpression(currentNode)
    ) {
      return false;
    }

    currentNode = currentNode.parent;
  }

  return false;
};

const isImportOrExportLiteral = (node) =>
  ts.isImportDeclaration(node.parent) ||
  ts.isExportDeclaration(node.parent) ||
  ts.isExternalModuleReference(node.parent);

const violationsForSourceFile = (filePath, sourceFile) => {
  const locations = [];

  const visitNode = (node) => {
    if (ts.isTryStatement(node)) {
      locations.push({
        ...locationForNode(sourceFile, node),
        message:
          "Broad try/catch is not allowed in application code. Use Result wrappers at approved system boundaries.",
        rule: "try-catch-boundary",
      });
    }

    if (
      isProcessEnvAccess(node) &&
      !isAllowedProcessEnvFile(filePath) &&
      !(isProcessEnvExpression(node) && ts.isElementAccessExpression(node.parent))
    ) {
      locations.push({
        ...locationForNode(sourceFile, node),
        message:
          "Read environment variables in approved startup/config modules, not scattered application code.",
        rule: "process-env-boundary",
      });
    }

    if (isDirectFetchCall(node) && !isAllowedFetchFile(filePath)) {
      locations.push({
        ...locationForNode(sourceFile, node),
        message:
          "Direct fetch calls belong in approved HTTP/API client modules or tests.",
        rule: "external-call-boundary",
      });
    }

    if (isQueryCall(node) && !isAllowedSqlFile(filePath)) {
      locations.push({
        ...locationForNode(sourceFile, node),
        message:
          "Raw database query calls belong in data-store repositories, migrations, seeds, or DB clients.",
        rule: "sql-boundary",
      });
    }

    if (
      (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) &&
      moduleSpecifierText(node) !== undefined &&
      targetsForbiddenLayer(filePath, moduleSpecifierText(node))
    ) {
      locations.push({
        ...locationForNode(sourceFile, node.moduleSpecifier),
        message:
          "Lower layers must not import API modules. Move shared contracts into platform/workflows.",
        rule: "dependency-direction",
      });
    }

    if (
      isCheckedDomainLiteralFile(filePath) &&
      isStringLiteralExpression(node) &&
      !isTypeOnlyLiteral(node) &&
      !isImportOrExportLiteral(node)
    ) {
      const suggestion = restrictedDomainLiterals.get(node.text);
      if (suggestion !== undefined) {
        locations.push({
          ...locationForNode(sourceFile, node),
          message: `Avoid magic domain literal "${node.text}". ${suggestion}`,
          rule: "domain-literal",
        });
      }
    }

    ts.forEachChild(node, visitNode);
  };

  visitNode(sourceFile);
  return locations;
};

const violationsForFile = (filePath) => {
  const sourceFile = sourceFileForPath(filePath);
  const violations = violationsForSourceFile(filePath, sourceFile);

  return violations
    .filter((violation) => {
      return (
        violation.rule !== "try-catch-boundary" || !allowedTryCatchFiles.has(filePath)
      );
    })
    .map((violation) => ({
      filePath,
      ...violation,
    }));
};

const checkedFiles = candidateFiles(process.argv.slice(2));
const violations = checkedFiles.flatMap(violationsForFile);

if (violations.length > 0) {
  process.stderr.write(
    [
      "Code style check failed.",
      "",
      ...violations.map(
        (violation) =>
          `- ${violation.filePath}:${violation.line}:${violation.column} [${violation.rule}] ${violation.message}`,
      ),
      "",
    ].join("\n"),
  );

  process.exit(1);
}

process.stdout.write(
  `Code style check passed: ${checkedFiles.length} source file${
    checkedFiles.length === 1 ? "" : "s"
  } checked.\n`,
);
