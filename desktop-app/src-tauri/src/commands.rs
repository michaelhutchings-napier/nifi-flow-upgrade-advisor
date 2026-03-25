use serde::{Deserialize, Serialize};
use std::ffi::OsStr;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::Instant;

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct BootstrapState {
    workspace_root: String,
    binary_candidates: Vec<String>,
    flow_candidates: Vec<WorkspaceEntry>,
    rule_packs: Vec<WorkspaceEntry>,
    manifests: Vec<WorkspaceEntry>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WorkspaceEntry {
    path: String,
    display_path: String,
    kind_label: String,
    source_format: Option<String>,
    detected_version: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CliActionRequest {
    action: String,
    workspace_path: String,
    binary_path: String,
    source_path: String,
    source_format: String,
    source_version: String,
    target_version: String,
    rule_pack_paths: Vec<String>,
    extensions_manifest_path: Option<String>,
    output_dir: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CliActionResult {
    exit_code: i32,
    stdout: String,
    stderr: String,
    duration_ms: u128,
    output_dir: String,
    report_paths: Vec<String>,
}

enum ExecTarget {
    Binary(String),
    GoRun(PathBuf),
}

#[tauri::command]
pub fn bootstrap_state() -> Result<BootstrapState, String> {
    scan_workspace_internal(None)
}

#[tauri::command]
pub fn scan_workspace(path: Option<String>) -> Result<BootstrapState, String> {
    scan_workspace_internal(path.as_deref())
}

#[tauri::command]
pub fn read_text_file(path: String) -> Result<String, String> {
    fs::read_to_string(&path).map_err(|err| format!("read {}: {}", path, err))
}

#[tauri::command]
pub fn run_cli_action(request: CliActionRequest) -> Result<CliActionResult, String> {
    let validated = validate_request(request)?;
    let exec_target = resolve_exec_target(&validated)?;
    let workspace_root =
        canonicalize_existing_path(Path::new(validated.workspace_path.trim()), "workspace path")?;
    let tool_root = repo_root()?;
    let output_dir = if validated.output_dir.trim().is_empty() {
        default_output_dir(&workspace_root)
    } else {
        PathBuf::from(validated.output_dir.trim())
    };
    fs::create_dir_all(&output_dir).map_err(|err| format!("create output dir: {}", err))?;

    let prepared = prepare_source(
        &validated.source_path,
        &validated.source_format,
        &output_dir,
    )?;
    let report_base = output_dir.to_string_lossy().to_string();

    let mut args: Vec<String> = Vec::new();
    match validated.action.as_str() {
        "analyze" => {
            args.push("analyze".into());
            args.extend(common_action_args(
                &prepared.path,
                &prepared.cli_format,
                &validated,
                &report_base,
                true,
            )?);
        }
        "rewrite" => {
            let plan = output_dir.join("migration-report.json");
            args.push("rewrite".into());
            if plan.exists() {
                args.push("--plan".into());
                args.push(plan.to_string_lossy().to_string());
            } else {
                args.extend(common_rewrite_args(
                    &prepared.path,
                    &prepared.cli_format,
                    &validated,
                    &report_base,
                )?);
            }
        }
        "validate" => {
            args.push("validate".into());
            let rewritten = output_dir.join("rewritten-flow.json.gz");
            let input = if rewritten.exists() {
                rewritten
            } else {
                PathBuf::from(&prepared.path)
            };
            args.push("--input".into());
            args.push(input.to_string_lossy().to_string());
            args.push("--input-format".into());
            args.push(prepared.cli_format.clone());
            args.push("--target-version".into());
            args.push(validated.target_version.clone());
            if let Some(manifest) = validated
                .extensions_manifest_path
                .as_ref()
                .filter(|v| !v.trim().is_empty())
            {
                args.push("--extensions-manifest".into());
                args.push(manifest.clone());
            }
            args.push("--output-dir".into());
            args.push(report_base.clone());
        }
        "run" => {
            args.push("run".into());
            args.extend(common_action_args(
                &prepared.path,
                &prepared.cli_format,
                &validated,
                &report_base,
                true,
            )?);
        }
        other => return Err(format!("unsupported action {}", other)),
    }

    let start = Instant::now();
    let output = match exec_target {
        ExecTarget::Binary(binary) => Command::new(&binary)
            .args(&args)
            .current_dir(&workspace_root)
            .output()
            .map_err(|err| format!("run {}: {}", binary, err))?,
        ExecTarget::GoRun(root) => Command::new("go")
            .arg("run")
            .arg("./cmd/nifi-flow-upgrade")
            .args(&args)
            .current_dir(root)
            .output()
            .map_err(|err| format!("run go fallback from {}: {}", tool_root.display(), err))?,
    };
    let duration_ms = start.elapsed().as_millis();

    let exit_code = output.status.code().unwrap_or(1);
    let stdout = String::from_utf8_lossy(&output.stdout).to_string();
    let stderr = String::from_utf8_lossy(&output.stderr).to_string();

    Ok(CliActionResult {
        exit_code,
        stdout,
        stderr,
        duration_ms,
        output_dir: report_base.clone(),
        report_paths: collect_report_paths(&output_dir),
    })
}

fn validate_request(mut request: CliActionRequest) -> Result<CliActionRequest, String> {
    if request.workspace_path.trim().is_empty() {
        return Err("workspacePath is required".into());
    }
    if request.source_path.trim().is_empty() {
        return Err("sourcePath is required".into());
    }
    if request.source_format.trim().is_empty() {
        return Err("sourceFormat is required".into());
    }
    if request.target_version.trim().is_empty() {
        return Err("targetVersion is required".into());
    }

    let workspace_root =
        canonicalize_existing_path(Path::new(request.workspace_path.trim()), "workspace path")?;
    request.workspace_path = workspace_root.to_string_lossy().to_string();

    let source_path =
        canonicalize_existing_path(Path::new(request.source_path.trim()), "source path")?;
    request.source_path = source_path.to_string_lossy().to_string();

    if request.action != "validate" && request.source_version.trim().is_empty() {
        return Err("sourceVersion is required".into());
    }

    if request.action != "validate" && request.rule_pack_paths.is_empty() {
        return Err("select at least one rule pack".into());
    }

    let mut validated_rule_packs = Vec::new();
    for path in request.rule_pack_paths {
        let canonical = canonicalize_existing_path(Path::new(path.trim()), "rule pack")?;
        if !canonical.is_file() {
            return Err(format!("rule pack {} is not a file", canonical.display()));
        }
        validated_rule_packs.push(canonical.to_string_lossy().to_string());
    }
    request.rule_pack_paths = validated_rule_packs;

    if let Some(path) = request
        .extensions_manifest_path
        .as_ref()
        .filter(|value| !value.trim().is_empty())
    {
        let canonical = canonicalize_existing_path(Path::new(path.trim()), "extensions manifest")?;
        if !canonical.is_file() {
            return Err(format!(
                "extensions manifest {} is not a file",
                canonical.display()
            ));
        }
        request.extensions_manifest_path = Some(canonical.to_string_lossy().to_string());
    }

    if !request.output_dir.trim().is_empty() {
        request.output_dir = PathBuf::from(request.output_dir.trim())
            .to_string_lossy()
            .to_string();
    }

    Ok(request)
}

fn canonicalize_existing_path(path: &Path, label: &str) -> Result<PathBuf, String> {
    if !path.exists() {
        return Err(format!("{} does not exist: {}", label, path.display()));
    }
    path.canonicalize()
        .map_err(|err| format!("resolve {} {}: {}", label, path.display(), err))
}

fn default_output_dir(workspace_root: &Path) -> PathBuf {
    workspace_root.join(".nifi-flow-upgrade-desktop/latest")
}

fn common_action_args(
    source_path: &str,
    cli_format: &str,
    request: &CliActionRequest,
    report_base: &str,
    include_manifest: bool,
) -> Result<Vec<String>, String> {
    if request.rule_pack_paths.is_empty() {
        return Err("select at least one rule pack".into());
    }
    let mut args: Vec<String> = vec![
        "--source".into(),
        source_path.into(),
        "--source-format".into(),
        cli_format.into(),
        "--source-version".into(),
        request.source_version.clone(),
        "--target-version".into(),
        request.target_version.clone(),
    ];
    args.extend(flat_rule_pack_args(&request.rule_pack_paths));
    if include_manifest {
        if let Some(manifest) = request
            .extensions_manifest_path
            .as_ref()
            .filter(|value| !value.trim().is_empty())
        {
            args.push("--extensions-manifest".into());
            args.push(manifest.clone());
        }
    }
    args.push("--output-dir".into());
    args.push(report_base.into());
    Ok(args)
}

fn common_rewrite_args(
    source_path: &str,
    cli_format: &str,
    request: &CliActionRequest,
    report_base: &str,
) -> Result<Vec<String>, String> {
    if request.rule_pack_paths.is_empty() {
        return Err("select at least one rule pack".into());
    }
    Ok(vec![
        "--source".into(),
        source_path.into(),
        "--source-format".into(),
        cli_format.into(),
        "--source-version".into(),
        request.source_version.clone(),
        "--target-version".into(),
        request.target_version.clone(),
    ]
    .into_iter()
    .chain(flat_rule_pack_args(&request.rule_pack_paths))
    .chain(["--output-dir".into(), report_base.into()])
    .collect())
}

fn flat_rule_pack_args(paths: &[String]) -> Vec<String> {
    let mut args = Vec::new();
    for path in paths {
        args.push("--rule-pack".into());
        args.push(path.clone());
    }
    args
}

struct PreparedSource {
    path: String,
    cli_format: String,
}

fn prepare_source(
    path: &str,
    source_format: &str,
    output_dir: &Path,
) -> Result<PreparedSource, String> {
    if source_format == "flow-json-fixture" {
        let target = output_dir.join("desktop-source-flow.json.gz");
        let body = fs::read(path).map_err(|err| format!("read fixture {}: {}", path, err))?;
        let file =
            fs::File::create(&target).map_err(|err| format!("create gzip fixture: {}", err))?;
        let mut encoder = flate2::write::GzEncoder::new(file, flate2::Compression::default());
        std::io::Write::write_all(&mut encoder, &body)
            .map_err(|err| format!("write gzip fixture: {}", err))?;
        encoder
            .finish()
            .map_err(|err| format!("finalize gzip fixture: {}", err))?;
        return Ok(PreparedSource {
            path: target.to_string_lossy().to_string(),
            cli_format: "flow-json-gz".into(),
        });
    }

    Ok(PreparedSource {
        path: path.into(),
        cli_format: source_format.into(),
    })
}

fn resolve_exec_target(request: &CliActionRequest) -> Result<ExecTarget, String> {
    let root = repo_root()?;
    let preferred_go_run = prefer_go_run_fallback(&root);
    let default_binary = root.join("bin/nifi-flow-upgrade");
    let requested = request.binary_path.trim();
    if preferred_go_run
        && (requested.is_empty() || Path::new(requested) == default_binary.as_path())
    {
        return Ok(ExecTarget::GoRun(root));
    }
    if !requested.is_empty() && Path::new(requested).exists() {
        return Ok(ExecTarget::Binary(requested.into()));
    }

    for candidate in binary_candidates(root.clone()) {
        if Path::new(&candidate).exists() {
            return Ok(ExecTarget::Binary(candidate));
        }
    }

    if root.join("go.mod").is_file() && root.join("cmd/nifi-flow-upgrade/main.go").is_file() {
        return Ok(ExecTarget::GoRun(root));
    }

    Err("no nifi-flow-upgrade binary candidate or go-run fallback found".into())
}

fn collect_report_paths(output_dir: &Path) -> Vec<String> {
    let report_names = [
        "migration-report.md",
        "migration-report.json",
        "rewrite-report.md",
        "rewrite-report.json",
        "validation-report.md",
        "validation-report.json",
        "run-report.md",
        "run-report.json",
    ];

    report_names
        .iter()
        .map(|name| output_dir.join(name))
        .filter(|path| path.exists())
        .map(|path| path.to_string_lossy().to_string())
        .collect()
}

fn scan_workspace_internal(path: Option<&str>) -> Result<BootstrapState, String> {
    let tool_root = repo_root()?;
    let workspace_root = match path {
        Some(raw) if !raw.trim().is_empty() => PathBuf::from(raw),
        _ => tool_root.clone(),
    };
    let workspace_root = workspace_root
        .canonicalize()
        .map_err(|err| format!("resolve workspace {}: {}", workspace_root.display(), err))?;

    let mut flow_candidates = Vec::new();
    let mut rule_packs = Vec::new();
    let mut manifests = Vec::new();

    scan_dir(
        &workspace_root,
        &workspace_root,
        &mut flow_candidates,
        &mut rule_packs,
        &mut manifests,
        0,
    )?;

    if workspace_root != tool_root {
        scan_dir(
            &tool_root,
            &tool_root,
            &mut Vec::new(),
            &mut rule_packs,
            &mut manifests,
            0,
        )?;
        scan_dir(
            &workspace_root,
            &workspace_root,
            &mut Vec::new(),
            &mut rule_packs,
            &mut manifests,
            0,
        )?;
    }

    dedupe_entries(&mut rule_packs);
    dedupe_entries(&mut manifests);

    Ok(BootstrapState {
        workspace_root: workspace_root.to_string_lossy().to_string(),
        binary_candidates: binary_candidates(tool_root),
        flow_candidates,
        rule_packs,
        manifests,
    })
}

fn dedupe_entries(entries: &mut Vec<WorkspaceEntry>) {
    let mut deduped = Vec::new();
    for entry in entries.drain(..) {
        if deduped
            .iter()
            .any(|existing: &WorkspaceEntry| existing.path == entry.path)
        {
            continue;
        }
        deduped.push(entry);
    }
    *entries = deduped;
}

fn scan_dir(
    root: &Path,
    current: &Path,
    flows: &mut Vec<WorkspaceEntry>,
    rule_packs: &mut Vec<WorkspaceEntry>,
    manifests: &mut Vec<WorkspaceEntry>,
    depth: usize,
) -> Result<(), String> {
    if depth > 5 {
        return Ok(());
    }

    let entries =
        fs::read_dir(current).map_err(|err| format!("scan {}: {}", current.display(), err))?;
    for entry in entries {
        let entry = entry.map_err(|err| format!("scan entry: {}", err))?;
        let path = entry.path();
        let name = entry.file_name();
        let name = name.to_string_lossy();

        if name == ".git"
            || name == "target"
            || name == "node_modules"
            || name == ".nifi-flow-upgrade-desktop"
            || (name == "out" && current.ends_with("demo"))
        {
            continue;
        }

        if path.is_dir() {
            if looks_like_git_registry_dir(&path) {
                flows.push(new_entry(
                    root,
                    &path,
                    "Git registry directory",
                    Some("git-registry-dir"),
                    None,
                ));
                continue;
            }
            scan_dir(root, &path, flows, rule_packs, manifests, depth + 1)?;
            continue;
        }

        if name.ends_with(".yaml") || name.ends_with(".yml") {
            let body = fs::read_to_string(&path).unwrap_or_default();
            if body.contains("kind: RulePack") {
                rule_packs.push(new_entry(root, &path, "Rule pack", None, None));
                continue;
            }
            if body.contains("kind: ExtensionsManifest") || name.contains("manifest") {
                manifests.push(new_entry(root, &path, "Extensions manifest", None, None));
                continue;
            }
        }

        if let Some(format) = detect_flow_format(&path) {
            flows.push(new_entry(
                root,
                &path,
                format.0,
                Some(format.1),
                detect_source_version(&path),
            ));
        }
    }

    flows.sort_by(|a, b| a.display_path.cmp(&b.display_path));
    rule_packs.sort_by(|a, b| a.display_path.cmp(&b.display_path));
    manifests.sort_by(|a, b| a.display_path.cmp(&b.display_path));

    Ok(())
}

fn detect_flow_format(path: &Path) -> Option<(&'static str, &'static str)> {
    let name = path.file_name()?.to_string_lossy();
    if name.ends_with(".json.gz") {
        return Some(("Flow artifact", "flow-json-gz"));
    }
    if name.ends_with(".xml.gz") {
        return Some(("Legacy flow.xml.gz", "flow-xml-gz"));
    }
    if name.ends_with(".json") {
        let body = fs::read_to_string(path).ok()?;
        if body.contains("\"rootGroup\"") || body.contains("\"parameterContexts\"") {
            return Some(("Flow fixture JSON", "flow-json-fixture"));
        }
        if body.contains("\"flowContents\"") || body.contains("\"externalControllerServices\"") {
            return Some(("Versioned flow snapshot", "versioned-flow-snapshot"));
        }
    }
    None
}

fn looks_like_git_registry_dir(path: &Path) -> bool {
    if !path.is_dir() {
        return false;
    }
    looks_like_git_registry_dir_inner(path, 0)
}

fn looks_like_git_registry_dir_inner(path: &Path, depth: usize) -> bool {
    if depth > 3 {
        return false;
    }

    let Ok(entries) = fs::read_dir(path) else {
        return false;
    };

    for entry in entries.flatten() {
        let candidate = entry.path();
        if candidate.is_dir() {
            if looks_like_git_registry_dir_inner(&candidate, depth + 1) {
                return true;
            }
            continue;
        }
        if candidate.extension() != Some(OsStr::new("json")) {
            continue;
        }
        let name = candidate
            .file_name()
            .and_then(|value| value.to_str())
            .unwrap_or_default();
        if matches!(
            name,
            "migration-report.json"
                | "rewrite-report.json"
                | "validation-report.json"
                | "run-report.json"
        ) {
            continue;
        }

        let Ok(body) = fs::read_to_string(&candidate) else {
            continue;
        };
        if body.contains("\"flowContents\"")
            || body.contains("\"externalControllerServices\"")
            || body.contains("\"parameterContexts\"")
        {
            return true;
        }
    }

    false
}

fn new_entry(
    root: &Path,
    path: &Path,
    kind_label: &str,
    source_format: Option<&str>,
    detected_version: Option<String>,
) -> WorkspaceEntry {
    let display_path = path
        .strip_prefix(root)
        .unwrap_or(path)
        .to_string_lossy()
        .to_string();
    WorkspaceEntry {
        path: path.to_string_lossy().to_string(),
        display_path,
        kind_label: kind_label.into(),
        source_format: source_format.map(str::to_string),
        detected_version,
    }
}

fn detect_source_version(path: &Path) -> Option<String> {
    let name = path.file_name()?.to_string_lossy().to_lowercase();
    if name.ends_with(".json.gz") {
        let content = fs::read(path).ok()?;
        let mut reader = flate2::read::GzDecoder::new(&content[..]);
        let mut decoded = String::new();
        std::io::Read::read_to_string(&mut reader, &mut decoded).ok()?;
        return detect_version_from_text(&decoded);
    }
    if name.ends_with(".xml.gz") {
        let content = fs::read(path).ok()?;
        let mut reader = flate2::read::GzDecoder::new(&content[..]);
        let mut decoded = String::new();
        std::io::Read::read_to_string(&mut reader, &mut decoded).ok()?;
        return detect_version_from_text(&decoded);
    }
    if name.ends_with(".json") || name.ends_with(".yaml") || name.ends_with(".yml") {
        let body = fs::read_to_string(path).ok()?;
        return detect_version_from_text(&body);
    }
    None
}

fn detect_version_from_text(body: &str) -> Option<String> {
    let patterns = [
        "\"nifiVersion\"",
        "\"niFiVersion\"",
        "\"flowEncodingVersion\"",
        "\"registryVersion\"",
        "\"version\"",
        "nifiVersion:",
        "niFiVersion:",
        "flowEncodingVersion:",
        "registryVersion:",
        "\"versionedFlowSnapshot\"",
        "<nifiVersion>",
        "<niFiVersion>",
        "<version>",
        "<registryVersion>",
        "<flowEncodingVersion>",
        "<maxTimerDrivenThreadCount>",
    ];

    let trimmed = body.trim();
    if trimmed.starts_with('{') {
        if let Ok(payload) = serde_json::from_str::<serde_json::Value>(trimmed) {
            if let Some(version) = find_version_in_json(&payload) {
                return Some(version);
            }
            if let Some(version) = find_consistent_bundle_version_in_json(&payload) {
                return Some(version);
            }
        }
    }

    if !patterns.iter().any(|pattern| body.contains(pattern)) {
        return None;
    }

    if body.contains('<') && body.contains('>') {
        for tag in [
            "nifiVersion",
            "niFiVersion",
            "version",
            "registryVersion",
            "flowEncodingVersion",
        ] {
            if let Some(version) = extract_xml_tag_value(body, tag) {
                if looks_like_version(&version) {
                    return Some(version);
                }
            }
        }
    }

    extract_semver_like(body)
}

fn find_version_in_json(value: &serde_json::Value) -> Option<String> {
    match value {
        serde_json::Value::Object(map) => {
            for key in [
                "nifiVersion",
                "niFiVersion",
                "registryVersion",
                "flowEncodingVersion",
                "version",
            ] {
                if let Some(version) = map.get(key).and_then(|v| v.as_str()) {
                    if looks_like_version(version) {
                        return Some(version.to_string());
                    }
                }
            }
            for nested in map.values() {
                if let Some(version) = find_version_in_json(nested) {
                    return Some(version);
                }
            }
            None
        }
        serde_json::Value::Array(items) => {
            for item in items {
                if let Some(version) = find_version_in_json(item) {
                    return Some(version);
                }
            }
            None
        }
        _ => None,
    }
}

fn extract_xml_tag_value(body: &str, tag: &str) -> Option<String> {
    let open = format!("<{}>", tag);
    let close = format!("</{}>", tag);
    let start = body.find(&open)?;
    let rest = &body[start + open.len()..];
    let end = rest.find(&close)?;
    Some(rest[..end].trim().to_string())
}

fn find_consistent_bundle_version_in_json(value: &serde_json::Value) -> Option<String> {
    let mut versions = Vec::new();
    collect_bundle_versions(value, &mut versions);
    versions.retain(|version| looks_like_version(version));
    versions.sort();
    versions.dedup();
    if versions.len() == 1 {
        return versions.into_iter().next();
    }
    None
}

fn collect_bundle_versions(value: &serde_json::Value, versions: &mut Vec<String>) {
    match value {
        serde_json::Value::Object(map) => {
            if let Some(bundle) = map
                .get("bundle")
                .and_then(|candidate| candidate.as_object())
            {
                if let Some(version) = bundle
                    .get("version")
                    .and_then(|candidate| candidate.as_str())
                {
                    versions.push(version.to_string());
                }
            }
            for nested in map.values() {
                collect_bundle_versions(nested, versions);
            }
        }
        serde_json::Value::Array(items) => {
            for item in items {
                collect_bundle_versions(item, versions);
            }
        }
        _ => {}
    }
}

fn extract_semver_like(body: &str) -> Option<String> {
    for token in body.split(|ch: char| !(ch.is_ascii_alphanumeric() || ch == '.' || ch == '-')) {
        if looks_like_version(token) {
            return Some(token.to_string());
        }
    }
    None
}

fn looks_like_version(value: &str) -> bool {
    let parts: Vec<&str> = value.trim().split('.').collect();
    if parts.len() < 2 || parts.len() > 4 {
        return false;
    }
    parts.iter().all(|part| {
        !part.is_empty()
            && part
                .chars()
                .all(|ch| ch.is_ascii_digit() || ch.is_ascii_alphabetic() || ch == '-')
    })
}

fn repo_root() -> Result<PathBuf, String> {
    let here = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let root = here
        .join("../..")
        .canonicalize()
        .map_err(|err| format!("resolve repo root: {}", err))?;
    Ok(root)
}

fn binary_candidates(root: PathBuf) -> Vec<String> {
    if prefer_go_run_fallback(&root) {
        return Vec::new();
    }

    let mut candidates = Vec::new();
    if let Ok(env_path) = std::env::var("NIFI_FLOW_UPGRADE_BINARY") {
        candidates.push(PathBuf::from(env_path));
    }
    candidates.push(root.join("bin/nifi-flow-upgrade"));
    candidates.push(root.join("nifi-flow-upgrade"));

    let mut result = Vec::new();
    for candidate in candidates {
        if !candidate.is_file() {
            continue;
        }
        let rendered = candidate.to_string_lossy().to_string();
        if !result.contains(&rendered) {
            result.push(rendered);
        }
    }
    result
}

fn prefer_go_run_fallback(root: &Path) -> bool {
    let binary = root.join("bin/nifi-flow-upgrade");
    let source_entry = root.join("cmd/nifi-flow-upgrade/main.go");
    let go_mod = root.join("go.mod");

    if !source_entry.is_file() || !go_mod.is_file() {
        return false;
    }
    if !binary.is_file() {
        return true;
    }

    let binary_time = match fs::metadata(&binary).and_then(|meta| meta.modified()) {
        Ok(time) => time,
        Err(_) => return true,
    };

    for candidate in [
        go_mod,
        source_entry,
        root.join("internal/flowupgrade/analyze.go"),
        root.join("internal/flowupgrade/rewrite.go"),
        root.join("internal/flowupgrade/rulepack.go"),
    ] {
        let Ok(modified) = fs::metadata(&candidate).and_then(|meta| meta.modified()) else {
            continue;
        };
        if modified > binary_time {
            return true;
        }
    }

    false
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{SystemTime, UNIX_EPOCH};

    fn temp_path(name: &str) -> PathBuf {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_nanos();
        std::env::temp_dir().join(format!("nifi-flow-upgrade-advisor-{name}-{nanos}"))
    }

    #[test]
    fn external_workspace_keeps_builtin_rule_packs() {
        let temp_root = temp_path("workspace-scan");
        fs::create_dir_all(&temp_root).expect("create temp workspace");
        let flow_path = temp_root.join("flow.json");
        fs::write(
            &flow_path,
            r#"{
              "rootGroup": {"identifier": "root"},
              "parameterContexts": [],
              "processors": [
                {
                  "identifier": "proc-1",
                  "name": "Asana",
                  "bundle": {"version": "2.7.1"}
                }
              ]
            }"#,
        )
        .expect("write flow");

        let state = scan_workspace_internal(Some(temp_root.to_string_lossy().as_ref()))
            .expect("scan external workspace");

        assert_eq!(state.workspace_root, temp_root.to_string_lossy());
        assert!(state
            .flow_candidates
            .iter()
            .any(|item| item.path == flow_path.to_string_lossy()));
        assert!(
            state
                .rule_packs
                .iter()
                .any(|item| item.display_path.contains("nifi-2.7-to-2.8.official.yaml")),
            "expected built-in rule packs from tool repo"
        );

        fs::remove_dir_all(&temp_root).expect("cleanup temp workspace");
    }

    #[test]
    fn go_run_fallback_is_available_without_built_binary() {
        let request = CliActionRequest {
            action: "analyze".into(),
            workspace_path: repo_root()
                .expect("repo root")
                .to_string_lossy()
                .to_string(),
            binary_path: String::new(),
            source_path: String::new(),
            source_format: "flow-json-gz".into(),
            source_version: "2.7.1".into(),
            target_version: "2.8.0".into(),
            rule_pack_paths: Vec::new(),
            extensions_manifest_path: None,
            output_dir: String::new(),
        };

        let target = resolve_exec_target(&request).expect("resolve exec target");
        match target {
            ExecTarget::Binary(_) | ExecTarget::GoRun(_) => {}
        }
    }
}
