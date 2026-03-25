const invoke = window.__TAURI__.core.invoke;

const state = {
  binaryCandidates: [],
  flowCandidates: [],
  rulePacks: [],
  manifests: [],
  reports: [],
  reportIndex: {},
  latestReport: null,
  latestResult: null,
  rewrittenArtifactPath: null,
  reportGroups: [],
  inlineViewLimitBytes: 0,
  selectedAction: "run",
  runningAction: null,
  nextAction: null,
  reviewDisplayMode: "grouped",
  reviewSortMode: "impact",
  flowUsageSummary: null,
  flowUsageStatus: "idle",
  flowUsageRequestKey: null,
};

function byId(id) {
  return document.getElementById(id);
}

function option(label, value, selected = false) {
  const el = document.createElement("option");
  el.textContent = label;
  el.value = value;
  el.selected = selected;
  return el;
}

function baseName(path) {
  return String(path || "").split("/").pop() || path;
}

function componentFamilyName(type) {
  const trimmed = String(type || "").trim();
  if (!trimmed) {
    return "";
  }
  const slashBase = trimmed.split("/").pop() || trimmed;
  return slashBase.split(".").pop() || slashBase;
}

function pluralize(count, singular, plural = `${singular}s`) {
  return `${count} ${count === 1 ? singular : plural}`;
}

function compactPath(path) {
  const parts = String(path || "").split("/");
  if (parts.length <= 3) {
    return parts.join("/");
  }
  return `.../${parts.slice(-2).join("/")}`;
}

function reportKey(report) {
  if (!report || typeof report !== "object") {
    return "";
  }
  return [
    report.kind || "",
    report.metadata?.generatedAt || "",
    report.source?.path || "",
    report.target?.nifiVersion || "",
  ].join("|");
}

function resetFlowUsageState() {
  state.flowUsageSummary = null;
  state.flowUsageStatus = "idle";
  state.flowUsageRequestKey = null;
}

function manualSourceStorageKey(path) {
  return `nifi-flow-upgrade-advisor:source-version:${path}`;
}

function loadRememberedSourceVersion(flow) {
  if (!flow?.path) {
    return "";
  }
  try {
    return localStorage.getItem(manualSourceStorageKey(flow.path)) || "";
  } catch (error) {
    return "";
  }
}

function saveRememberedSourceVersion(flowPath, version) {
  if (!flowPath) {
    return;
  }
  try {
    if (String(version || "").trim()) {
      localStorage.setItem(manualSourceStorageKey(flowPath), String(version).trim());
    } else {
      localStorage.removeItem(manualSourceStorageKey(flowPath));
    }
  } catch (error) {
    // Ignore storage issues and keep the UI functional.
  }
}

function filenameVersionHint(flow) {
  const candidate = [flow?.displayPath, flow?.path].filter(Boolean).join(" ");
  const match = candidate.match(/(?:^|[^0-9])(\d+\.\d+(?:\.\d+)?)(?:[^0-9]|$)/);
  return match ? match[1] : "";
}

function sourceFormatLabel(format) {
  switch (format) {
    case "flow-json-gz":
      return "flow.json.gz";
    case "flow-xml-gz":
      return "flow.xml.gz";
    case "flow-json-fixture":
      return "Flow fixture JSON";
    case "versioned-flow-snapshot":
      return "Versioned flow snapshot";
    case "git-registry-dir":
      return "Git registry directory";
    default:
      return format || "Unknown format";
  }
}

function escapeHtml(value) {
  return String(value || "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function renderInlineMarkdown(text) {
  return escapeHtml(text).replace(/`([^`]+)`/g, "<code>$1</code>");
}

function renderMarkdownToHtml(markdown) {
  const lines = String(markdown || "").split("\n");
  const parts = [];
  let inList = false;
  let inCode = false;
  let codeLines = [];

  function closeList() {
    if (inList) {
      parts.push("</ul>");
      inList = false;
    }
  }

  function closeCode() {
    if (inCode) {
      parts.push(`<pre><code>${escapeHtml(codeLines.join("\n"))}</code></pre>`);
      inCode = false;
      codeLines = [];
    }
  }

  for (const line of lines) {
    if (line.startsWith("```")) {
      closeList();
      if (inCode) {
        closeCode();
      } else {
        inCode = true;
      }
      continue;
    }
    if (inCode) {
      codeLines.push(line);
      continue;
    }

    if (!line.trim()) {
      closeList();
      continue;
    }

    const headingMatch = line.match(/^(#{1,3})\s+(.*)$/);
    if (headingMatch) {
      closeList();
      const level = headingMatch[1].length;
      parts.push(`<h${level}>${renderInlineMarkdown(headingMatch[2])}</h${level}>`);
      continue;
    }

    const listMatch = line.match(/^- (.*)$/);
    if (listMatch) {
      if (!inList) {
        parts.push("<ul>");
        inList = true;
      }
      parts.push(`<li>${renderInlineMarkdown(listMatch[1])}</li>`);
      continue;
    }

    closeList();
    parts.push(`<p>${renderInlineMarkdown(line)}</p>`);
  }

  closeCode();
  closeList();
  return parts.join("");
}

function setReportViewContent(path, content) {
  const view = byId("reportView");
  if (!view) {
    return;
  }
  if (String(path || "").endsWith(".md")) {
    view.classList.remove("empty");
    view.innerHTML = renderMarkdownToHtml(content);
    return;
  }
  const pretty = (() => {
    try {
      return JSON.stringify(JSON.parse(content), null, 2);
    } catch (error) {
      return content;
    }
  })();
  view.classList.remove("empty");
  view.innerHTML = `<pre><code>${escapeHtml(pretty)}</code></pre>`;
}

function setReportViewMessage(title, body) {
  const view = byId("reportView");
  if (!view) {
    return;
  }
  view.classList.remove("empty");
  view.innerHTML = `<div class="report-empty"><strong>${escapeHtml(title)}</strong><p>${escapeHtml(body)}</p></div>`;
}

function reportLabel(path) {
  const name = String(path || "").split("/").pop() || "";
  const base = name.replace(/\.(md|json)$/, "");
  const prefixMap = {
    "migration-report": "Analyze",
    "rewrite-report": "Rewrite",
    "validation-report": "Validate",
    "run-report": "Run",
  };
  const prefix = prefixMap[base];
  if (prefix) {
    return name.endsWith(".md") ? `${prefix} report` : `${prefix} JSON`;
  }
  if (name.endsWith(".md")) {
    return "Human report";
  }
  if (name.endsWith(".json")) {
    return "Structured report";
  }
  return name || "Report";
}

function preferredJsonReportPath(paths) {
  const ordered = [
    "run-report.json",
    "validation-report.json",
    "rewrite-report.json",
    "migration-report.json",
  ];
  for (const suffix of ordered) {
    const match = paths.find((path) => path.endsWith(suffix));
    if (match) {
      return match;
    }
  }
  return paths.find((path) => path.endsWith(".json")) || null;
}

function reportFileSizeLabel(bytes) {
  if (!bytes) {
    return "";
  }
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${Math.round(bytes / 1024)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function displaySourceLabel(report) {
  const flow = selectedFlowCandidate();
  if (flow?.displayPath) {
    return flow.displayPath;
  }
  if (report?.source?.path) {
    return baseName(report.source.path);
  }
  return "unknown source";
}

function setText(id, value) {
  byId(id).textContent = value;
}

function titleAction(action) {
  switch (action) {
    case "analyze":
      return "Analyze";
    case "rewrite":
      return "Rewrite";
    case "validate":
      return "Validate";
    case "run":
      return "Run";
    default:
      return action;
  }
}

function actionGuide(action) {
  switch (action) {
    case "analyze":
      return {
        title: "Compatibility check",
        body: "Checks the selected flow against the target version and shows blocked items, review items, and safe fixes before anything is changed.",
        checklist: [
          "Reads the selected source flow",
          "Applies the auto-selected upgrade packs",
          "Produces migration-report.json and migration-report.md",
        ],
      };
    case "rewrite":
      return {
        title: "Safe rewrite only",
        body: "Applies deterministic low-risk changes only. Anything architecture-sensitive or ambiguous stays as a finding for review.",
        checklist: [
          "Uses the analyze plan when available",
          "Writes a rewritten flow artifact",
          "Produces rewrite-report.json and rewrite-report.md",
        ],
      };
    case "validate":
      return {
        title: "Target readiness check",
        body: "Validates the rewritten artifact if one already exists in the output folder. Otherwise it validates the selected source artifact against the target checks.",
        checklist: [
          "Checks target version and extension inventory",
          "Can validate against a live target API and process group",
          "Produces validation-report.json and validation-report.md",
        ],
      };
    case "run":
      return {
        title: "Full upgrade workflow",
        body: "Runs analyze, rewrite, and validate in sequence. If analyze fails the threshold or validate blocks, later steps stop safely instead of forcing the upgrade through.",
        checklist: [
          "Stops after analyze if blocked threshold is exceeded",
          "Validates the rewritten artifact when rewrite completes",
          "Produces a final run-report plus step reports",
        ],
      };
    default:
      return {
        title: "Upgrade action",
        body: "Choose an action to see what it does.",
        checklist: [],
      };
  }
}

function actionDescription(action) {
  switch (action) {
    case "analyze":
      return "Analyze flow compatibility.";
    case "rewrite":
      return "Rewrite safe deterministic changes.";
    case "validate":
      return "Validate target readiness.";
    case "run":
      return "Run full workflow.";
    default:
      return "Action selected.";
  }
}

function validateActionRequest(action, request) {
  if (!request.targetVersion) {
    return "Please select a target version first.";
  }
  if (action !== "validate" && !request.sourceVersion) {
    return "Please enter or confirm a source version first.";
  }
  if (action !== "validate" && request.rulePackPaths.length === 0) {
    return "No built-in upgrade path is available for this version pair yet.";
  }
  return "";
}

function renderActionSelection() {
  const selected = state.selectedAction || "run";
  const running = state.runningAction;
  const guide = actionGuide(selected);
  const latestReport = state.latestReport;
  const rewriteSafeFixes =
    latestReport && latestReport.kind === "MigrationReport"
      ? Number(latestReport.summary?.byClass?.["auto-fix"] || 0)
      : null;
  const rewriteAssisted =
    latestReport && latestReport.kind === "MigrationReport"
      ? Number(latestReport.summary?.byClass?.["assisted-rewrite"] || 0)
      : null;
  const hasNoRewrites = rewriteSafeFixes === 0 && rewriteAssisted === 0;

  document.querySelectorAll("[data-action]").forEach((button) => {
    const action = button.dataset.action;
    button.classList.toggle("is-active", action === selected && action !== running);
    button.classList.toggle("is-running", action === running);
    button.disabled =
      (running !== null && action !== running) ||
      (action === "rewrite" && running === null && hasNoRewrites);
    button.setAttribute("aria-pressed", String(action === selected));
  });

  if (rewriteSafeFixes === null || rewriteAssisted === null) {
    setText("rewriteAvailabilityNote", "");
  } else if (hasNoRewrites) {
    setText("rewriteAvailabilityNote", "No safe rewrites are available for this flow.");
  } else if (rewriteSafeFixes > 0 && rewriteAssisted > 0) {
    setText(
      "rewriteAvailabilityNote",
      `${rewriteSafeFixes} safe rewrite${rewriteSafeFixes === 1 ? "" : "s"} and ${rewriteAssisted} assisted rewrite${rewriteAssisted === 1 ? "" : "s"} available for this flow.`
    );
  } else if (rewriteSafeFixes > 0) {
    setText(
      "rewriteAvailabilityNote",
      `${rewriteSafeFixes} safe rewrite${rewriteSafeFixes === 1 ? "" : "s"} available for this flow.`
    );
  } else {
    setText(
      "rewriteAvailabilityNote",
      `${rewriteAssisted} assisted rewrite${rewriteAssisted === 1 ? "" : "s"} available for this flow.`
    );
  }

  if (running) {
    setText("activeActionNote", `Running ${titleAction(running)}...`);
  } else {
    setText("activeActionNote", `Selected action: ${actionDescription(selected)}`);
  }

  setText("actionGuideEyebrow", titleAction(selected));
  setText("actionGuideTitle", guide.title);
  setText("actionGuideBody", guide.body);
  renderValidateAffordance();
  const checklist = byId("actionGuideChecklist");
  checklist.innerHTML = "";
  guide.checklist.forEach((item) => {
    const row = document.createElement("div");
    row.className = "selection-list-item";
    row.textContent = item;
    checklist.appendChild(row);
  });
}

function defaultOutputDir(workspacePath) {
  const date = new Date().toISOString().replace(/[:.]/g, "-");
  return `${workspacePath}/.nifi-flow-upgrade-desktop/${date}`;
}

function sourceVersionInput() {
  return byId("sourceVersion");
}

function versionMinor(version) {
  const match = String(version || "").trim().match(/^(\d+)\.(\d+)/);
  return match ? `${match[1]}.${match[2]}` : "";
}

function parseVersion(version) {
  const match = String(version || "").trim().match(/^(\d+)\.(\d+)(?:\.(\d+))?/);
  if (!match) {
    return null;
  }
  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: match[3] ? Number(match[3]) : null,
    minorKey: `${match[1]}.${match[2]}`,
    full: String(version || "").trim(),
  };
}

function parseRulePack(candidate) {
  const name = baseName(candidate.path || candidate.displayPath || "");
  if (name.includes(".sample.") || name.includes("-sample")) {
    return null;
  }
  const blockedMatch = name.match(/^nifi-(\d+\.\d+)-to-(\d+\.\d+)-pre-(\d+\.\d+)/);
  if (blockedMatch) {
    return {
      candidate,
      name,
      from: null,
      toMinor: null,
      toPatch: null,
      isPatchCaveat: false,
      isBlockedBridge: true,
      blockSourceStart: blockedMatch[1],
      blockSourceEnd: blockedMatch[2],
      blockTarget: blockedMatch[3],
    };
  }

  const edgeMatch = name.match(/^nifi-(\d+\.\d+)-to-(\d+\.\d+)(?:\.(\d+))?/);
  if (edgeMatch) {
    return {
      candidate,
      name,
      from: edgeMatch[1],
      toMinor: edgeMatch[2],
      toPatch: edgeMatch[3] || null,
      isPatchCaveat: name.includes(".patch-caveats."),
      isBlockedBridge: false,
      blockSourceStart: null,
      blockSourceEnd: null,
      blockTarget: null,
    };
  }

  return null;
}

function compareMinorKeys(leftKey, rightKey) {
  const left = parseVersion(leftKey);
  const right = parseVersion(rightKey);
  if (!left || !right) {
    return 0;
  }
  if (left.major !== right.major) {
    return left.major - right.major;
  }
  return left.minor - right.minor;
}

function compareVersions(leftVersion, rightVersion) {
  const left = parseVersion(leftVersion);
  const right = parseVersion(rightVersion);
  if (!left || !right) {
    return 0;
  }
  if (left.major !== right.major) {
    return left.major - right.major;
  }
  if (left.minor !== right.minor) {
    return left.minor - right.minor;
  }
  return (left.patch ?? 0) - (right.patch ?? 0);
}

function isMinorKeyInRange(value, start, end) {
  return compareMinorKeys(value, start) >= 0 && compareMinorKeys(value, end) <= 0;
}

function findPackPath(packs, sourceMinor, targetMinor) {
  if (sourceMinor === targetMinor) {
    return [];
  }

  const queue = [{ node: sourceMinor, path: [] }];
  const seen = new Set([sourceMinor]);

  while (queue.length > 0) {
    const current = queue.shift();
    for (const pack of packs) {
      if (pack.isPatchCaveat || pack.isBlockedBridge) {
        continue;
      }
      if (pack.from !== current.node || !pack.toMinor) {
        continue;
      }

      const nextPath = [...current.path, pack];
      if (pack.toMinor === targetMinor) {
        return nextPath;
      }
      if (!seen.has(pack.toMinor)) {
        seen.add(pack.toMinor);
        queue.push({ node: pack.toMinor, path: nextPath });
      }
    }
  }

  return [];
}

function bestRulePackSelection(sourceVersion, targetVersion) {
  const source = parseVersion(sourceVersion);
  const target = parseVersion(targetVersion);
  if (!source || !target) {
    return [];
  }

  const packs = state.rulePacks.map(parseRulePack).filter(Boolean);
  const selected = [];
  const selectedNames = new Set();

  function include(pack) {
    if (!pack || selectedNames.has(pack.name)) {
      return;
    }
    selected.push(pack.candidate.path);
    selectedNames.add(pack.name);
  }

  for (const pack of packs) {
    if (!pack.isBlockedBridge) {
      continue;
    }
    if (
      compareVersions(target.full, `${pack.blockTarget}.0`) >= 0 &&
      isMinorKeyInRange(source.minorKey, pack.blockSourceStart, pack.blockSourceEnd)
    ) {
      include(pack);
    }
  }

  const path = findPackPath(packs, source.minorKey, target.minorKey);
  for (const pack of path) {
    include(pack);
  }

  if (target.patch !== null) {
    include(
      packs.find(
        (pack) =>
          pack.isPatchCaveat &&
          pack.toMinor === target.minorKey &&
          pack.name.includes(`${target.minorKey}.${target.patch}`)
      )
    );
  }

  return selected;
}

function describeRulePackSelection(selectedPaths) {
  const parsed = selectedPaths
    .map((path) => state.rulePacks.find((candidate) => candidate.path === path))
    .map(parseRulePack)
    .filter(Boolean);

  const bridgeBlocks = parsed.filter((pack) => pack.isBlockedBridge).length;
  const patchCaveats = parsed.filter((pack) => pack.isPatchCaveat).length;
  const versionSteps = parsed.filter((pack) => !pack.isBlockedBridge && !pack.isPatchCaveat).length;

  return { bridgeBlocks, patchCaveats, versionSteps };
}

function noRulePackRequired(sourceVersion, targetVersion) {
  const source = parseVersion(sourceVersion);
  const target = parseVersion(targetVersion);
  if (!source || !target) {
    return false;
  }
  if (source.full === target.full) {
    return true;
  }
  return source.minorKey === target.minorKey;
}

function autoSelectRulePacks() {
  const select = byId("rulePackSelect");
  const sourceVersion = byId("sourceVersion").value;
  const targetVersion = byId("targetVersion").value;
  const selectedPaths = bestRulePackSelection(byId("sourceVersion").value, byId("targetVersion").value);
  const selected = new Set(selectedPaths);

  for (const opt of select.options) {
    opt.selected = selected.has(opt.value);
  }

  const hasPair = parseVersion(sourceVersion) && parseVersion(targetVersion);
  const summary = describeRulePackSelection(selectedPaths);
  setText(
    "rulePackNote",
    selected.size > 0
      ? [
          `Built-in upgrade coverage ready for ${sourceVersion} -> ${targetVersion}.`,
          summary.versionSteps > 0
            ? `${summary.versionSteps} version step${summary.versionSteps === 1 ? "" : "s"} selected.`
            : null,
          summary.patchCaveats > 0
            ? `${summary.patchCaveats} patch caveat${summary.patchCaveats === 1 ? "" : "s"} included.`
            : null,
          summary.bridgeBlocks > 0 ? "Bridge guidance included for this path." : null,
        ]
          .filter(Boolean)
          .join(" ")
      : noRulePackRequired(sourceVersion, targetVersion)
        ? "No upgrade packs are needed for this version step."
      : hasPair
        ? "No built-in upgrade path was found for this version pair yet."
        : "Pick source and target versions to use the built-in upgrade coverage for this path."
  );
}

function selectedManifestLabel() {
  const select = byId("manifestSelect");
  if (!select || !select.value) {
    return "No installed-components list selected.";
  }
  const picked = state.manifests.find((candidate) => candidate.path === select.value);
  return picked ? `Checking against installed components: ${picked.displayPath}` : "Checking against installed components.";
}

function currentSourceVersionState() {
  const flow = selectedFlowCandidate();
  const sourceInput = sourceVersionInput();
  const value = sourceInput.value.trim();
  const mode = sourceInput.dataset.mode || "";
  const detectedVersion = flow?.detectedVersion || "";
  const detectedConfidence = flow?.detectedVersionConfidence || "";
  const rememberedVersion = loadRememberedSourceVersion(flow);
  const filenameHint = filenameVersionHint(flow);

  if (value) {
    if (mode === "auto" && detectedVersion) {
      return {
        value,
        sourceLabel:
          detectedConfidence === "inferred" ? "inferred from embedded metadata" : "detected from embedded metadata",
        note:
          detectedConfidence === "inferred"
            ? `Source version inferred from embedded flow metadata: ${value}`
            : `Source version detected from embedded metadata: ${value}`,
      };
    }
    if (mode === "remembered" && rememberedVersion) {
      return {
        value,
        sourceLabel: "remembered from your last manual choice",
        note: `Using remembered source version: ${value}`,
      };
    }
    return {
      value,
      sourceLabel: "entered manually",
      note: `Using manually entered source version: ${value}`,
    };
  }

  if (filenameHint) {
    return {
      value: "",
      sourceLabel: "not set",
      note: `No embedded source version found. Filename suggests ${filenameHint}; confirm it before you run.`,
    };
  }

  return {
    value: "",
    sourceLabel: "not set",
    note: "No embedded source version found. Enter the source NiFi version manually to continue.",
  };
}

function renderFlowDetails() {
  const flow = selectedFlowCandidate();
  const target = byId("targetVersion").value.trim();
  if (!flow) {
    setText("flowDetails", "Choose a flow to see format, source version, and target details.");
    return;
  }

  const sourceState = currentSourceVersionState();
  const detectedSource = sourceState.value || "enter manually";
  const targetLabel = target || "choose a target";
  setText(
    "flowDetails",
    `Format: ${sourceFormatLabel(flow.sourceFormat)} • Source: ${detectedSource} (${sourceState.sourceLabel}) • Target: ${targetLabel}`
  );
}

function renderValidateAffordance() {
  const details = byId("validateDetails");
  const summary = byId("validateSummary");
  const note = byId("validateNote");
  const manifestSelected = Boolean(byId("manifestSelect").value);
  const validateSelected = state.selectedAction === "validate";

  details.classList.toggle("is-ready", manifestSelected);
  details.open = manifestSelected || validateSelected;
  summary.textContent = manifestSelected ? "Advanced check ready" : "Advanced check";
  note.textContent = manifestSelected
    ? "Validate can compare the flow against the installed components list you selected."
    : "Use Validate for an extra target-readiness pass after rewrite. Add an installed-components list if you want stricter target inventory checks.";
}

function renderSourceVersionNote() {
  const flow = selectedFlowCandidate();
  if (!flow) {
    setText("sourceVersionNote", "Choose a flow to detect embedded version metadata or enter the source version manually.");
    return;
  }
  setText("sourceVersionNote", currentSourceVersionState().note);
}

function autoSelectManifest() {
  const select = byId("manifestSelect");
  if (!select) {
    return;
  }
  select.value = "";
}

function applyFlowDefaults() {
  const flow = selectedFlowCandidate();
  const sourceInput = sourceVersionInput();
  if (!flow) {
    renderSourceVersionNote();
    renderFlowDetails();
    return;
  }
  if (flow.detectedVersion) {
    const currentValue = sourceInput.value.trim();
    const previousMode = sourceInput.dataset.mode || "";
    if (!currentValue || previousMode === "auto" || currentValue !== flow.detectedVersion) {
      sourceInput.value = flow.detectedVersion;
      sourceInput.dataset.mode = "auto";
    }
  } else {
    const remembered = loadRememberedSourceVersion(flow);
    if (remembered) {
      sourceInput.value = remembered;
      sourceInput.dataset.mode = "remembered";
    } else if ((sourceInput.dataset.mode || "") === "auto" || (sourceInput.dataset.mode || "") === "remembered") {
      sourceInput.value = "";
      sourceInput.dataset.mode = "";
    }
  }
  renderSourceVersionNote();
  renderFlowDetails();
  autoSelectRulePacks();
}

function renderWorkspace(data) {
  state.binaryCandidates = data.binaryCandidates || [];
  state.flowCandidates = data.flowCandidates || [];
  state.rulePacks = (data.rulePacks || []).filter((candidate) => parseRulePack(candidate) !== null);
  state.manifests = data.manifests || [];

  byId("workspacePath").value = data.workspaceRoot;
  byId("outputDir").value = defaultOutputDir(data.workspaceRoot);

  const binarySelect = byId("binarySelect");
  if (state.binaryCandidates.length === 0) {
    binarySelect.value = "";
    setText("binaryNote", "Using the built-in upgrade engine.");
  } else {
    binarySelect.value = state.binaryCandidates[0];
    setText("binaryNote", "Using the built-in upgrade engine.");
  }

  const flowSelect = byId("flowSelect");
  flowSelect.innerHTML = "";
  if (state.flowCandidates.length === 0) {
    flowSelect.appendChild(option("No flow detected", ""));
  } else {
    state.flowCandidates.forEach((candidate, index) => {
      const versionHint = candidate.detectedVersion
        ? candidate.detectedVersionConfidence === "inferred"
          ? ` · ${candidate.detectedVersion} (inferred)`
          : ` · ${candidate.detectedVersion}`
        : "";
      const label = `${candidate.kindLabel} — ${candidate.displayPath}${versionHint}`;
      flowSelect.appendChild(option(label, candidate.path, index === 0));
    });
  }

  const manifestSelect = byId("manifestSelect");
  manifestSelect.innerHTML = "";
  manifestSelect.appendChild(option("None", "", true));
  state.manifests.forEach((candidate) => {
    const isSample = candidate.path.includes("/examples/manifests/") || candidate.path.includes(".sample.");
    manifestSelect.appendChild(option(isSample ? `${candidate.displayPath} (sample)` : candidate.displayPath, candidate.path));
  });

  const rulePackSelect = byId("rulePackSelect");
  rulePackSelect.innerHTML = "";
  state.rulePacks.forEach((candidate) => {
    rulePackSelect.appendChild(option(candidate.displayPath, candidate.path));
  });

  setText(
    "scanSummary",
    state.flowCandidates.length > 0
      ? `Workspace ready: ${state.flowCandidates.length} flow${state.flowCandidates.length === 1 ? "" : "s"} detected.`
      : "Workspace ready: no supported flows detected yet."
  );

  applyFlowDefaults();
  autoSelectRulePacks();
  autoSelectManifest();
  setText("manifestNote", selectedManifestLabel());
  renderSourceVersionNote();
  renderFlowDetails();
  renderValidateAffordance();
}

function selectedRulePacks() {
  return Array.from(byId("rulePackSelect").selectedOptions).map((opt) => opt.value);
}

function selectedFlowCandidate() {
  return state.flowCandidates.find((candidate) => candidate.path === byId("flowSelect").value) || null;
}

function renderBadges(result) {
  const root = byId("summaryBadges");
  root.innerHTML = "";

  const exitBadge =
    result.exitCode === 0
      ? { label: "Command completed", className: "success" }
      : result.exitCode === 2
        ? { label: "Blocked findings present", className: "warning" }
        : { label: `Command failed (${result.exitCode})`, className: "error" };

  const badges = [exitBadge];

  if ((result.reportPaths || []).length > 0) {
    badges.push({
      label: `${result.reportPaths.length} export${result.reportPaths.length === 1 ? "" : "s"}`,
      className: "success",
    });
  }

  badges.forEach((badge) => {
    const el = document.createElement("span");
    el.className = `badge ${badge.className}`;
    el.textContent = badge.label;
    root.appendChild(el);
  });
}

function setResultNextAction(config) {
  state.nextAction = config || null;
  const button = byId("resultNextAction");
  if (!button) {
    return;
  }
  if (!config) {
    button.hidden = true;
    button.textContent = "Next step";
    return;
  }
  button.hidden = false;
  button.textContent = config.label;
}

function focusFindingSection(kind) {
  const section = document.querySelector(`.finding-section.section-${kind}`);
  if (section) {
    section.open = true;
    section.scrollIntoView({ behavior: "smooth", block: "start" });
    return;
  }
  focusReportView();
}

function focusReportView() {
  const reportView = byId("reportView");
  if (reportView) {
    reportView.scrollIntoView({ behavior: "smooth", block: "start" });
  }
}

function focusRelevantFindings() {
  const priorities = ["blocked", "assisted-rewrite", "review", "auto-fix", "info"];
  for (const kind of priorities) {
    const section = document.querySelector(`.finding-section.section-${kind}`);
    if (section) {
      const hasItems = section.querySelector(".finding-item");
      if (hasItems) {
        focusFindingSection(kind);
        return;
      }
    }
  }
  focusReportView();
}

function metricCard(label, value) {
  const card = document.createElement("div");
  card.className = "metric-card";

  const labelNode = document.createElement("span");
  labelNode.className = "metric-label";
  labelNode.textContent = label;

  const valueNode = document.createElement("span");
  valueNode.className = "metric-value";
  valueNode.textContent = String(value);

  card.appendChild(labelNode);
  card.appendChild(valueNode);
  return card;
}

function setResultBanner(config) {
  const card = byId("resultBanner");
  const title = byId("resultBannerTitle");
  const body = byId("resultBannerBody");
  card.className = "result-banner";
  if (!config) {
    card.hidden = true;
    title.textContent = "Ready";
    body.textContent = "Run a command to see a compact upgrade summary.";
    return;
  }
  card.hidden = false;
  card.classList.add(`variant-${config.variant || "warning"}`);
  title.textContent = config.title;
  body.textContent = config.body;
}

function topBlockedFinding(report) {
  if (!report || !Array.isArray(report.findings)) {
    return null;
  }
  return report.findings.find((finding) => finding.class === "blocked") || null;
}

function isReviewFinding(finding) {
  return finding?.class === "manual-change" || finding?.class === "manual-inspection";
}

function reviewFindings(report) {
  if (!report || !Array.isArray(report.findings)) {
    return [];
  }
  return report.findings.filter(isReviewFinding);
}

function reviewFindingGroupKey(finding) {
  const component = finding.component || {};
  return [
    finding.ruleId || "",
    finding.class || "",
    finding.message || "",
    finding.notes || "",
    JSON.stringify(finding.suggestedActions || []),
    component.name || "",
    component.type || "",
    component.scope || "",
  ].join("|");
}

function reviewFindingComponentKey(finding) {
  const component = finding.component || {};
  return component.id || component.path || [component.name || "", component.type || "", component.scope || ""].join("|");
}

function reviewFindingComponentSummary(finding) {
  const component = finding.component || {};
  return {
    id: component.id || "",
    name: component.name || componentFamilyName(component.type || "component"),
    type: component.type || "",
    scope: component.scope || "",
    path: component.path || "",
    occurrences: 1,
  };
}

function singleFindingDisplayEntry(finding) {
  const componentSummary = reviewFindingComponentSummary(finding);
  return {
    finding,
    occurrences: 1,
    distinctComponents: componentSummary.id || componentSummary.path || componentSummary.name ? 1 : 0,
    paths: componentSummary.path ? [componentSummary.path] : [],
    components: componentSummary.id || componentSummary.path || componentSummary.name ? [componentSummary] : [],
  };
}

function groupReviewFindingsForDisplay(findings) {
  const groups = new Map();
  (findings || []).forEach((finding) => {
    const key = reviewFindingGroupKey(finding);
    const componentKey = reviewFindingComponentKey(finding);
    const componentSummary = reviewFindingComponentSummary(finding);
    const existing = groups.get(key);
    if (existing) {
      existing.occurrences += 1;
      if (componentKey) {
        existing.componentKeys.add(componentKey);
        const existingComponent = existing.components.get(componentKey);
        if (existingComponent) {
          existingComponent.occurrences += 1;
        } else {
          existing.components.set(componentKey, componentSummary);
        }
      }
      if (finding.component?.path && !existing.paths.includes(finding.component.path)) {
        existing.paths.push(finding.component.path);
      }
      return;
    }
    const componentMap = new Map();
    if (componentKey) {
      componentMap.set(componentKey, componentSummary);
    }
    groups.set(key, {
      finding,
      occurrences: 1,
      componentKeys: componentKey ? new Set([componentKey]) : new Set(),
      paths: finding.component?.path ? [finding.component.path] : [],
      components: componentMap,
    });
  });
  return Array.from(groups.values()).map((group) => ({
    finding: group.finding,
    occurrences: group.occurrences,
    distinctComponents: group.componentKeys.size,
    paths: group.paths,
    components: Array.from(group.components.values()).sort((left, right) => {
      const leftLabel = [left.name || "", left.path || "", left.id || ""].join("|");
      const rightLabel = [right.name || "", right.path || "", right.id || ""].join("|");
      return leftLabel.localeCompare(rightLabel);
    }),
  }));
}

function reviewGroupSizeMap(findings) {
  const counts = new Map();
  (findings || []).forEach((finding) => {
    const key = reviewFindingGroupKey(finding);
    counts.set(key, Number(counts.get(key) || 0) + 1);
  });
  return counts;
}

function reviewEntrySortLabel(entry) {
  return `${findingDetailText(entry.finding)}|${entry.finding?.message || ""}|${entry.finding?.component?.path || ""}`;
}

function sortReviewEntries(entries, allFindings) {
  const byGroupSize = reviewGroupSizeMap(allFindings);
  return [...(entries || [])].sort((left, right) => {
    if (state.reviewSortMode === "az") {
      return reviewEntrySortLabel(left).localeCompare(reviewEntrySortLabel(right));
    }
    const leftWeight = Number(byGroupSize.get(reviewFindingGroupKey(left.finding)) || left.occurrences || 1);
    const rightWeight = Number(byGroupSize.get(reviewFindingGroupKey(right.finding)) || right.occurrences || 1);
    if (rightWeight !== leftWeight) {
      return rightWeight - leftWeight;
    }
    if (right.occurrences !== left.occurrences) {
      return right.occurrences - left.occurrences;
    }
    if (right.distinctComponents !== left.distinctComponents) {
      return right.distinctComponents - left.distinctComponents;
    }
    return reviewEntrySortLabel(left).localeCompare(reviewEntrySortLabel(right));
  });
}

function controllerServiceUsageMap() {
  const services = Array.isArray(state.flowUsageSummary?.controllerServices) ? state.flowUsageSummary.controllerServices : [];
  return new Map(services.map((service) => [service.id, service]));
}

function reviewEntryUsageStats(entry) {
  const usageById = controllerServiceUsageMap();
  const tracked = [];
  const referrerIds = new Set();
  (entry.components || []).forEach((component) => {
    if (!component.id || !usageById.has(component.id)) {
      return;
    }
    const usage = usageById.get(component.id);
    tracked.push({ component, usage });
    (usage.referencedBy || []).forEach((reference) => {
      if (reference.componentId) {
        referrerIds.add(reference.componentId);
      }
    });
  });
  const activeComponents = tracked.filter(({ usage }) => Number(usage.activeReferenceCount || 0) > 0).length;
  const unreferencedComponents = tracked.filter(({ usage }) => Number(usage.activeReferenceCount || 0) === 0).length;
  return {
    tracked,
    trackedComponents: tracked.length,
    activeComponents,
    unreferencedComponents,
    totalReferences: tracked.reduce((sum, { usage }) => sum + Number(usage.activeReferenceCount || 0), 0),
    distinctReferrers: referrerIds.size,
  };
}

function isFutureCleanupFinding(finding) {
  const text = `${finding?.message || ""} ${finding?.notes || ""}`.toLowerCase();
  return (
    text.includes("later major upgrade") ||
    text.includes("future major") ||
    text.includes("future cleanup") ||
    (text.includes("deprecated") && text.includes("removal"))
  );
}

function isReviewOnlyReport(report) {
  if (!report || (report.kind !== "MigrationReport" && report.kind !== "ValidationReport")) {
    return false;
  }
  const byClass = report.summary?.byClass || {};
  const blocked = Number(byClass["blocked"] || 0);
  const autoFix = Number(byClass["auto-fix"] || 0);
  const assisted = Number(byClass["assisted-rewrite"] || 0);
  const review = Number(byClass["manual-change"] || 0) + Number(byClass["manual-inspection"] || 0);
  return blocked === 0 && autoFix === 0 && assisted === 0 && review > 0;
}

function reviewOccurrenceStats(report) {
  const loaded = reviewFindings(report).length;
  const total = Number(report?.summary?.byClass?.["manual-change"] || 0) + Number(report?.summary?.byClass?.["manual-inspection"] || 0);
  return {
    loaded,
    total,
    truncated: Boolean(report?.preview?.truncated && total > loaded),
  };
}

function reviewPreviewNotice(report) {
  const stats = reviewOccurrenceStats(report);
  if (!stats.truncated) {
    return null;
  }
  return `These desktop insights are based on ${stats.loaded} loaded review occurrence${stats.loaded === 1 ? "" : "s"} out of ${stats.total} in the export. Open the output folder if you need the full raw review set.`;
}

function reviewSectionSummaryText(report, reviewContext) {
  const stats = reviewOccurrenceStats(report);
  if (reviewContext?.useGroupedView && reviewContext?.canGroup) {
    if (stats.truncated) {
      return `${reviewContext.entries.length} review group${reviewContext.entries.length === 1 ? "" : "s"} shown from ${stats.loaded} loaded occurrence${stats.loaded === 1 ? "" : "s"}. ${stats.total} total occurrence${stats.total === 1 ? "" : "s"} in the export.`;
    }
    return `${reviewContext.entries.length} review group${reviewContext.entries.length === 1 ? "" : "s"} to check. ${stats.total} total occurrence${stats.total === 1 ? "" : "s"} in the export.`;
  }
  if (stats.truncated) {
    return `${stats.loaded} review occurrence${stats.loaded === 1 ? "" : "s"} loaded in the desktop preview. ${stats.total} total occurrence${stats.total === 1 ? "" : "s"} in the export.`;
  }
  return `${stats.total} review item${stats.total === 1 ? "" : "s"} to check.`;
}

function renderPriorityCallout(report, reportIndex) {
  const card = byId("priorityCallout");
  const title = byId("priorityCalloutTitle");
  const body = byId("priorityCalloutBody");
  const meta = byId("priorityCalloutMeta");

  let blockedReport = null;
  if (report?.kind === "MigrationReport" || report?.kind === "ValidationReport") {
    blockedReport = report;
  } else if (report?.kind === "RunReport") {
    blockedReport = report.summary?.analyzeThresholdExceeded
      ? reportIndex.migrationReport
      : report.summary?.validationBlocked
        ? reportIndex.validationReport
        : null;
  }

  const finding = topBlockedFinding(blockedReport);
  if (!finding) {
    card.hidden = true;
    title.textContent = "Top blocker";
    body.textContent = "";
    meta.textContent = "";
    return;
  }

  card.hidden = false;
  title.textContent = "Top blocker";
  body.textContent = finding.message;
  meta.textContent = [findingDetailText(finding), finding.notes].filter(Boolean).join(" • ");
}

function summarizeRewriteChanges(rewriteReport) {
  if (!rewriteReport || rewriteReport.kind !== "RewriteReport" || !Array.isArray(rewriteReport.operations)) {
    return [];
  }

  const counts = new Map();
  const add = (label, amount = 1) => counts.set(label, (counts.get(label) || 0) + amount);

  rewriteReport.operations
    .filter((operation) => operation.status === "applied")
    .forEach((operation) => {
      switch (operation.actionType) {
        case "replace-component-type":
          if (operation.component?.scope === "processor") {
            add("processor replaced", 1);
          } else if (operation.component?.scope === "controller-service") {
            add("controller service replaced", 1);
          } else {
            add("component replaced", 1);
          }
          break;
        case "rename-property":
          add("property renamed", 1);
          break;
        case "remove-property":
          add("property removed", 1);
          break;
        case "set-property":
        case "set-property-if-absent":
          add("property set", 1);
          break;
        case "copy-property":
          add("property copied", 1);
          break;
        case "update-bundle-coordinate":
          add("bundle update", 1);
          break;
        case "emit-parameter-scaffold":
          add("parameter scaffold added", 1);
          break;
        default:
          add("rewrite applied", 1);
          break;
      }
    });

  return Array.from(counts.entries()).map(([label, count]) => pluralize(count, label));
}

function renderRewriteSummary(rewriteReport) {
  const card = byId("rewriteSummaryCard");
  const list = byId("rewriteSummaryList");
  card.hidden = true;
  list.innerHTML = "";

  const lines = summarizeRewriteChanges(rewriteReport);
  if (lines.length === 0) {
    return;
  }

  lines.forEach((line) => {
    const row = document.createElement("div");
    row.className = "selection-list-item";
    row.textContent = line;
    list.appendChild(row);
  });

  card.hidden = false;
}

function setUtilityActions(actions) {
  const root = byId("resultUtilityActions");
  root.innerHTML = "";
  (actions || []).forEach((action) => {
    const button = document.createElement("button");
    button.className = action.primary ? "button primary" : "button secondary";
    button.textContent = action.label;
    button.addEventListener("click", action.onClick);
    root.appendChild(button);
  });
}

async function openPath(path, createDirIfMissing = false) {
  if (!path) {
    return;
  }
  await invoke("open_path", { path, createDirIfMissing });
}

function renderResultUtilities(result) {
  const actions = [];
  if (state.rewrittenArtifactPath) {
    actions.push({
      label: "Open rewritten artifact",
      onClick: () => openPath(state.rewrittenArtifactPath, false),
    });
  }
  if (result?.outputDir) {
    actions.push({
      label: "Open output folder",
      onClick: () => openPath(result.outputDir, true),
    });
  }
  setUtilityActions(actions);
}

function renderResultBanner(report, result, reportIndex) {
  if (!report || typeof report !== "object") {
    setResultBanner(
      result?.exitCode === 0
        ? { variant: "success", title: "Command completed", body: "Structured results are ready below." }
        : { variant: "warning", title: "Command finished", body: "Review the command output and reports below." }
    );
    return;
  }

  if (report.kind === "MigrationReport") {
    const byClass = report.summary?.byClass || {};
    const blocked = Number(byClass["blocked"] || 0);
    const autoFix = Number(byClass["auto-fix"] || 0);
    const assisted = Number(byClass["assisted-rewrite"] || 0);
    const review = Number(byClass["manual-change"] || 0) + Number(byClass["manual-inspection"] || 0);
    const groupedReview = groupReviewFindingsForDisplay(reviewFindings(report));
    const groupedReviewCount = groupedReview.length;
    if (blocked > 0) {
      setResultBanner({ variant: "danger", title: "Blocked upgrade", body: topBlockedFinding(report)?.message || "Resolve the blocker before rewrite or run." });
    } else if (autoFix > 0 || assisted > 0) {
      setResultBanner({
        variant: "success",
        title: "Ready for rewrite",
        body:
          autoFix > 0
            ? `Rewrite can apply ${pluralize(autoFix, "safe fix", "safe fixes")}${assisted > 0 ? ` and scaffold ${pluralize(assisted, "assisted rewrite", "assisted rewrites")}.` : "."}`
            : `Rewrite can scaffold ${pluralize(assisted, "assisted rewrite", "assisted rewrites")}.`,
      });
    } else if (review > 0) {
      const reviewLabel =
        groupedReviewCount > 0 && groupedReviewCount !== review
          ? `${pluralize(groupedReviewCount, "review group", "review groups")} are shown in the desktop summary (${review} total occurrences remain in the export).`
          : `${pluralize(review, "review item", "review items")} are ready for review.`;
      setResultBanner({
        variant: "warning",
        title: "Upgrade can proceed with review",
        body: `No blockers were found. ${reviewLabel}`,
      });
    } else {
      setResultBanner({ variant: "success", title: "Upgrade check complete", body: "No flow-specific issues were found in the built-in coverage for this path." });
    }
    return;
  }

  if (report.kind === "RewriteReport") {
    const applied = Number(report.summary?.appliedOperations || 0);
    if (state.rewrittenArtifactPath) {
      setResultBanner({
        variant: "success",
        title: "Rewrite created a reviewed copy",
        body:
          applied > 0
            ? `A separate rewritten artifact was exported with ${pluralize(applied, "applied change", "applied changes")}.`
            : "A separate rewritten artifact was exported with no applied rewrite changes.",
      });
    } else {
      setResultBanner({ variant: "warning", title: "Rewrite finished", body: "Review the rewrite report for details." });
    }
    return;
  }

  if (report.kind === "ValidationReport") {
    const byClass = report.summary?.byClass || {};
    const blocked = Number(byClass["blocked"] || 0);
    const review = Number(byClass["manual-change"] || 0) + Number(byClass["manual-inspection"] || 0);
    if (blocked > 0) {
      setResultBanner({ variant: "danger", title: "Validation blocked", body: topBlockedFinding(report)?.message || "Target checks failed for this artifact." });
    } else if (review > 0) {
      setResultBanner({ variant: "warning", title: "Validation needs review", body: `Target checks surfaced ${pluralize(review, "review item", "review items")}.` });
    } else {
      setResultBanner({ variant: "success", title: "Validation passed", body: "The selected artifact passed the current target readiness checks." });
    }
    return;
  }

  if (report.kind === "RunReport") {
    if (report.summary?.analyzeThresholdExceeded || report.summary?.validationBlocked) {
      renderPriorityCallout(report, reportIndex);
      setResultBanner({ variant: "danger", title: "Blocked upgrade", body: "Run stopped safely before this flow could be pushed further." });
    } else if (state.rewrittenArtifactPath) {
      setResultBanner({ variant: "success", title: "Run created a reviewed copy", body: "Analyze, rewrite, and validate completed with a separate reviewed artifact exported for you." });
    } else {
      setResultBanner({ variant: "success", title: "Run completed", body: "The guided workflow completed for this flow." });
    }
    return;
  }

  setResultBanner({ variant: "success", title: "Report loaded", body: "Structured results are ready below." });
}

function renderResultOverview(report, result) {
  const headline = byId("resultHeadline");
  const subhead = byId("resultSubhead");
  const metrics = byId("resultMetrics");
  const meta = byId("resultMeta");
  metrics.innerHTML = "";
  setResultNextAction(null);

  if (!report || typeof report !== "object") {
    headline.textContent = result.exitCode === 0 ? "Command completed." : "Command finished with warnings.";
    subhead.textContent = "Run a command to load a structured report summary here.";
    meta.textContent = "No structured report details available yet.";
    return;
  }

  const sourceVersion = report.source?.nifiVersion || byId("sourceVersion").value.trim() || "unknown";
  const targetVersion = report.target?.nifiVersion || byId("targetVersion").value.trim() || "unknown";
  const sourceLabel = displaySourceLabel(report);

  if (report.kind === "MigrationReport" || report.kind === "ValidationReport") {
    const byClass = report.summary?.byClass || {};
    const blocked = byClass["blocked"] || 0;
    const autoFix = byClass["auto-fix"] || 0;
    const assisted = byClass["assisted-rewrite"] || 0;
    const manual = (byClass["manual-change"] || 0) + (byClass["manual-inspection"] || 0);
    const groupedManual = groupReviewFindingsForDisplay(reviewFindings(report));
    const groupedManualCount = groupedManual.length;
    const info = byClass["info"] || 0;
    const topFinding = Array.isArray(report.findings) && report.findings.length > 0 ? report.findings[0] : null;
    const isBridgeUpgradeBlock = topFinding?.ruleId === "core.bridge-upgrade.requires-1.27";
    const isPre121BaselineBlock = topFinding?.ruleId === "core.pre-1.21.support-baseline-required";

    if (isPre121BaselineBlock) {
      headline.textContent = "Upgrade path blocked: re-export this flow from NiFi 1.21.x or newer first";
    } else if (isBridgeUpgradeBlock) {
      headline.textContent = "Upgrade path blocked: move this flow to 1.27.x before targeting NiFi 2.x";
    } else if (blocked > 0) {
      headline.textContent = `Blocked upgrade: ${blocked} required change${blocked === 1 ? "" : "s"}`;
    } else if (autoFix > 0 || assisted > 0) {
      headline.textContent = `Rewrite available: ${autoFix} safe, ${assisted} assisted`;
    } else if (manual > 0) {
      headline.textContent =
        groupedManualCount > 0 && groupedManualCount !== manual
          ? `Review guidance: ${groupedManualCount} group${groupedManualCount === 1 ? "" : "s"}`
          : `Review guidance: ${manual} item${manual === 1 ? "" : "s"}`;
    } else {
      headline.textContent = "No flow-specific upgrade issues found.";
    }

    subhead.textContent =
      report.kind === "ValidationReport"
        ? topFinding?.message || `Validated ${sourceVersion} -> ${targetVersion} against the chosen target checks.`
        : isPre121BaselineBlock
          ? "Built-in version-to-version coverage starts at NiFi 1.21.x."
          : isBridgeUpgradeBlock
          ? "Apache NiFi requires a bridge upgrade to 1.27.x before entering the 2.x line."
          : topFinding?.message || `Analyzed ${sourceVersion} -> ${targetVersion} using the built-in upgrade coverage.`;

    metrics.appendChild(metricCard("Blocked", blocked));
    metrics.appendChild(metricCard("Safe fixes", autoFix));
    metrics.appendChild(metricCard("Assisted", assisted));
    metrics.appendChild(metricCard(groupedManualCount > 0 && groupedManualCount !== manual ? "Review groups" : "Review items", groupedManualCount > 0 && groupedManualCount !== manual ? groupedManualCount : manual));
    if (groupedManualCount > 0 && groupedManualCount !== manual) {
      metrics.appendChild(metricCard("Occurrences", manual));
    }
    metrics.appendChild(metricCard("Info", info));
    meta.textContent = `Flow ${sourceLabel} • Target ${targetVersion}`;

    if (report.kind === "ValidationReport") {
      if (blocked > 0 || manual > 0) {
        setResultNextAction({ label: "Review validation details", onClick: focusRelevantFindings });
      }
    } else if (blocked > 0) {
      setResultNextAction({ label: "Review blockers", onClick: () => focusFindingSection("blocked") });
    } else if (autoFix > 0 || assisted > 0) {
      setResultNextAction({ label: "Run Rewrite", onClick: () => runAction("rewrite") });
      if (autoFix > 0 && assisted > 0) {
        subhead.textContent = `This flow can move forward. Rewrite can apply ${autoFix} safe fix${autoFix === 1 ? "" : "es"} and scaffold ${assisted} reviewable change${assisted === 1 ? "" : "s"}.`;
      } else if (autoFix > 0) {
        subhead.textContent = `This flow can move forward. Rewrite can apply ${autoFix} safe fix${autoFix === 1 ? "" : "es"}.`;
      } else {
        subhead.textContent = `This flow can move forward. Rewrite can scaffold ${assisted} reviewable change${assisted === 1 ? "" : "s"}.`;
      }
    } else if (manual > 0) {
      setResultNextAction({ label: "Review findings", onClick: () => focusFindingSection("review") });
      if (groupedManualCount > 0 && groupedManualCount !== manual) {
        subhead.textContent = `No blockers were found. The desktop summary groups repeated review entries into ${groupedManualCount} review group${groupedManualCount === 1 ? "" : "s"} while the export keeps all ${manual} occurrences.`;
      } else {
        subhead.textContent = "No blockers were found. Review items are advisory checks to confirm before upgrade.";
      }
    } else {
      setResultNextAction({ label: "Run Validate", onClick: () => runAction("validate") });
      subhead.textContent = "This flow can move forward with no flow-specific issues in the built-in coverage.";
    }
    return;
  }

  if (report.kind === "RewriteReport") {
    const applied = report.summary?.appliedOperations || 0;
    const skipped = report.summary?.skippedOperations || 0;
    const total = report.summary?.totalOperations || 0;
    const appliedSafe = report.summary?.appliedByClass?.["auto-fix"] || 0;
    const appliedAssisted = report.summary?.appliedByClass?.["assisted-rewrite"] || 0;
    headline.textContent =
      applied > 0 ? `Rewrite applied: ${applied} change${applied === 1 ? "" : "s"}` : "No rewrites applied.";
    subhead.textContent = `Rewrote ${sourceVersion} -> ${targetVersion} using safe and assisted migration actions only.`;
    metrics.appendChild(metricCard("Applied", applied));
    metrics.appendChild(metricCard("Safe", appliedSafe));
    metrics.appendChild(metricCard("Assisted", appliedAssisted));
    metrics.appendChild(metricCard("Skipped", skipped));
    metrics.appendChild(metricCard("Total ops", total));
    meta.textContent = `Flow ${sourceLabel} • Target ${targetVersion}`;
    setResultNextAction({ label: "Run Validate", onClick: () => runAction("validate") });
    return;
  }

  if (report.kind === "RunReport") {
    const summary = report.summary || {};
    const rewriteStep = Array.isArray(report.steps)
      ? report.steps.find((step) => step.name === "rewrite")
      : null;
    if (summary.analyzeThresholdExceeded) {
      headline.textContent = "Run stopped after analyze.";
    } else if (summary.validationBlocked) {
      headline.textContent = "Run blocked during validate.";
    } else if (summary.status === "completed") {
      headline.textContent = "Run completed successfully.";
    } else {
      headline.textContent = `Run status: ${summary.status || "unknown"}`;
    }
    if (rewriteStep?.outputPath) {
      subhead.textContent = `Ran the guided ${sourceVersion} -> ${targetVersion} workflow and wrote a reviewed artifact to ${compactPath(rewriteStep.outputPath)}.`;
    } else if (summary.analyzeThresholdExceeded) {
      subhead.textContent = "Analyze stopped the workflow before a rewritten artifact was created.";
    } else {
      subhead.textContent = `Ran the guided ${sourceVersion} -> ${targetVersion} workflow.`;
    }
    metrics.appendChild(metricCard("Steps done", summary.completedSteps || 0));
    metrics.appendChild(metricCard("Publish", summary.publishEnabled ? "On" : "Off"));
    metrics.appendChild(metricCard("Status", summary.status || "unknown"));
    meta.textContent = `Flow ${sourceLabel} • Target ${targetVersion}`;
    if (summary.analyzeThresholdExceeded || summary.validationBlocked) {
      setResultNextAction({ label: "Review findings", onClick: focusRelevantFindings });
    }
    return;
  }

  headline.textContent = report.kind || "Report loaded";
  subhead.textContent = `Source ${sourceVersion} -> Target ${targetVersion}`;
  meta.textContent = `Flow ${sourceLabel}`;
}

function resetResultOverview() {
  byId("resultHeadline").textContent = "No results yet.";
  byId("resultSubhead").textContent = "Run a command to see a compact upgrade summary here first.";
  byId("resultMetrics").innerHTML = "";
  byId("resultMeta").textContent = "No report summary yet.";
  setResultNextAction(null);
  setUtilityActions([]);
  setResultBanner(null);
  renderPriorityCallout(null, {});
  renderRewriteSummary(null);
  state.latestReport = null;
  state.latestResult = null;
  state.rewrittenArtifactPath = null;
  state.reportIndex = {};
  resetFlowUsageState();
  renderRunSteps(null);
  renderActionSelection();
  renderReviewInsights(null);
  renderUpgradeTestChecklist(null);
}

function renderRunSteps(report) {
  const card = byId("runStepsCard");
  const list = byId("runStepsList");
  if (!card || !list) {
    return;
  }
  list.innerHTML = "";
  card.hidden = true;

  if (!report || report.kind !== "RunReport" || !Array.isArray(report.steps) || report.steps.length === 0) {
    return;
  }

  report.steps.forEach((step) => {
    const row = document.createElement("div");
    row.className = "selection-list-item step-item";

    const copy = document.createElement("div");
    copy.className = "step-copy";

    const name = document.createElement("div");
    name.className = "step-name";
    name.textContent = step.name || "step";
    copy.appendChild(name);

    if (step.message) {
      const message = document.createElement("div");
      message.className = "step-message";
      message.textContent = step.message;
      copy.appendChild(message);
    }

    if (step.outputPath) {
      const output = document.createElement("div");
      output.className = "step-path";
      output.textContent = step.outputPath;
      copy.appendChild(output);
    }

    const status = document.createElement("span");
    status.className = `step-status ${step.status === "completed" ? "completed" : step.status === "skipped" ? "skipped" : "other"}`;
    status.textContent = step.status || "unknown";

    row.appendChild(copy);
    row.appendChild(status);
    list.appendChild(row);
  });

  card.hidden = false;
}

function findingSectionTitle(kind) {
  switch (kind) {
    case "blocked":
      return "Blocked";
    case "review":
      return "Review";
    case "assisted-rewrite":
      return "Assisted rewrites";
    case "auto-fix":
      return "Safe fixes";
    case "info":
      return "Info";
    default:
      return kind;
  }
}

function findingSectionIcon(kind) {
  switch (kind) {
    case "blocked":
      return "!";
    case "review":
      return "?";
    case "assisted-rewrite":
      return "~";
    case "auto-fix":
      return "✓";
    case "info":
      return "i";
    default:
      return "•";
  }
}

function summarizeSection(kind, count) {
  if (count === 0) {
    switch (kind) {
      case "blocked":
        return "No blockers found.";
      case "review":
        return "No review items found.";
      case "assisted-rewrite":
        return "No assisted rewrites available.";
      case "auto-fix":
        return "No safe fixes available.";
      case "info":
        return "No extra notes.";
      default:
        return "No findings.";
    }
  }
  switch (kind) {
    case "blocked":
      return `${count} blocker${count === 1 ? "" : "s"} need attention.`;
    case "review":
      return `${count} review item${count === 1 ? "" : "s"} to check.`;
    case "assisted-rewrite":
      return `${count} assisted rewrite${count === 1 ? "" : "s"} can be scaffolded.`;
    case "auto-fix":
      return `${count} safe fix${count === 1 ? "" : "es"} can be applied.`;
    case "info":
      return `${count} informational note${count === 1 ? "" : "s"}.`;
    default:
      return `${count} item${count === 1 ? "" : "s"}.`;
  }
}

function findingDetailText(finding) {
  const parts = [];
  const component = finding.component;
  if (component?.name) {
    parts.push(component.name);
  } else if (component?.type) {
    parts.push(componentFamilyName(component.type));
  }
  if (component?.scope) {
    parts.push(component.scope);
  }
  return parts.join(" • ");
}

function reviewSectionDisplayContext(findings) {
  const groupedEntries = groupReviewFindingsForDisplay(findings);
  const canGroup = groupedEntries.length > 0 && groupedEntries.length !== findings.length;
  const useGroupedView = canGroup ? state.reviewDisplayMode !== "all" : true;
  const entries = useGroupedView ? groupedEntries : findings.map(singleFindingDisplayEntry);
  return {
    entries: sortReviewEntries(entries, findings),
    groupedEntries,
    canGroup,
    useGroupedView,
  };
}

function findingEntryBadges(entry, usageStats) {
  const badges = [];
  if (isReviewFinding(entry.finding)) {
    badges.push({ label: "Advisory", tone: "advisory" });
  }
  if (isFutureCleanupFinding(entry.finding)) {
    badges.push({ label: "Future cleanup", tone: "neutral" });
  }
  if (usageStats.trackedComponents > 0) {
    if (usageStats.activeComponents > 0) {
      badges.push({ label: `${usageStats.activeComponents} active`, tone: "success" });
    }
    if (usageStats.unreferencedComponents > 0) {
      badges.push({ label: `${usageStats.unreferencedComponents} unreferenced`, tone: "muted" });
    }
  }
  return badges;
}

function appendFindingBadges(container, badges) {
  if (!Array.isArray(badges) || badges.length === 0) {
    return;
  }
  const row = document.createElement("div");
  row.className = "finding-badges";
  badges.forEach((badge) => {
    const chip = document.createElement("span");
    chip.className = `finding-badge tone-${badge.tone || "neutral"}`;
    chip.textContent = badge.label;
    row.appendChild(chip);
  });
  container.appendChild(row);
}

function reviewUsageSummaryText(entry, usageStats) {
  if (entry.finding?.component?.scope !== "controller-service") {
    return null;
  }
  if (state.flowUsageStatus === "loading") {
    return "Loading active-use insight from the source flow.";
  }
  if (state.flowUsageSummary && state.flowUsageSummary.supported === false) {
    return state.flowUsageSummary.message || "Usage insight is not available for this source format.";
  }
  if (usageStats.trackedComponents === 0) {
    return null;
  }
  if (usageStats.activeComponents > 0 && usageStats.unreferencedComponents > 0) {
    return `Active use: ${usageStats.activeComponents} grouped service${usageStats.activeComponents === 1 ? "" : "s"} are referenced ${usageStats.totalReferences} time${usageStats.totalReferences === 1 ? "" : "s"} by ${usageStats.distinctReferrers} component${usageStats.distinctReferrers === 1 ? "" : "s"}. ${usageStats.unreferencedComponents} grouped service${usageStats.unreferencedComponents === 1 ? "" : "s"} are currently unreferenced.`;
  }
  if (usageStats.activeComponents > 0) {
    return `Active use: referenced ${usageStats.totalReferences} time${usageStats.totalReferences === 1 ? "" : "s"} by ${usageStats.distinctReferrers} component${usageStats.distinctReferrers === 1 ? "" : "s"} in this export.`;
  }
  return "Unreferenced in this export. This looks like cleanup debt rather than a current upgrade risk.";
}

function reviewComponentUsage(component) {
  if (!component?.id) {
    return null;
  }
  return controllerServiceUsageMap().get(component.id) || null;
}

function appendInlineComponentDetails(item, entry) {
  const components = Array.isArray(entry.components) ? entry.components : [];
  const hasUsage = components.some((component) => reviewComponentUsage(component));
  const hasPaths = components.some((component) => component.path || component.id);
  if (components.length === 0 || (!hasUsage && !hasPaths && components.length < 2)) {
    return;
  }

  const details = document.createElement("details");
  details.className = "finding-inline-details";

  const summary = document.createElement("summary");
  if (components.length > 1) {
    summary.textContent = `Show ${components.length} grouped component${components.length === 1 ? "" : "s"}`;
  } else if (hasUsage) {
    summary.textContent = "Show component usage";
  } else {
    summary.textContent = "Show component details";
  }
  details.appendChild(summary);

  const list = document.createElement("div");
  list.className = "inline-detail-list";

  components.forEach((component) => {
    const usage = reviewComponentUsage(component);
    const row = document.createElement("div");
    row.className = "inline-detail-item";

    const head = document.createElement("div");
    head.className = "inline-detail-head";

    const title = document.createElement("div");
    title.className = "inline-detail-title";
    title.textContent = component.name || componentFamilyName(component.type || "component");
    head.appendChild(title);

    const badges = [];
    if (component.occurrences > 1) {
      badges.push({ label: `Repeated ${component.occurrences} times`, tone: "neutral" });
    }
    if (usage) {
      badges.push({
        label: Number(usage.activeReferenceCount || 0) > 0 ? "Active use" : "Unreferenced",
        tone: Number(usage.activeReferenceCount || 0) > 0 ? "success" : "muted",
      });
    }
    appendFindingBadges(head, badges);
    row.appendChild(head);

    const detailLines = [];
    if (component.path) {
      detailLines.push(component.path);
    }
    if (component.id) {
      detailLines.push(`ID ${component.id}`);
    }
    if (detailLines.length > 0) {
      const meta = document.createElement("div");
      meta.className = "inline-detail-meta";
      meta.textContent = detailLines.join(" • ");
      row.appendChild(meta);
    }

    if (usage && Number(usage.activeReferenceCount || 0) > 0) {
      const summaryLine = document.createElement("div");
      summaryLine.className = "inline-detail-meta";
      summaryLine.textContent = `Referenced ${usage.activeReferenceCount} time${Number(usage.activeReferenceCount || 0) === 1 ? "" : "s"} by ${usage.distinctReferrerCount} component${Number(usage.distinctReferrerCount || 0) === 1 ? "" : "s"}.`;
      row.appendChild(summaryLine);

      const examples = [];
      const seen = new Set();
      (usage.referencedBy || []).forEach((reference) => {
        const key = `${reference.componentId || ""}|${reference.propertyName || ""}`;
        if (seen.has(key) || examples.length >= 3) {
          return;
        }
        seen.add(key);
        const componentLabel = reference.componentName || componentFamilyName(reference.componentType || "component");
        examples.push(`${componentLabel}${reference.propertyName ? ` via ${reference.propertyName}` : ""}`);
      });
      if (examples.length > 0) {
        const refs = document.createElement("div");
        refs.className = "inline-detail-meta";
        refs.textContent = `Examples: ${examples.join(" • ")}`;
        row.appendChild(refs);
      }
    }

    list.appendChild(row);
  });

  details.appendChild(list);
  item.appendChild(details);
}

function appendReviewSectionToolbar(body, findings, displayContext) {
  if (!displayContext || findings.length === 0) {
    return;
  }
  const toolbar = document.createElement("div");
  toolbar.className = "finding-toolbar";

  if (displayContext.canGroup) {
    const viewGroup = document.createElement("div");
    viewGroup.className = "finding-toolbar-group";
    const viewLabel = document.createElement("span");
    viewLabel.className = "finding-toolbar-label";
    viewLabel.textContent = "View";
    viewGroup.appendChild(viewLabel);

    const viewButtons = document.createElement("div");
    viewButtons.className = "finding-toolbar-buttons";
    [
      { value: "grouped", label: "Grouped" },
      { value: "all", label: "All occurrences" },
    ].forEach((option) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = `finding-toolbar-button ${state.reviewDisplayMode === option.value ? "active" : ""}`;
      button.textContent = option.label;
      button.addEventListener("click", () => {
        state.reviewDisplayMode = option.value;
        renderFindingSections(state.latestReport);
      });
      viewButtons.appendChild(button);
    });
    viewGroup.appendChild(viewButtons);
    toolbar.appendChild(viewGroup);
  }

  const sortGroup = document.createElement("div");
  sortGroup.className = "finding-toolbar-group";
  const sortLabel = document.createElement("span");
  sortLabel.className = "finding-toolbar-label";
  sortLabel.textContent = "Sort";
  sortGroup.appendChild(sortLabel);

  const sortButtons = document.createElement("div");
  sortButtons.className = "finding-toolbar-buttons";
  [
    { value: "impact", label: "Largest first" },
    { value: "az", label: "A-Z" },
  ].forEach((option) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `finding-toolbar-button ${state.reviewSortMode === option.value ? "active" : ""}`;
    button.textContent = option.label;
    button.addEventListener("click", () => {
      state.reviewSortMode = option.value;
      renderFindingSections(state.latestReport);
    });
    sortButtons.appendChild(button);
  });
  sortGroup.appendChild(sortButtons);
  toolbar.appendChild(sortGroup);

  body.appendChild(toolbar);
}

function renderFindingSections(report) {
  const root = byId("findingSections");
  root.innerHTML = "";
  if (!report || !Array.isArray(report.findings) || (report.kind !== "MigrationReport" && report.kind !== "ValidationReport")) {
    return;
  }

  const grouped = {
    blocked: report.findings.filter((finding) => finding.class === "blocked"),
    "assisted-rewrite": report.findings.filter((finding) => finding.class === "assisted-rewrite"),
    review: report.findings.filter((finding) => finding.class === "manual-change" || finding.class === "manual-inspection"),
    "auto-fix": report.findings.filter((finding) => finding.class === "auto-fix"),
    info: report.findings.filter((finding) => finding.class === "info"),
  };

  Object.entries(grouped).forEach(([kind, findings]) => {
    if (findings.length === 0) {
      return;
    }
    const reviewContext = kind === "review" ? reviewSectionDisplayContext(findings) : null;
    const condensed = kind === "review"
      ? reviewContext.entries
      : findings.map(singleFindingDisplayEntry);
    const section = document.createElement("details");
    section.className = `result-card finding-section section-${kind}`;
    section.dataset.kind = kind;
    section.open = kind !== "info";

    const summary = document.createElement("summary");
    const titleWrap = document.createElement("span");
    titleWrap.className = "section-title-wrap";
    const icon = document.createElement("span");
    icon.className = "section-icon";
    icon.textContent = findingSectionIcon(kind);
    const title = document.createElement("span");
    title.textContent = findingSectionTitle(kind);
    titleWrap.appendChild(icon);
    titleWrap.appendChild(title);
    const shownCount = Number(report.preview?.shownByClass?.[kind] || findings.length);
    const summaryCount =
      kind === "review"
        ? Number(report.summary?.byClass?.["manual-change"] || 0) + Number(report.summary?.byClass?.["manual-inspection"] || 0)
        : Number(
            report.summary?.byClass?.[
              kind === "assisted-rewrite" ? "assisted-rewrite" : kind
            ] || 0
          );

    const meta = document.createElement("span");
    meta.className = "section-summary";
    meta.textContent =
      kind === "review"
        ? reviewSectionSummaryText(report, reviewContext)
        : summarizeSection(kind, findings.length);
    summary.appendChild(titleWrap);
    summary.appendChild(meta);
    section.appendChild(summary);

    const body = document.createElement("div");
    body.className = "finding-section-body";

    if (kind === "review") {
      appendReviewSectionToolbar(body, findings, reviewContext);
    }

    if (kind === "review" && reviewContext?.useGroupedView && reviewContext?.canGroup) {
      const dedupeNote = document.createElement("div");
      dedupeNote.className = "finding-item-meta";
      dedupeNote.textContent = `The desktop summary groups review findings that share the same migration note and component label, then shows how many distinct components and occurrences contributed to each row. The exported report keeps all ${summaryCount} occurrences.`;
      body.appendChild(dedupeNote);
    }

    if (report.preview?.truncated && summaryCount > shownCount) {
      const previewLimit = document.createElement("div");
      previewLimit.className = "finding-item-meta";
      previewLimit.textContent =
        kind === "review" && reviewContext?.useGroupedView && reviewContext?.canGroup
          ? `The desktop preview loaded ${shownCount} of ${summaryCount} review occurrences before grouping. The desktop summary shows ${condensed.length} grouped row${condensed.length === 1 ? "" : "s"} here.`
          : kind === "review"
          ? `Showing ${condensed.length} of ${summaryCount} review occurrence${summaryCount === 1 ? "" : "s"} loaded into the desktop preview. Use the exported reports for the full list.`
          : `Showing ${shownCount} of ${summaryCount} ${findingSectionTitle(kind).toLowerCase()} items in the desktop preview. Use the exported reports for the full list.`;
      body.appendChild(previewLimit);
    }

    condensed.forEach((entry) => {
      const { finding, occurrences, distinctComponents, paths } = entry;
      const usageStats = kind === "review" ? reviewEntryUsageStats(entry) : { trackedComponents: 0, activeComponents: 0, unreferencedComponents: 0, totalReferences: 0, distinctReferrers: 0 };
      const item = document.createElement("div");
      item.className = "finding-item";

      const itemHead = document.createElement("div");
      itemHead.className = "finding-item-head";

      const itemTitle = document.createElement("div");
      itemTitle.className = "finding-item-title";
      itemTitle.textContent = finding.message;
      itemHead.appendChild(itemTitle);
      appendFindingBadges(itemHead, findingEntryBadges(entry, usageStats));
      item.appendChild(itemHead);

      const detail = findingDetailText(finding);
      if (detail) {
        const metaLine = document.createElement("div");
        metaLine.className = "finding-item-meta";
        metaLine.textContent = detail;
        item.appendChild(metaLine);
      }

      if (occurrences > 1 || distinctComponents > 1) {
        const repeat = document.createElement("div");
        repeat.className = "finding-item-meta";
        if (distinctComponents > 1 && occurrences > distinctComponents) {
          repeat.textContent = `Grouped from ${distinctComponents} distinct component${distinctComponents === 1 ? "" : "s"} and ${occurrences} total occurrences in this export.`;
        } else if (distinctComponents > 1) {
          repeat.textContent = `Grouped from ${distinctComponents} distinct component${distinctComponents === 1 ? "" : "s"} in this export.`;
        } else {
          repeat.textContent = `Repeated ${occurrences} times in this export.`;
        }
        item.appendChild(repeat);
      }

      if (kind === "review") {
        const usageSummary = reviewUsageSummaryText(entry, usageStats);
        if (usageSummary) {
          const usageMeta = document.createElement("div");
          usageMeta.className = "finding-item-meta";
          usageMeta.textContent = usageSummary;
          item.appendChild(usageMeta);
        }
      }

      if (finding.notes) {
        const notes = document.createElement("div");
        notes.className = "finding-item-meta";
        notes.textContent = finding.notes;
        item.appendChild(notes);
      }

      if ((occurrences > 1 || distinctComponents > 1) && paths.length > 1) {
        const pathNote = document.createElement("div");
        pathNote.className = "finding-item-meta";
        pathNote.textContent = `Shown here once for the grouped review summary. Check the exported report if you need every component path.`;
        item.appendChild(pathNote);
      }

      if (kind === "review") {
        appendInlineComponentDetails(item, entry);
      }

      if (Array.isArray(finding.suggestedActions) && finding.suggestedActions.length > 0) {
        const suggestions = document.createElement("div");
        suggestions.className = "finding-item-meta";
        suggestions.textContent = `Suggested next steps: ${finding.suggestedActions.map(formatActionPreview).join(" ")}`;
        item.appendChild(suggestions);
      }

      body.appendChild(item);
    });

    section.appendChild(body);
    root.appendChild(section);
  });
}

function appendInsightItem(list, titleText, bodyText) {
  const item = document.createElement("div");
  item.className = "selection-list-item insight-item";

  const title = document.createElement("div");
  title.className = "insight-item-title";
  title.textContent = titleText;
  item.appendChild(title);

  if (bodyText) {
    const meta = document.createElement("div");
    meta.className = "insight-item-meta";
    meta.textContent = bodyText;
    item.appendChild(meta);
  }

  list.appendChild(item);
}

function controllerServiceReviewEntries(report) {
  return groupReviewFindingsForDisplay(reviewFindings(report)).filter(
    (entry) => entry.finding?.component?.scope === "controller-service"
  );
}

function reviewedControllerServiceUsage(report) {
  const entries = controllerServiceReviewEntries(report);
  const services = [];
  entries.forEach((entry) => {
    (entry.components || []).forEach((component) => {
      const usage = reviewComponentUsage(component);
      if (usage) {
        services.push({ entry, component, usage });
      }
    });
  });
  return services;
}

function referenceFamilyCounts(services) {
  const families = new Map();
  services.forEach(({ usage }) => {
    (usage.referencedBy || []).forEach((reference) => {
      const family = componentFamilyName(reference.componentType || "");
      if (!family) {
        return;
      }
      const existing = families.get(family) || { ids: new Set(), names: new Set() };
      existing.ids.add(reference.componentId || `${reference.componentName || family}|${reference.propertyName || ""}`);
      if (reference.componentName) {
        existing.names.add(reference.componentName);
      }
      families.set(family, existing);
    });
  });
  return families;
}

function renderReviewInsights(report) {
  const card = byId("reviewInsightsCard");
  const meta = byId("reviewInsightsMeta");
  const list = byId("reviewInsightsList");
  if (!card || !meta || !list) {
    return;
  }
  card.hidden = true;
  meta.textContent = "";
  list.innerHTML = "";

  if (!report || (report.kind !== "MigrationReport" && report.kind !== "ValidationReport")) {
    return;
  }

  const entries = controllerServiceReviewEntries(report);
  if (entries.length === 0) {
    return;
  }

  card.hidden = false;
  const previewNotice = reviewPreviewNotice(report);

  if (state.flowUsageStatus === "loading") {
    meta.textContent = previewNotice
      ? `Loading active-use insight from the source flow so the advisory findings can be prioritized. ${previewNotice}`
      : "Loading active-use insight from the source flow so the advisory findings can be prioritized.";
    appendInsightItem(
      list,
      `${entries.length} controller-service review group${entries.length === 1 ? "" : "s"} detected`,
      "The desktop app is checking which of these services are still actively referenced versus left behind as cleanup debt."
    );
    return;
  }

  if (state.flowUsageSummary && state.flowUsageSummary.supported === false) {
    meta.textContent = [
      state.flowUsageSummary.message || "Usage insight is not available for this source format.",
      previewNotice,
    ].filter(Boolean).join(" ");
    appendInsightItem(
      list,
      `${entries.length} controller-service review group${entries.length === 1 ? "" : "s"} detected`,
      "These review findings are still advisory, but active-versus-unreferenced breakdown is not available for this flow export."
    );
    return;
  }

  const services = reviewedControllerServiceUsage(report);
  const activeServices = services.filter(({ usage }) => Number(usage.activeReferenceCount || 0) > 0);
  const unreferencedServices = services.filter(({ usage }) => Number(usage.activeReferenceCount || 0) === 0);
  const referrerIds = new Set();
  activeServices.forEach(({ usage }) => {
    (usage.referencedBy || []).forEach((reference) => {
      if (reference.componentId) {
        referrerIds.add(reference.componentId);
      }
    });
  });

  meta.textContent = [
    "These controller-service findings are advisory. Actively referenced services are the best smoke-test candidates for the upgrade.",
    previewNotice,
  ].filter(Boolean).join(" ");
  appendInsightItem(
    list,
    `${activeServices.length} actively used reviewed service${activeServices.length === 1 ? "" : "s"}`,
    activeServices.length > 0
      ? `Referenced by ${referrerIds.size} distinct component${referrerIds.size === 1 ? "" : "s"} in this export.`
      : "No actively referenced reviewed controller services were found."
  );
  if (unreferencedServices.length > 0) {
    appendInsightItem(
      list,
      `${unreferencedServices.length} unreferenced reviewed service${unreferencedServices.length === 1 ? "" : "s"}`,
      "These look like cleanup debt you can schedule after the upgrade rather than immediate smoke-test priorities."
    );
  }

  sortReviewEntries(entries, reviewFindings(report))
    .slice(0, 5)
    .forEach((entry) => {
      const usageStats = reviewEntryUsageStats(entry);
      appendInsightItem(
        list,
        findingDetailText(entry.finding) || entry.finding.message,
        reviewUsageSummaryText(entry, usageStats)
      );
    });
}

function renderUpgradeTestChecklist(report) {
  const card = byId("upgradeTestCard");
  const meta = byId("upgradeTestMeta");
  const list = byId("upgradeTestList");
  if (!card || !meta || !list) {
    return;
  }
  card.hidden = true;
  meta.textContent = "";
  list.innerHTML = "";

  if (!isReviewOnlyReport(report)) {
    return;
  }

  card.hidden = false;
  const previewNotice = reviewPreviewNotice(report);
  meta.textContent = [
    "These are smoke tests to run before promotion. Review findings here are advisory checks, not blockers.",
    previewNotice,
  ].filter(Boolean).join(" ");

  const groupedReview = groupReviewFindingsForDisplay(reviewFindings(report));
  const futureCleanupCount = groupedReview.filter((entry) => isFutureCleanupFinding(entry.finding)).length;
  const reviewedServices = reviewedControllerServiceUsage(report);
  const activeReviewedServices = reviewedServices.filter(({ usage }) => Number(usage.activeReferenceCount || 0) > 0);
  const unreferencedReviewedServices = reviewedServices.filter(({ usage }) => Number(usage.activeReferenceCount || 0) === 0);

  if (activeReviewedServices.length > 0) {
    appendInsightItem(
      list,
      "Start with the actively used reviewed services",
      `${activeReviewedServices.length} reviewed controller service${activeReviewedServices.length === 1 ? "" : "s"} are still referenced in this export, so they are the best first-pass upgrade smoke tests.`
    );
  }

  const families = referenceFamilyCounts(activeReviewedServices);
  const familyEntries = Array.from(families.entries()).sort((left, right) => right[1].ids.size - left[1].ids.size);
  familyEntries.forEach(([family, details], index) => {
    if (index >= 3) {
      return;
    }
    if (family === "HandleHttpRequest") {
      appendInsightItem(
        list,
        "Send inbound HTTPS requests through reviewed listeners",
        `Exercise ${details.ids.size} HandleHttpRequest component${details.ids.size === 1 ? "" : "s"} that depend on reviewed SSL services.`
      );
      return;
    }
    if (family === "InvokeHTTP") {
      appendInsightItem(
        list,
        "Run outbound HTTPS call paths",
        `Exercise ${details.ids.size} InvokeHTTP component${details.ids.size === 1 ? "" : "s"} that depend on reviewed SSL services.`
      );
      return;
    }
    if (family === "ElasticSearchClientServiceImpl" || family === "ElasticSearchClientService") {
      appendInsightItem(
        list,
        "Validate reviewed Elasticsearch connectivity",
        `Confirm ${details.ids.size} Elasticsearch client service${details.ids.size === 1 ? "" : "s"} still connect cleanly after the upgrade.`
      );
      return;
    }
    appendInsightItem(
      list,
      `Exercise reviewed ${family} components`,
      `${details.ids.size} ${family} component${details.ids.size === 1 ? "" : "s"} reference reviewed services in this export.`
    );
  });

  if (unreferencedReviewedServices.length > 0) {
    appendInsightItem(
      list,
      "Leave unreferenced deprecated services for scheduled cleanup",
      `${unreferencedReviewedServices.length} reviewed controller service${unreferencedReviewedServices.length === 1 ? "" : "s"} are not referenced anywhere in this export, so they can be cleaned up after the 2.8 rollout.`
    );
  }

  if (futureCleanupCount > 0) {
    appendInsightItem(
      list,
      "Treat future-cleanup notes as follow-up work, not current blockers",
      `${futureCleanupCount} grouped review finding${futureCleanupCount === 1 ? "" : "s"} describe later-major-upgrade cleanup rather than expected 2.8 breakage.`
    );
  }

  if (list.children.length === 0) {
    appendInsightItem(
      list,
      "Run one smoke test per grouped advisory finding",
      `Start with the ${groupedReview.length} review group${groupedReview.length === 1 ? "" : "s"} shown in the desktop summary and confirm each affected flow still behaves as expected.`
    );
  }
}

async function refreshFlowUsageInsights(report) {
  const key = reportKey(report);
  if (!report || (report.kind !== "MigrationReport" && report.kind !== "ValidationReport")) {
    resetFlowUsageState();
    renderFindingSections(report);
    renderReviewInsights(null);
    renderUpgradeTestChecklist(null);
    return;
  }

  const controllerReviewGroups = controllerServiceReviewEntries(report);
  if (controllerReviewGroups.length === 0) {
    resetFlowUsageState();
    renderFindingSections(report);
    renderReviewInsights(report);
    renderUpgradeTestChecklist(report);
    return;
  }

  state.flowUsageSummary = null;
  state.flowUsageStatus = "loading";
  state.flowUsageRequestKey = key;
  renderFindingSections(report);
  renderReviewInsights(report);
  renderUpgradeTestChecklist(report);

  const sourcePath = report.source?.path || "";
  const sourceFormat = report.source?.format || "";
  if (!sourcePath) {
    state.flowUsageSummary = {
      supported: false,
      sourcePath: "",
      sourceFormat,
      message: "The selected report did not include a source flow path for usage insight.",
      controllerServices: [],
    };
    state.flowUsageStatus = "unsupported";
    if (state.flowUsageRequestKey === key) {
      renderFindingSections(report);
      renderReviewInsights(report);
      renderUpgradeTestChecklist(report);
    }
    return;
  }

  try {
    const summary = await invoke("inspect_flow_usage", { path: sourcePath, sourceFormat });
    if (state.flowUsageRequestKey !== key) {
      return;
    }
    state.flowUsageSummary = summary;
    state.flowUsageStatus = summary?.supported ? "ready" : "unsupported";
  } catch (error) {
    if (state.flowUsageRequestKey !== key) {
      return;
    }
    state.flowUsageSummary = {
      supported: false,
      sourcePath,
      sourceFormat,
      message: String(error),
      controllerServices: [],
    };
    state.flowUsageStatus = "unsupported";
  }

  if (reportKey(state.latestReport) === key) {
    renderFindingSections(state.latestReport);
    renderReviewInsights(state.latestReport);
    renderUpgradeTestChecklist(state.latestReport);
  }
}

function formatActionPreview(action) {
  const params = action.params || {};
  switch (action.type) {
    case "rename-property":
      return `Rename property ${params.from || "old"} to ${params.to || "new"}.`;
    case "remove-property":
      return `Remove property ${params.name || "unknown"}.`;
    case "replace-component-type":
      return `Replace component type ${componentFamilyName(params.from || "old")} with ${componentFamilyName(params.to || "new")}.`;
    case "set-property":
      return `Set property ${params.property || "unknown"} to ${params.value || "value"}.`;
    case "set-property-if-absent":
      return `Create property ${params.property || "unknown"} with ${params.value || "value"} if it is missing.`;
    case "copy-property":
      return `Copy property ${params.from || "unknown"} into ${params.to || "unknown"}.`;
    case "update-bundle-coordinate":
      return "Update bundle coordinates to the target component bundle.";
    case "emit-parameter-scaffold":
      return `Create a parameter placeholder named ${params.parameterName || "parameter"} for the reviewed migration.`;
    default:
      return action.type;
  }
}

function previewDiffFromAction(action) {
  const params = action?.params || {};
  switch (action?.type) {
    case "rename-property":
      return {
        before: `Property: ${params.from || "old"}`,
        after: `Property: ${params.to || "new"}`,
      };
    case "remove-property":
      return {
        before: `Property: ${params.name || "unknown"}`,
        after: "Removed",
      };
    case "replace-component-type":
      return {
        before: `Type: ${componentFamilyName(params.from || "old")}`,
        after: `Type: ${componentFamilyName(params.to || "new")}`,
      };
    case "set-property":
      return {
        before: `Property: ${params.property || "unknown"}`,
        after: `Set to ${params.value || "value"}`,
      };
    case "set-property-if-absent":
      return {
        before: `Property: ${params.property || "unknown"}`,
        after: `Create ${params.value || "value"}`,
      };
    case "copy-property":
      return {
        before: `Property: ${params.from || "unknown"}`,
        after: `Copy to ${params.to || "unknown"}`,
      };
    default:
      return null;
  }
}

function renderRewritePreview(report) {
  const card = byId("rewritePreview");
  const list = byId("rewritePreviewList");
  card.hidden = true;
  list.innerHTML = "";

  if (!report || report.kind !== "MigrationReport" || !Array.isArray(report.findings)) {
    return;
  }

  const rewriteableFindings = report.findings.filter(
    (finding) => finding.class === "auto-fix" || finding.class === "assisted-rewrite"
  );
  if (rewriteableFindings.length === 0) {
    return;
  }

  rewriteableFindings.forEach((finding) => {
    const item = document.createElement("div");
    item.className = "preview-item";

    const title = document.createElement("div");
    title.className = "preview-title";
    title.textContent = finding.message;
    item.appendChild(title);

    const detail = findingDetailText(finding);
    if (detail) {
      const meta = document.createElement("div");
      meta.className = "preview-meta";
      meta.textContent = `${finding.class === "assisted-rewrite" ? "Assisted rewrite" : "Safe fix"} • ${detail}`;
      item.appendChild(meta);
    }

    const action = Array.isArray(finding.suggestedActions) && finding.suggestedActions.length > 0
      ? finding.suggestedActions[0]
      : null;
    const diff = previewDiffFromAction(action);
    if (diff) {
      const diffRow = document.createElement("div");
      diffRow.className = "diff-row";

      const before = document.createElement("span");
      before.className = "diff-chip before";
      before.textContent = diff.before;
      diffRow.appendChild(before);

      const after = document.createElement("span");
      after.className = "diff-chip after";
      after.textContent = diff.after;
      diffRow.appendChild(after);

      item.appendChild(diffRow);
    } else if (action) {
      const meta = document.createElement("div");
      meta.className = "preview-meta";
      meta.textContent = formatActionPreview(action);
      item.appendChild(meta);
    }

    list.appendChild(item);
  });

  if (report.preview?.truncated) {
    const meta = document.createElement("div");
    meta.className = "preview-meta";
    meta.textContent = "Showing a desktop preview only. Use the exported reports for the full rewriteable set.";
    list.appendChild(meta);
  }

  card.hidden = false;
}

async function renderReports(result) {
  const list = byId("reportLinks");
  const view = byId("reportView");
  list.innerHTML = "";
  view.classList.remove("empty");
  state.reports = result.reportPaths || [];
  state.latestResult = result;
  state.rewrittenArtifactPath = result.rewrittenArtifactPath || null;

  if (state.reports.length === 0) {
    list.textContent = "No reports generated.";
    resetResultOverview();
    byId("findingSections").innerHTML = "";
    byId("rewritePreview").hidden = true;
    view.classList.add("empty");
    view.textContent = "Run a command to load a report here.";
    return;
  }

  const bundle = await invoke("load_report_bundle", { reportPaths: state.reports });
  const structuredByKind = bundle.reportIndex || {};
  const jsonReport = bundle.primaryReport || null;
  state.reportGroups = bundle.groups || [];
  state.inlineViewLimitBytes = Number(bundle.inlineViewLimitBytes || 0);
  state.latestReport = jsonReport;
  state.reportIndex = structuredByKind;
  renderResultBanner(jsonReport, result, structuredByKind);
  renderResultOverview(jsonReport, result);
  renderPriorityCallout(jsonReport, structuredByKind);
  renderRunSteps(jsonReport);
  renderRewriteSummary(structuredByKind.rewriteReport || null);
  renderRewritePreview(jsonReport?.kind === "MigrationReport" ? jsonReport : structuredByKind.migrationReport || null);
  renderResultUtilities(result);
  renderActionSelection();
  refreshFlowUsageInsights(jsonReport).catch((error) => {
    state.flowUsageSummary = {
      supported: false,
      sourcePath: jsonReport?.source?.path || "",
      sourceFormat: jsonReport?.source?.format || "",
      message: String(error),
      controllerServices: [],
    };
    state.flowUsageStatus = "unsupported";
    renderFindingSections(state.latestReport);
    renderReviewInsights(state.latestReport);
    renderUpgradeTestChecklist(state.latestReport);
  });

  for (const group of state.reportGroups) {
    const card = document.createElement("div");
    card.className = "report-card";

    const head = document.createElement("div");
    head.className = "report-card-head";

    const title = document.createElement("div");
    title.className = "report-card-title";
    title.textContent = group.label;
    head.appendChild(title);

    const meta = document.createElement("div");
    meta.className = "report-card-meta";
    meta.textContent = group.md
      ? `Readable report available${group.mdSizeBytes ? ` • ${reportFileSizeLabel(group.mdSizeBytes)}` : ""}`
      : `Structured export only${group.jsonSizeBytes ? ` • ${reportFileSizeLabel(group.jsonSizeBytes)}` : ""}`;
    head.appendChild(meta);
    card.appendChild(head);

    const actions = document.createElement("div");
    actions.className = "report-card-actions";
    const primaryPath = group.mdPath || group.jsonPath;
    const primaryInlineSafe = group.mdPath ? group.mdInlineSafe : group.jsonInlineSafe;
    if (primaryPath) {
      const button = document.createElement("button");
      button.className = "button secondary";
      button.textContent = group.mdPath ? `View ${group.label} report` : `View ${group.label} export`;
      button.addEventListener("click", async () => {
        if (!primaryInlineSafe) {
          setReportViewMessage(
            `${group.label} report is large`,
            `This export is larger than the in-app preview limit. Use Open output folder if you want the full file on disk.`
          );
          return;
        }
        const content = await invoke("read_text_file", { path: primaryPath });
        setReportViewContent(primaryPath, content);
      });
      actions.appendChild(button);
    }
    card.appendChild(actions);

    if (group.jsonPath) {
      const advanced = document.createElement("details");
      advanced.className = "report-advanced";
      const summary = document.createElement("summary");
      summary.textContent = "Automation details";
      advanced.appendChild(summary);

      const advancedActions = document.createElement("div");
      advancedActions.className = "report-advanced-actions";
      const jsonButton = document.createElement("button");
      jsonButton.className = "button secondary";
      jsonButton.textContent = `View ${group.label} JSON`;
      jsonButton.addEventListener("click", async () => {
        if (!group.jsonInlineSafe) {
          setReportViewMessage(
            `${group.label} JSON is large`,
            `This export is larger than the in-app preview limit. Use Open output folder if you want the full JSON on disk.`
          );
          return;
        }
        const content = await invoke("read_text_file", { path: group.jsonPath });
        setReportViewContent(group.jsonPath, content);
      });
      advancedActions.appendChild(jsonButton);
      advanced.appendChild(advancedActions);
      card.appendChild(advanced);
    }

    list.appendChild(card);
  }

  setReportViewMessage(
    "Choose a report to view",
    "Large runs stay in summary mode by default so the desktop app remains responsive."
  );
}

async function runAction(action) {
  state.selectedAction = action;
  state.runningAction = action;
  state.rewrittenArtifactPath = null;
  state.reportIndex = {};
  state.latestReport = null;
  state.latestResult = null;
  resetFlowUsageState();
  renderActionSelection();

  const flow = selectedFlowCandidate();
  if (!flow) {
    setText("lastAction", "Choose a flow candidate first.");
    state.runningAction = null;
    renderActionSelection();
    return;
  }

  const request = {
    action,
    workspacePath: byId("workspacePath").value.trim(),
    binaryPath: byId("binarySelect").value,
    sourcePath: flow.path,
    sourceFormat: flow.sourceFormat,
    sourceVersion: byId("sourceVersion").value.trim(),
    targetVersion: byId("targetVersion").value.trim(),
    rulePackPaths: selectedRulePacks(),
    extensionsManifestPath: byId("manifestSelect").value || null,
    outputDir: byId("outputDir").value.trim(),
  };

  const validationError = validateActionRequest(action, request);
  if (validationError) {
    byId("stdoutView").textContent = validationError;
    setText("lastAction", `${titleAction(action)} not started.`);
    setResultBanner({
      variant: "warning",
      title: `${titleAction(action)} needs one more input`,
      body: validationError,
    });
    byId("resultHeadline").textContent = `${titleAction(action)} is ready when you are.`;
    byId("resultSubhead").textContent = validationError;
    byId("resultMetrics").innerHTML = "";
    byId("resultMeta").textContent = "No command was started.";
    byId("summaryBadges").innerHTML = "";
    byId("findingSections").innerHTML = "";
    byId("rewritePreview").hidden = true;
    renderReviewInsights(null);
    renderUpgradeTestChecklist(null);
    state.runningAction = null;
    renderActionSelection();
    return;
  }

  setText("lastAction", `Running ${action}...`);
  byId("stdoutView").textContent = "Running command...";
  byId("reportView").textContent = "Waiting for report output...";
  byId("reportLinks").textContent = "Generating reports...";
  byId("summaryBadges").innerHTML = "";
  byId("findingSections").innerHTML = "";
  byId("rewritePreview").hidden = true;
  renderReviewInsights(null);
  renderUpgradeTestChecklist(null);
  setUtilityActions([]);
  setResultBanner({
    variant: "warning",
    title: `Running ${titleAction(action)}`,
    body: "Preparing a structured summary from the generated report.",
  });
  renderPriorityCallout(null, {});
  renderRewriteSummary(null);
  byId("resultHeadline").textContent = `Running ${titleAction(action)}...`;
  byId("resultSubhead").textContent = "Preparing a structured summary from the generated report.";
  byId("resultMetrics").innerHTML = "";
  byId("resultMeta").textContent = "Waiting for report output.";

  try {
    const result = await invoke("run_cli_action", { request });
    byId("stdoutView").textContent = [result.stdout, result.stderr].filter(Boolean).join("\n\n") || "Command completed with no console output.";
    setText("lastAction", `${titleAction(action)} finished in ${result.durationMs} ms.`);
    renderBadges(result);
    await renderReports(result);
  } catch (error) {
    byId("stdoutView").textContent = String(error);
    setText("lastAction", `${titleAction(action)} failed.`);
    setResultBanner({
      variant: "danger",
      title: `${titleAction(action)} failed`,
      body: "Review the command output below for the exact error.",
    });
  } finally {
    state.runningAction = null;
    renderActionSelection();
  }
}

function prepareFreshOutputDir() {
  const workspacePath = byId("workspacePath").value.trim();
  if (!workspacePath) {
    return;
  }
  byId("outputDir").value = defaultOutputDir(workspacePath);
  state.reports = [];
  state.reportIndex = {};
  state.latestReport = null;
  state.latestResult = null;
  state.rewrittenArtifactPath = null;
  resetFlowUsageState();
  byId("reportLinks").textContent = "Start a fresh run to generate new reports here.";
  byId("reportView").classList.add("empty");
  byId("reportView").textContent = "Start a fresh run to load a report here.";
  byId("stdoutView").textContent = "Waiting for a command...";
  byId("summaryBadges").innerHTML = "";
  byId("findingSections").innerHTML = "";
  byId("rewritePreview").hidden = true;
  renderReviewInsights(null);
  renderUpgradeTestChecklist(null);
  setText("lastAction", "Fresh output folder ready.");
  resetResultOverview();
}

async function openOutputFolder() {
  const path = byId("outputDir").value.trim();
  if (!path) {
    return;
  }
  await openPath(path, true);
}

async function scanWorkspace() {
  const path = byId("workspacePath").value.trim();
  const data = await invoke("scan_workspace", { path: path || null });
  renderWorkspace(data);
}

async function bootstrap() {
  const data = await invoke("bootstrap_state");
  renderWorkspace(data);
}

document.getElementById("scanButton").addEventListener("click", scanWorkspace);
byId("openOutputButton").addEventListener("click", () => {
  openOutputFolder().catch((error) => {
    byId("stdoutView").textContent = String(error);
    setText("lastAction", "Could not open output folder.");
  });
});
byId("freshOutputButton").addEventListener("click", prepareFreshOutputDir);
document.querySelectorAll("[data-action]").forEach((button) => {
  button.addEventListener("click", () => {
    state.selectedAction = button.dataset.action;
    renderActionSelection();
    runAction(button.dataset.action);
  });
});
byId("flowSelect").addEventListener("change", () => {
  applyFlowDefaults();
});
byId("manifestSelect").addEventListener("change", () => {
  setText("manifestNote", selectedManifestLabel());
  renderValidateAffordance();
});
byId("rulePackSelect").addEventListener("change", () => {
  const count = selectedRulePacks().length;
  setText(
    "rulePackNote",
    count > 0
      ? `Using ${count} manually selected rule pack${count === 1 ? "" : "s"}.`
      : "Pick source and target versions to use the built-in upgrade coverage for this path."
  );
});
byId("sourceVersion").addEventListener("input", () => {
  sourceVersionInput().dataset.mode = "manual";
  const flow = selectedFlowCandidate();
  saveRememberedSourceVersion(flow?.path, byId("sourceVersion").value.trim());
  autoSelectRulePacks();
  renderSourceVersionNote();
  renderFlowDetails();
});
byId("targetVersion").addEventListener("input", () => {
  autoSelectRulePacks();
  autoSelectManifest();
  setText("manifestNote", selectedManifestLabel());
  renderFlowDetails();
  renderValidateAffordance();
});

bootstrap().catch((error) => {
  byId("stdoutView").textContent = String(error);
  setText("lastAction", "Initial scan failed.");
});

renderActionSelection();
byId("resultNextAction").addEventListener("click", () => {
  if (state.nextAction?.onClick) {
    state.nextAction.onClick();
  }
});
