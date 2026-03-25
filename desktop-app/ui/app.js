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
  selectedAction: "run",
  runningAction: null,
  nextAction: null,
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

function reportGroupName(path) {
  const name = String(path || "").split("/").pop() || "";
  const base = name.replace(/\.(md|json)$/, "");
  const labelMap = {
    "migration-report": "Analyze",
    "rewrite-report": "Rewrite",
    "validation-report": "Validate",
    "run-report": "Run",
  };
  return labelMap[base] || baseName(base || name || "Report");
}

function groupReportPaths(paths) {
  const groups = new Map();
  for (const reportPath of paths) {
    const key = reportGroupName(reportPath);
    if (!groups.has(key)) {
      groups.set(key, { label: key, md: null, json: null });
    }
    const group = groups.get(key);
    if (reportPath.endsWith(".md")) {
      group.md = reportPath;
    } else if (reportPath.endsWith(".json")) {
      group.json = reportPath;
    }
  }
  return ["Analyze", "Rewrite", "Validate", "Run"]
    .map((label) => groups.get(label))
    .filter(Boolean);
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

function renderFlowDetails() {
  const flow = selectedFlowCandidate();
  const target = byId("targetVersion").value.trim();
  if (!flow) {
    setText("flowDetails", "Choose a flow to see format, source version, and target details.");
    return;
  }

  const detectedSource = flow.detectedVersion || byId("sourceVersion").value.trim() || "enter manually";
  const targetLabel = target || "choose a target";
  setText(
    "flowDetails",
    `Format: ${sourceFormatLabel(flow.sourceFormat)} • Source: ${detectedSource} • Target: ${targetLabel}`
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
  if (flow?.detectedVersion) {
    setText("sourceVersionNote", `Source version auto-detected: ${flow.detectedVersion}`);
    return;
  }
  setText("sourceVersionNote", "No embedded source version found in this flow. Enter the source NiFi version manually to continue.");
}

function autoSelectManifest() {
  const targetMinor = versionMinor(byId("targetVersion").value);
  const select = byId("manifestSelect");
  if (!select) {
    return;
  }

  let matched = false;
  for (const opt of select.options) {
    if (!opt.value) {
      opt.selected = !targetMinor;
      continue;
    }
    const isMatch = targetMinor && opt.value.includes(`nifi-${targetMinor}`);
    opt.selected = Boolean(isMatch);
    matched = matched || Boolean(isMatch);
  }

  if (!matched) {
    select.value = "";
  }
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
  } else if ((sourceInput.dataset.mode || "") === "auto") {
    sourceInput.value = "";
    sourceInput.dataset.mode = "";
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
      const versionHint = candidate.detectedVersion ? ` · ${candidate.detectedVersion}` : "";
      const label = `${candidate.kindLabel} — ${candidate.displayPath}${versionHint}`;
      flowSelect.appendChild(option(label, candidate.path, index === 0));
    });
  }

  const manifestSelect = byId("manifestSelect");
  manifestSelect.innerHTML = "";
  manifestSelect.appendChild(option("None", "", true));
  state.manifests.forEach((candidate) => {
    manifestSelect.appendChild(option(candidate.displayPath, candidate.path));
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
      ? reportIndex.MigrationReport
      : report.summary?.validationBlocked
        ? reportIndex.ValidationReport
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
      setResultBanner({ variant: "warning", title: "Review needed", body: `This flow can move forward after ${pluralize(review, "review item", "review items")} are checked.` });
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

  const sourceVersion = report.source?.nifiVersion || "unknown";
  const targetVersion = report.target?.nifiVersion || "unknown";
  const sourceLabel = displaySourceLabel(report);

  if (report.kind === "MigrationReport" || report.kind === "ValidationReport") {
    const byClass = report.summary?.byClass || {};
    const blocked = byClass["blocked"] || 0;
    const autoFix = byClass["auto-fix"] || 0;
    const assisted = byClass["assisted-rewrite"] || 0;
    const manual = (byClass["manual-change"] || 0) + (byClass["manual-inspection"] || 0);
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
      headline.textContent = `Review needed: ${manual} change${manual === 1 ? "" : "s"}`;
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
    metrics.appendChild(metricCard("Review items", manual));
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
  renderRunSteps(null);
  renderActionSelection();
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
    parts.push(baseName(component.type));
  }
  if (component?.scope) {
    parts.push(component.scope);
  }
  return parts.join(" • ");
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
    const section = document.createElement("details");
    section.className = `result-card finding-section section-${kind}`;
    section.dataset.kind = kind;
    section.open = findings.length > 0 && kind !== "info";

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
    const meta = document.createElement("span");
    meta.className = "section-summary";
    meta.textContent = summarizeSection(kind, findings.length);
    summary.appendChild(titleWrap);
    summary.appendChild(meta);
    section.appendChild(summary);

    const body = document.createElement("div");
    body.className = "finding-section-body";

    if (findings.length === 0) {
      const empty = document.createElement("div");
      empty.className = "finding-item-meta";
      empty.textContent = summarizeSection(kind, 0);
      body.appendChild(empty);
    } else {
      findings.forEach((finding) => {
        const item = document.createElement("div");
        item.className = "finding-item";

        const itemTitle = document.createElement("div");
        itemTitle.className = "finding-item-title";
        itemTitle.textContent = finding.message;
        item.appendChild(itemTitle);

        const detail = findingDetailText(finding);
        if (detail) {
          const metaLine = document.createElement("div");
          metaLine.className = "finding-item-meta";
          metaLine.textContent = detail;
          item.appendChild(metaLine);
        }

        if (finding.notes) {
          const notes = document.createElement("div");
          notes.className = "finding-item-meta";
          notes.textContent = finding.notes;
          item.appendChild(notes);
        }

        if (Array.isArray(finding.suggestedActions) && finding.suggestedActions.length > 0) {
          const suggestions = document.createElement("div");
          suggestions.className = "finding-item-meta";
          suggestions.textContent = `Suggested next steps: ${finding.suggestedActions.map(formatActionPreview).join(" ")}`;
          item.appendChild(suggestions);
        }

        body.appendChild(item);
      });
    }

    section.appendChild(body);
    root.appendChild(section);
  });
}

function formatActionPreview(action) {
  const params = action.params || {};
  switch (action.type) {
    case "rename-property":
      return `Rename property ${params.from || "old"} to ${params.to || "new"}.`;
    case "remove-property":
      return `Remove property ${params.name || "unknown"}.`;
    case "replace-component-type":
      return `Replace component type ${baseName(params.from || "old")} with ${baseName(params.to || "new")}.`;
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
        before: `Type: ${baseName(params.from || "old")}`,
        after: `Type: ${baseName(params.to || "new")}`,
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

  const structuredByPath = {};
  const structuredByKind = {};
  for (const path of state.reports.filter((candidate) => candidate.endsWith(".json"))) {
    try {
      const parsed = JSON.parse(await invoke("read_text_file", { path }));
      structuredByPath[path] = parsed;
      if (parsed?.kind && !structuredByKind[parsed.kind]) {
        structuredByKind[parsed.kind] = parsed;
      }
    } catch (error) {
      // Ignore non-parseable exports and keep the readable report flow working.
    }
  }

  const jsonPath = preferredJsonReportPath(state.reports);
  const jsonReport = jsonPath ? structuredByPath[jsonPath] || null : null;
  state.latestReport = jsonReport;
  state.reportIndex = structuredByKind;
  renderResultBanner(jsonReport, result, structuredByKind);
  renderResultOverview(jsonReport, result);
  renderPriorityCallout(jsonReport, structuredByKind);
  renderRunSteps(jsonReport);
  renderFindingSections(jsonReport);
  renderRewriteSummary(structuredByKind.RewriteReport || null);
  renderRewritePreview(jsonReport?.kind === "MigrationReport" ? jsonReport : structuredByKind.MigrationReport || null);
  renderResultUtilities(result);
  renderActionSelection();

  const groups = groupReportPaths(state.reports);
  for (const group of groups) {
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
    meta.textContent = group.md ? "Readable report available" : "Structured export only";
    head.appendChild(meta);
    card.appendChild(head);

    const actions = document.createElement("div");
    actions.className = "report-card-actions";
    const primaryPath = group.md || group.json;
    if (primaryPath) {
      const button = document.createElement("button");
      button.className = "button secondary";
      button.textContent = group.md ? `View ${group.label} report` : `View ${group.label} export`;
      button.addEventListener("click", async () => {
        const content = await invoke("read_text_file", { path: primaryPath });
        setReportViewContent(primaryPath, content);
      });
      actions.appendChild(button);
    }
    card.appendChild(actions);

    if (group.json) {
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
        const content = await invoke("read_text_file", { path: group.json });
        setReportViewContent(group.json, content);
      });
      advancedActions.appendChild(jsonButton);
      advanced.appendChild(advancedActions);
      card.appendChild(advanced);
    }

    list.appendChild(card);
  }

  let defaultReport = state.reports.find((path) => path.endsWith(".md")) || state.reports[0];
  if (jsonReport?.kind === "RunReport") {
    defaultReport =
      state.reports.find((path) => path.endsWith("run-report.md")) ||
      state.reports.find((path) => path.endsWith("run-report.json")) ||
      defaultReport;
  } else if (jsonReport?.kind === "RewriteReport") {
    defaultReport =
      state.reports.find((path) => path.endsWith("rewrite-report.md")) ||
      state.reports.find((path) => path.endsWith("rewrite-report.json")) ||
      defaultReport;
  } else if (jsonReport?.kind === "ValidationReport") {
    defaultReport =
      state.reports.find((path) => path.endsWith("validation-report.md")) ||
      state.reports.find((path) => path.endsWith("validation-report.json")) ||
      defaultReport;
  } else if (jsonReport?.kind === "MigrationReport") {
    defaultReport =
      state.reports.find((path) => path.endsWith("migration-report.md")) ||
      state.reports.find((path) => path.endsWith("migration-report.json")) ||
      defaultReport;
  }
  const content = await invoke("read_text_file", { path: defaultReport });
  setReportViewContent(defaultReport, content);
}

async function runAction(action) {
  state.selectedAction = action;
  state.runningAction = action;
  state.rewrittenArtifactPath = null;
  state.reportIndex = {};
  state.latestReport = null;
  state.latestResult = null;
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

  setText("lastAction", `Running ${action}...`);
  byId("stdoutView").textContent = "Running command...";
  byId("reportView").textContent = "Waiting for report output...";
  byId("reportLinks").textContent = "Generating reports...";
  byId("summaryBadges").innerHTML = "";
  byId("findingSections").innerHTML = "";
  byId("rewritePreview").hidden = true;
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
  byId("reportLinks").textContent = "Start a fresh run to generate new reports here.";
  byId("reportView").classList.add("empty");
  byId("reportView").textContent = "Start a fresh run to load a report here.";
  byId("stdoutView").textContent = "Waiting for a command...";
  byId("summaryBadges").innerHTML = "";
  byId("findingSections").innerHTML = "";
  byId("rewritePreview").hidden = true;
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
  autoSelectRulePacks();
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
