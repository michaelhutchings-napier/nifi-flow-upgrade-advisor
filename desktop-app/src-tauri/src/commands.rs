use serde::{Deserialize, Serialize};
use serde_json::json;
use std::collections::{BTreeMap, BTreeSet};
use std::ffi::OsStr;
use std::fs;
use std::io::Read;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::Instant;

const MAX_PREVIEW_FINDINGS_PER_CLASS: usize = 25;
const MAX_PREVIEW_OPERATIONS: usize = 50;
const MAX_INLINE_VIEW_BYTES: u64 = 512 * 1024;
const MAX_SCAN_DEPTH: usize = 5;
const MAX_FILE_BYTES_FOR_CONTENT_SNIFF: u64 = 256 * 1024;
const MAX_COMPRESSED_BYTES_FOR_VERSION_DETECTION: u64 = 2 * 1024 * 1024;
const MAX_ENTRIES_PER_DIRECTORY: usize = 2000;

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
    detected_version_confidence: Option<String>,
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
    rewritten_artifact_path: Option<String>,
}

#[derive(Debug, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ReportIndexPreview {
    migration_report: Option<serde_json::Value>,
    rewrite_report: Option<serde_json::Value>,
    validation_report: Option<serde_json::Value>,
    run_report: Option<serde_json::Value>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ReportGroupPreview {
    label: String,
    md_path: Option<String>,
    json_path: Option<String>,
    md_size_bytes: Option<u64>,
    json_size_bytes: Option<u64>,
    md_inline_safe: bool,
    json_inline_safe: bool,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ReportBundlePreview {
    primary_report: Option<serde_json::Value>,
    report_index: ReportIndexPreview,
    groups: Vec<ReportGroupPreview>,
    default_view_path: Option<String>,
    inline_view_limit_bytes: u64,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FlowUsageSummary {
    supported: bool,
    source_path: String,
    source_format: String,
    message: Option<String>,
    controller_services: Vec<ControllerServiceUsage>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ControllerServiceUsage {
    id: String,
    name: String,
    component_type: String,
    component_scope: String,
    path: String,
    active_reference_count: usize,
    distinct_referrer_count: usize,
    referenced_by: Vec<FlowUsageReference>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FlowUsageReference {
    component_id: String,
    component_name: String,
    component_type: String,
    component_scope: String,
    component_path: String,
    property_name: String,
}

enum ExecTarget {
    Binary(String),
    GoRun(PathBuf),
}

#[derive(Debug, Clone)]
struct VersionDetection {
    version: String,
    confidence: &'static str,
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
pub fn load_report_bundle(report_paths: Vec<String>) -> Result<ReportBundlePreview, String> {
    let groups = build_report_groups(&report_paths);
    let mut previews_by_path = BTreeMap::new();
    let mut report_index = ReportIndexPreview::default();

    for path in report_paths.iter().filter(|path| path.ends_with(".json")) {
        let preview = load_report_preview(path)?;
        match preview
            .get("kind")
            .and_then(|value| value.as_str())
            .unwrap_or_default()
        {
            "MigrationReport" if report_index.migration_report.is_none() => {
                report_index.migration_report = Some(preview.clone());
            }
            "RewriteReport" if report_index.rewrite_report.is_none() => {
                report_index.rewrite_report = Some(preview.clone());
            }
            "ValidationReport" if report_index.validation_report.is_none() => {
                report_index.validation_report = Some(preview.clone());
            }
            "RunReport" if report_index.run_report.is_none() => {
                report_index.run_report = Some(preview.clone());
            }
            _ => {}
        }
        previews_by_path.insert(path.clone(), preview);
    }

    let preferred_json = preferred_json_report_path(&report_paths);
    let primary_report = preferred_json
        .as_ref()
        .and_then(|path| previews_by_path.get(path).cloned());

    Ok(ReportBundlePreview {
        primary_report,
        report_index,
        groups,
        default_view_path: None,
        inline_view_limit_bytes: MAX_INLINE_VIEW_BYTES,
    })
}

#[tauri::command]
pub fn inspect_flow_usage(path: String, source_format: String) -> Result<FlowUsageSummary, String> {
    let rendered_path = path.trim().to_string();
    if rendered_path.is_empty() {
        return Ok(FlowUsageSummary {
            supported: false,
            source_path: String::new(),
            source_format,
            message: Some("No source flow path was available for usage insights.".into()),
            controller_services: Vec::new(),
        });
    }

    let source_path = PathBuf::from(&rendered_path);
    if !source_path.exists() {
        return Ok(FlowUsageSummary {
            supported: false,
            source_path: rendered_path,
            source_format,
            message: Some(
                "The analyzed source flow is no longer available on disk for usage insights."
                    .into(),
            ),
            controller_services: Vec::new(),
        });
    }

    summarize_flow_usage(&source_path, &source_format)
}

#[tauri::command]
pub fn open_path(path: String, create_dir_if_missing: bool) -> Result<(), String> {
    let path = PathBuf::from(path.trim());
    if create_dir_if_missing && !path.exists() {
        fs::create_dir_all(&path).map_err(|err| format!("create {}: {}", path.display(), err))?;
    }
    if !path.exists() {
        return Err(format!("path does not exist: {}", path.display()));
    }

    let mut command = open_command_for(&path)?;
    command
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn()
        .map_err(|err| format!("open {}: {}", path.display(), err))?;
    Ok(())
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
            let input = preferred_validate_input(&output_dir, &prepared);
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
        rewritten_artifact_path: detect_rewritten_artifact_path(
            validated.action.as_str(),
            &output_dir,
            prepared.cli_format.as_str(),
        ),
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

fn preferred_validate_input(output_dir: &Path, prepared: &PreparedSource) -> PathBuf {
    let rewritten = match prepared.cli_format.as_str() {
        "git-registry-dir" => output_dir.join("rewritten-flow"),
        "flow-json-gz" => output_dir.join("rewritten-flow.json.gz"),
        _ => output_dir.join("rewritten-flow.json"),
    };

    if rewritten.exists() {
        rewritten
    } else {
        PathBuf::from(&prepared.path)
    }
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

fn preferred_json_report_path(paths: &[String]) -> Option<String> {
    for suffix in [
        "run-report.json",
        "validation-report.json",
        "rewrite-report.json",
        "migration-report.json",
    ] {
        if let Some(path) = paths.iter().find(|path| path.ends_with(suffix)) {
            return Some(path.clone());
        }
    }
    paths.iter().find(|path| path.ends_with(".json")).cloned()
}

fn build_report_groups(paths: &[String]) -> Vec<ReportGroupPreview> {
    let mut groups = BTreeMap::<String, ReportGroupPreview>::new();

    for path in paths {
        let label = report_group_name(path);
        let entry = groups
            .entry(label.clone())
            .or_insert_with(|| ReportGroupPreview {
                label,
                md_path: None,
                json_path: None,
                md_size_bytes: None,
                json_size_bytes: None,
                md_inline_safe: false,
                json_inline_safe: false,
            });

        let size_bytes = fs::metadata(path).map(|meta| meta.len()).ok();
        let inline_safe = size_bytes
            .map(|size| size <= MAX_INLINE_VIEW_BYTES)
            .unwrap_or(false);
        if path.ends_with(".md") {
            entry.md_path = Some(path.clone());
            entry.md_size_bytes = size_bytes;
            entry.md_inline_safe = inline_safe;
        } else if path.ends_with(".json") {
            entry.json_path = Some(path.clone());
            entry.json_size_bytes = size_bytes;
            entry.json_inline_safe = inline_safe;
        }
    }

    ["Analyze", "Rewrite", "Validate", "Run"]
        .iter()
        .filter_map(|label| groups.remove(*label))
        .collect()
}

fn report_group_name(path: &str) -> String {
    let name = Path::new(path)
        .file_name()
        .and_then(|value| value.to_str())
        .unwrap_or_default()
        .trim_end_matches(".json")
        .trim_end_matches(".md")
        .to_string();

    match name.as_str() {
        "migration-report" => "Analyze".into(),
        "rewrite-report" => "Rewrite".into(),
        "validation-report" => "Validate".into(),
        "run-report" => "Run".into(),
        _ => name,
    }
}

fn load_report_preview(path: &str) -> Result<serde_json::Value, String> {
    let body = fs::read_to_string(path).map_err(|err| format!("read {}: {}", path, err))?;
    let mut value: serde_json::Value =
        serde_json::from_str(&body).map_err(|err| format!("parse {}: {}", path, err))?;
    let inline_safe = fs::metadata(path)
        .map(|meta| meta.len() <= MAX_INLINE_VIEW_BYTES)
        .unwrap_or(false);
    let kind = value
        .get("kind")
        .and_then(|candidate| candidate.as_str())
        .unwrap_or_default()
        .to_string();

    match kind.as_str() {
        "MigrationReport" | "ValidationReport" => {
            let preview = if inline_safe {
                full_findings_preview(&value)
            } else {
                limit_findings_preview(&mut value)
            };
            value["preview"] = preview;
        }
        "RewriteReport" => {
            let preview = if inline_safe {
                full_operations_preview(&value)
            } else {
                limit_operations_preview(&mut value)
            };
            value["preview"] = preview;
        }
        "RunReport" => {
            value["preview"] = json!({
                "truncated": false,
                "totalItems": value.get("steps").and_then(|candidate| candidate.as_array()).map(|items| items.len()).unwrap_or(0),
                "shownItems": value.get("steps").and_then(|candidate| candidate.as_array()).map(|items| items.len()).unwrap_or(0),
                "inlineViewLimitBytes": MAX_INLINE_VIEW_BYTES
            });
        }
        _ => {}
    }

    Ok(value)
}

fn full_findings_preview(value: &serde_json::Value) -> serde_json::Value {
    let findings = value
        .get("findings")
        .and_then(|candidate| candidate.as_array())
        .cloned()
        .unwrap_or_default();

    let mut shown_by_class = BTreeMap::<String, usize>::new();
    for finding in &findings {
        let class = finding
            .get("class")
            .and_then(|candidate| candidate.as_str())
            .unwrap_or("unknown")
            .to_string();
        *shown_by_class.entry(class).or_insert(0) += 1;
    }

    json!({
        "truncated": false,
        "totalItems": findings.len(),
        "shownItems": findings.len(),
        "shownByClass": shown_by_class,
        "inlineViewLimitBytes": MAX_INLINE_VIEW_BYTES
    })
}

fn limit_findings_preview(value: &mut serde_json::Value) -> serde_json::Value {
    let findings = value
        .get("findings")
        .and_then(|candidate| candidate.as_array())
        .cloned()
        .unwrap_or_default();

    let mut shown_by_class = BTreeMap::<String, usize>::new();
    let mut limited = Vec::new();
    for finding in findings.iter() {
        let class = finding
            .get("class")
            .and_then(|candidate| candidate.as_str())
            .unwrap_or("unknown")
            .to_string();
        let count = shown_by_class.entry(class).or_insert(0);
        if *count >= MAX_PREVIEW_FINDINGS_PER_CLASS {
            continue;
        }
        *count += 1;
        limited.push(finding.clone());
    }

    let truncated = limited.len() < findings.len();
    value["findings"] = serde_json::Value::Array(limited);
    json!({
        "truncated": truncated,
        "totalItems": findings.len(),
        "shownItems": value.get("findings").and_then(|candidate| candidate.as_array()).map(|items| items.len()).unwrap_or(0),
        "shownByClass": shown_by_class,
        "inlineViewLimitBytes": MAX_INLINE_VIEW_BYTES
    })
}

fn limit_operations_preview(value: &mut serde_json::Value) -> serde_json::Value {
    let operations = value
        .get("operations")
        .and_then(|candidate| candidate.as_array())
        .cloned()
        .unwrap_or_default();

    let limited: Vec<serde_json::Value> = operations
        .iter()
        .take(MAX_PREVIEW_OPERATIONS)
        .cloned()
        .collect();
    let truncated = limited.len() < operations.len();
    value["operations"] = serde_json::Value::Array(limited);
    json!({
        "truncated": truncated,
        "totalItems": operations.len(),
        "shownItems": value.get("operations").and_then(|candidate| candidate.as_array()).map(|items| items.len()).unwrap_or(0),
        "inlineViewLimitBytes": MAX_INLINE_VIEW_BYTES
    })
}

fn full_operations_preview(value: &serde_json::Value) -> serde_json::Value {
    let operations = value
        .get("operations")
        .and_then(|candidate| candidate.as_array())
        .cloned()
        .unwrap_or_default();

    json!({
        "truncated": false,
        "totalItems": operations.len(),
        "shownItems": operations.len(),
        "inlineViewLimitBytes": MAX_INLINE_VIEW_BYTES
    })
}

fn detect_rewritten_artifact_path(
    action: &str,
    output_dir: &Path,
    source_format: &str,
) -> Option<String> {
    match action {
        "rewrite" => preferred_rewritten_artifact_path(output_dir, source_format),
        "run" => rewritten_artifact_path_from_run_report(output_dir)
            .or_else(|| preferred_rewritten_artifact_path(output_dir, source_format)),
        _ => None,
    }
}

#[derive(Debug, Clone)]
struct UsageComponentRecord {
    id: String,
    name: String,
    component_type: String,
    component_scope: String,
    path: String,
    properties: BTreeMap<String, serde_json::Value>,
    property_descriptors: BTreeMap<String, serde_json::Value>,
    controller_service_refs: Vec<(String, String)>,
}

fn summarize_flow_usage(path: &Path, source_format: &str) -> Result<FlowUsageSummary, String> {
    let source_path = path.to_string_lossy().to_string();
    let body = match read_flow_usage_source(path, source_format) {
        Ok(body) => body,
        Err(message) => {
            return Ok(FlowUsageSummary {
                supported: false,
                source_path,
                source_format: source_format.to_string(),
                message: Some(message),
                controller_services: Vec::new(),
            });
        }
    };

    let document: serde_json::Value = match serde_json::from_str(&body) {
        Ok(value) => value,
        Err(err) => {
            return Ok(FlowUsageSummary {
                supported: false,
                source_path,
                source_format: source_format.to_string(),
                message: Some(format!(
                    "Could not parse the source flow for usage insights: {}",
                    err
                )),
                controller_services: Vec::new(),
            });
        }
    };

    let mut components = Vec::new();
    collect_usage_components(&document, &[], &mut components);

    let controller_service_ids = components
        .iter()
        .filter(|component| component.component_scope == "controller-service")
        .map(|component| component.id.clone())
        .collect::<BTreeSet<_>>();

    for component in &mut components {
        component.controller_service_refs = collect_controller_service_refs(
            &component.properties,
            &component.property_descriptors,
            &controller_service_ids,
        );
    }

    let mut services = BTreeMap::<String, ControllerServiceUsage>::new();
    for component in &components {
        if component.component_scope != "controller-service" {
            continue;
        }
        services.insert(
            component.id.clone(),
            ControllerServiceUsage {
                id: component.id.clone(),
                name: component.name.clone(),
                component_type: component.component_type.clone(),
                component_scope: component.component_scope.clone(),
                path: component.path.clone(),
                active_reference_count: 0,
                distinct_referrer_count: 0,
                referenced_by: Vec::new(),
            },
        );
    }

    for component in &components {
        for (property_name, referenced_service_id) in &component.controller_service_refs {
            let Some(service) = services.get_mut(referenced_service_id) else {
                continue;
            };
            service.active_reference_count += 1;
            service.referenced_by.push(FlowUsageReference {
                component_id: component.id.clone(),
                component_name: component.name.clone(),
                component_type: component.component_type.clone(),
                component_scope: component.component_scope.clone(),
                component_path: component.path.clone(),
                property_name: property_name.clone(),
            });
        }
    }

    let mut controller_services: Vec<ControllerServiceUsage> = services.into_values().collect();
    for service in &mut controller_services {
        service.distinct_referrer_count = service
            .referenced_by
            .iter()
            .map(|reference| reference.component_id.clone())
            .collect::<BTreeSet<_>>()
            .len();
    }
    controller_services.sort_by(|left, right| {
        right
            .active_reference_count
            .cmp(&left.active_reference_count)
            .then_with(|| {
                right
                    .distinct_referrer_count
                    .cmp(&left.distinct_referrer_count)
            })
            .then_with(|| left.name.cmp(&right.name))
            .then_with(|| left.id.cmp(&right.id))
    });

    for service in &mut controller_services {
        service.referenced_by.sort_by(|left, right| {
            left.component_name
                .cmp(&right.component_name)
                .then_with(|| left.property_name.cmp(&right.property_name))
                .then_with(|| left.component_id.cmp(&right.component_id))
        });
    }

    Ok(FlowUsageSummary {
        supported: true,
        source_path,
        source_format: source_format.to_string(),
        message: None,
        controller_services,
    })
}

fn read_flow_usage_source(path: &Path, source_format: &str) -> Result<String, String> {
    match source_format {
        "flow-json-fixture" | "versioned-flow-snapshot" | "" => {
            fs::read_to_string(path).map_err(|err| format!("read {}: {}", path.display(), err))
        }
        "flow-json-gz" => {
            let file = fs::File::open(path).map_err(|err| format!("open {}: {}", path.display(), err))?;
            let mut decoder = flate2::read::GzDecoder::new(file);
            let mut body = String::new();
            decoder
                .read_to_string(&mut body)
                .map_err(|err| format!("decompress {}: {}", path.display(), err))?;
            Ok(body)
        }
        other => Err(format!(
            "Usage insights are available for JSON-based flow exports right now. This source uses {}.",
            other
        )),
    }
    .or_else(|err| {
        let rendered = path.to_string_lossy().to_ascii_lowercase();
        if rendered.ends_with(".json") {
            fs::read_to_string(path).map_err(|read_err| format!("read {}: {}", path.display(), read_err))
        } else if rendered.ends_with(".json.gz") {
            let file = fs::File::open(path).map_err(|open_err| format!("open {}: {}", path.display(), open_err))?;
            let mut decoder = flate2::read::GzDecoder::new(file);
            let mut body = String::new();
            decoder
                .read_to_string(&mut body)
                .map_err(|decompress_err| format!("decompress {}: {}", path.display(), decompress_err))?;
            Ok(body)
        } else {
            Err(err)
        }
    })
}

fn collect_usage_components(
    value: &serde_json::Value,
    path: &[String],
    components: &mut Vec<UsageComponentRecord>,
) {
    match value {
        serde_json::Value::Object(map) => {
            let component = extract_usage_component(map, path);
            let mut child_context = path.to_vec();
            if let Some(component) = component {
                child_context.push(component.name.clone());
                components.push(component);
            }

            for (key, nested) in map {
                let mut next_path = child_context.clone();
                if usage_path_key(key) {
                    next_path.push(key.clone());
                }
                collect_usage_components(nested, &next_path, components);
            }
        }
        serde_json::Value::Array(items) => {
            for item in items {
                collect_usage_components(item, path, components);
            }
        }
        _ => {}
    }
}

fn extract_usage_component(
    map: &serde_json::Map<String, serde_json::Value>,
    path: &[String],
) -> Option<UsageComponentRecord> {
    let component_kind = infer_usage_component_kind(map, path)?;
    let identifier = map
        .get("identifier")
        .and_then(|value| value.as_str())
        .or_else(|| map.get("id").and_then(|value| value.as_str()))?
        .trim();
    if identifier.is_empty() {
        return None;
    }

    let name = map
        .get("name")
        .and_then(|value| value.as_str())
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .unwrap_or(identifier)
        .to_string();

    let component_type = map
        .get("type")
        .and_then(|value| value.as_str())
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .unwrap_or(component_kind.as_str())
        .to_string();

    Some(UsageComponentRecord {
        id: identifier.to_string(),
        name: name.clone(),
        component_type,
        component_scope: normalize_component_scope(&component_kind),
        path: usage_component_path(path, &name),
        properties: json_object_field(map, "properties"),
        property_descriptors: json_object_field(map, "propertyDescriptors"),
        controller_service_refs: Vec::new(),
    })
}

fn usage_path_key(key: &str) -> bool {
    matches!(
        key,
        "rootGroup"
            | "flowContents"
            | "externalControllerServices"
            | "processGroups"
            | "controllerServices"
            | "processors"
            | "reportingTasks"
            | "funnels"
            | "labels"
            | "inputPorts"
            | "outputPorts"
            | "remoteProcessGroups"
    )
}

fn infer_usage_component_kind(
    map: &serde_json::Map<String, serde_json::Value>,
    path: &[String],
) -> Option<String> {
    if let Some(kind) = map
        .get("componentType")
        .and_then(|value| value.as_str())
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return Some(kind.to_string());
    }

    let path_hint = path
        .iter()
        .rev()
        .find(|segment| !segment.trim().is_empty())
        .map(|segment| segment.as_str());

    match path_hint {
        Some("rootGroup") | Some("flowContents") | Some("processGroups") => {
            Some("PROCESS_GROUP".into())
        }
        Some("externalControllerServices") | Some("controllerServices") => {
            Some("CONTROLLER_SERVICE".into())
        }
        Some("processors") => Some("PROCESSOR".into()),
        Some("reportingTasks") => Some("REPORTING_TASK".into()),
        Some("funnels") => Some("FUNNEL".into()),
        Some("labels") => Some("LABEL".into()),
        Some("inputPorts") => Some("INPUT_PORT".into()),
        Some("outputPorts") => Some("OUTPUT_PORT".into()),
        Some("remoteProcessGroups") => Some("REMOTE_PROCESS_GROUP".into()),
        _ if map.contains_key("processors")
            || map.contains_key("controllerServices")
            || map.contains_key("processGroups")
            || map.contains_key("reportingTasks") =>
        {
            Some("PROCESS_GROUP".into())
        }
        _ => None,
    }
}

fn usage_component_path(path: &[String], name: &str) -> String {
    path.iter()
        .cloned()
        .chain([name.to_string()])
        .filter(|segment| !segment.trim().is_empty())
        .collect::<Vec<_>>()
        .join("/")
}

fn normalize_component_scope(component_kind: &str) -> String {
    component_kind.trim().to_ascii_lowercase().replace('_', "-")
}

fn json_object_field(
    map: &serde_json::Map<String, serde_json::Value>,
    field: &str,
) -> BTreeMap<String, serde_json::Value> {
    map.get(field)
        .and_then(|value| value.as_object())
        .map(|entries| {
            entries
                .iter()
                .map(|(key, value)| (key.clone(), value.clone()))
                .collect()
        })
        .unwrap_or_default()
}

fn collect_controller_service_refs(
    properties: &BTreeMap<String, serde_json::Value>,
    descriptors: &BTreeMap<String, serde_json::Value>,
    controller_service_ids: &BTreeSet<String>,
) -> Vec<(String, String)> {
    let mut refs = BTreeSet::<(String, String)>::new();
    let mut saw_controller_service_descriptor = false;

    for (property_name, descriptor) in descriptors {
        let Some(descriptor_object) = descriptor.as_object() else {
            continue;
        };
        if descriptor_object
            .get("identifiesControllerService")
            .and_then(|value| value.as_bool())
            != Some(true)
        {
            continue;
        }
        saw_controller_service_descriptor = true;
        let Some(value) = properties.get(property_name) else {
            continue;
        };
        for reference in extract_controller_service_ref_values(value, controller_service_ids) {
            refs.insert((property_name.clone(), reference));
        }
    }

    if saw_controller_service_descriptor || controller_service_ids.is_empty() {
        return refs.into_iter().collect();
    }

    for (property_name, value) in properties {
        for reference in extract_controller_service_ref_values(value, controller_service_ids) {
            refs.insert((property_name.clone(), reference));
        }
    }

    refs.into_iter().collect()
}

fn extract_controller_service_ref_values(
    value: &serde_json::Value,
    controller_service_ids: &BTreeSet<String>,
) -> Vec<String> {
    match value {
        serde_json::Value::String(raw) => {
            let candidate = raw.trim();
            if controller_service_ids.contains(candidate) {
                vec![candidate.to_string()]
            } else {
                Vec::new()
            }
        }
        serde_json::Value::Array(items) => items
            .iter()
            .flat_map(|item| extract_controller_service_ref_values(item, controller_service_ids))
            .collect(),
        _ => Vec::new(),
    }
}

fn preferred_rewritten_artifact_path(output_dir: &Path, source_format: &str) -> Option<String> {
    let candidate = match source_format {
        "git-registry-dir" => output_dir.join("rewritten-flow"),
        "flow-json-gz" => output_dir.join("rewritten-flow.json.gz"),
        _ => output_dir.join("rewritten-flow.json"),
    };
    candidate
        .exists()
        .then(|| candidate.to_string_lossy().to_string())
}

fn rewritten_artifact_path_from_run_report(output_dir: &Path) -> Option<String> {
    let path = output_dir.join("run-report.json");
    let body = fs::read_to_string(path).ok()?;
    let report: DesktopRunReport = serde_json::from_str(&body).ok()?;
    report
        .steps
        .into_iter()
        .find(|step| {
            step.name == "rewrite" && step.status == "completed" && !step.output_path.is_empty()
        })
        .map(|step| step.output_path)
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct DesktopRunReport {
    steps: Vec<DesktopRunStep>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct DesktopRunStep {
    name: String,
    status: String,
    output_path: String,
}

fn open_command_for(path: &Path) -> Result<Command, String> {
    #[cfg(target_os = "macos")]
    {
        let mut command = Command::new("open");
        command.arg(path);
        return Ok(command);
    }

    #[cfg(target_os = "windows")]
    {
        let mut command = Command::new("explorer");
        command.arg(path);
        return Ok(command);
    }

    #[cfg(all(unix, not(target_os = "macos")))]
    {
        if is_wsl() {
            if let Some(windows_path) = wsl_windows_path(path) {
                let mut command = Command::new("powershell.exe");
                command.args([
                    "-NoProfile",
                    "-Command",
                    "Start-Process",
                    "-FilePath",
                    windows_path.as_str(),
                ]);
                return Ok(command);
            }
        }

        let mut command = Command::new("xdg-open");
        command.arg(path);
        return Ok(command);
    }

    #[allow(unreachable_code)]
    Err("opening paths is not supported on this platform".into())
}

fn is_wsl() -> bool {
    std::env::var_os("WSL_DISTRO_NAME").is_some()
        || fs::read_to_string("/proc/version")
            .map(|body| body.to_ascii_lowercase().contains("microsoft"))
            .unwrap_or(false)
}

fn wsl_windows_path(path: &Path) -> Option<String> {
    let output = Command::new("wslpath").arg("-w").arg(path).output().ok()?;
    if !output.status.success() {
        return None;
    }
    let rendered = String::from_utf8_lossy(&output.stdout).trim().to_string();
    (!rendered.is_empty()).then_some(rendered)
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
    flow_candidates.sort_by(|a, b| a.display_path.cmp(&b.display_path));
    rule_packs.sort_by(|a, b| a.display_path.cmp(&b.display_path));
    manifests.sort_by(|a, b| a.display_path.cmp(&b.display_path));

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
    if depth > MAX_SCAN_DEPTH {
        return Ok(());
    }

    let entries =
        fs::read_dir(current).map_err(|err| format!("scan {}: {}", current.display(), err))?;
    for (index, entry) in entries.enumerate() {
        if index >= MAX_ENTRIES_PER_DIRECTORY {
            break;
        }
        let entry = entry.map_err(|err| format!("scan entry: {}", err))?;
        let path = entry.path();
        let name = entry.file_name();
        let name = name.to_string_lossy();

        if path.is_dir() {
            if should_skip_dir(&name, current) {
                continue;
            }
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

    Ok(())
}

fn detect_flow_format(path: &Path) -> Option<(&'static str, &'static str)> {
    let name = path.file_name()?.to_string_lossy();
    let lowered = name.to_ascii_lowercase();
    if name.ends_with(".json.gz") {
        return Some(("Flow artifact", "flow-json-gz"));
    }
    if name.ends_with(".xml.gz") {
        return Some(("Legacy flow.xml.gz", "flow-xml-gz"));
    }
    if name.ends_with(".json") {
        if !looks_like_flow_json_name(&lowered)
            && file_size(path).is_some_and(|size| size > MAX_FILE_BYTES_FOR_CONTENT_SNIFF)
        {
            return None;
        }
        let body = read_text_prefix(path, MAX_FILE_BYTES_FOR_CONTENT_SNIFF).ok()?;
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

    for (index, entry) in entries.flatten().enumerate() {
        if index >= MAX_ENTRIES_PER_DIRECTORY {
            break;
        }
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

        if file_size(&candidate).is_some_and(|size| size > MAX_FILE_BYTES_FOR_CONTENT_SNIFF) {
            continue;
        }
        let Ok(body) = read_text_prefix(&candidate, MAX_FILE_BYTES_FOR_CONTENT_SNIFF) else {
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
    detected_version: Option<VersionDetection>,
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
        detected_version: detected_version.as_ref().map(|value| value.version.clone()),
        detected_version_confidence: detected_version
            .as_ref()
            .map(|value| value.confidence.to_string()),
    }
}

fn detect_source_version(path: &Path) -> Option<VersionDetection> {
    let name = path.file_name()?.to_string_lossy().to_lowercase();
    if name.ends_with(".json.gz") {
        if file_size(path).is_some_and(|size| size > MAX_COMPRESSED_BYTES_FOR_VERSION_DETECTION) {
            return None;
        }
        let content = fs::read(path).ok()?;
        let mut reader = flate2::read::GzDecoder::new(&content[..]);
        let mut decoded = String::new();
        std::io::Read::read_to_string(&mut reader, &mut decoded).ok()?;
        return detect_version_from_text(&decoded);
    }
    if name.ends_with(".xml.gz") {
        if file_size(path).is_some_and(|size| size > MAX_COMPRESSED_BYTES_FOR_VERSION_DETECTION) {
            return None;
        }
        let content = fs::read(path).ok()?;
        let mut reader = flate2::read::GzDecoder::new(&content[..]);
        let mut decoded = String::new();
        std::io::Read::read_to_string(&mut reader, &mut decoded).ok()?;
        return detect_version_from_text(&decoded);
    }
    if name.ends_with(".json") || name.ends_with(".yaml") || name.ends_with(".yml") {
        if file_size(path).is_some_and(|size| size > MAX_FILE_BYTES_FOR_CONTENT_SNIFF) {
            return None;
        }
        let body = read_text_prefix(path, MAX_FILE_BYTES_FOR_CONTENT_SNIFF).ok()?;
        return detect_version_from_text(&body);
    }
    None
}

fn should_skip_dir(name: &str, current: &Path) -> bool {
    name == ".git"
        || name == "target"
        || name == "node_modules"
        || name == ".nifi-flow-upgrade-desktop"
        || name == ".idea"
        || name == ".vscode"
        || name == ".gradle"
        || name == ".terraform"
        || name == ".venv"
        || name == "__pycache__"
        || name == "vendor"
        || name == "dist"
        || name == "build"
        || name == "coverage"
        || name == ".next"
        || name == ".yarn"
        || (name.starts_with('.') && name != ".github")
        || (name == "out" && current.ends_with("demo"))
}

fn file_size(path: &Path) -> Option<u64> {
    fs::metadata(path).ok().map(|meta| meta.len())
}

fn read_text_prefix(path: &Path, max_bytes: u64) -> Result<String, String> {
    let bytes = fs::read(path).map_err(|err| format!("read {}: {}", path.display(), err))?;
    let limit = usize::try_from(max_bytes).unwrap_or(usize::MAX);
    let prefix = &bytes[..bytes.len().min(limit)];
    Ok(String::from_utf8_lossy(prefix).to_string())
}

fn looks_like_flow_json_name(name: &str) -> bool {
    name.contains("flow") || name.contains("snapshot") || name.contains("registry")
}

fn detect_version_from_text(body: &str) -> Option<VersionDetection> {
    let patterns = [
        "\"nifiVersion\"",
        "\"niFiVersion\"",
        "nifiVersion:",
        "niFiVersion:",
        "<nifiVersion>",
        "<niFiVersion>",
    ];

    let trimmed = body.trim();
    if trimmed.starts_with('{') {
        if let Ok(payload) = serde_json::from_str::<serde_json::Value>(trimmed) {
            if let Some(version) = find_version_in_json(&payload) {
                return Some(VersionDetection {
                    version,
                    confidence: "detected",
                });
            }
            if let Some(version) = find_consistent_bundle_version_in_json(&payload) {
                return Some(VersionDetection {
                    version,
                    confidence: "inferred",
                });
            }
        }
    }

    if !patterns.iter().any(|pattern| body.contains(pattern)) {
        return None;
    }

    if body.contains('<') && body.contains('>') {
        for tag in ["nifiVersion", "niFiVersion"] {
            if let Some(version) = extract_xml_tag_value(body, tag) {
                if looks_like_version(&version) {
                    return Some(VersionDetection {
                        version,
                        confidence: "detected",
                    });
                }
            }
        }
    }

    for key in ["nifiVersion", "niFiVersion"] {
        if let Some(version) = extract_yaml_key_value(body, key) {
            if looks_like_version(&version) {
                return Some(VersionDetection {
                    version,
                    confidence: "detected",
                });
            }
        }
    }

    None
}

fn find_version_in_json(value: &serde_json::Value) -> Option<String> {
    match value {
        serde_json::Value::Object(map) => {
            for key in ["nifiVersion", "niFiVersion"] {
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

fn extract_yaml_key_value(body: &str, key: &str) -> Option<String> {
    for line in body.lines() {
        let trimmed = line.trim();
        let prefix = format!("{key}:");
        if !trimmed.starts_with(&prefix) {
            continue;
        }
        let value = trimmed[prefix.len()..]
            .trim()
            .trim_matches('"')
            .trim_matches('\'');
        if !value.is_empty() {
            return Some(value.to_string());
        }
    }
    None
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

    #[test]
    fn generic_schema_version_is_not_treated_as_nifi_version() {
        let detected = detect_version_from_text(
            r#"{
              "version": "1.0",
              "flowEncodingVersion": "1.0",
              "registryVersion": "1.0",
              "rootGroup": {"identifier": "root"}
            }"#,
        );

        assert!(
            detected.is_none(),
            "schema version fields should not be treated as NiFi runtime versions"
        );
    }

    #[test]
    fn explicit_nifi_version_is_detected() {
        let detected = detect_version_from_text(
            r#"{
              "nifiVersion": "1.27.0",
              "rootGroup": {"identifier": "root"}
            }"#,
        )
        .expect("detect explicit nifi version");

        assert_eq!(detected.version, "1.27.0");
        assert_eq!(detected.confidence, "detected");
    }

    #[test]
    fn consistent_bundle_version_is_inferred() {
        let detected = detect_version_from_text(
            r#"{
              "rootGroup": {"identifier": "root"},
              "processors": [
                {"bundle": {"version": "2.7.1"}},
                {"bundle": {"version": "2.7.1"}}
              ]
            }"#,
        )
        .expect("infer bundle version");

        assert_eq!(detected.version, "2.7.1");
        assert_eq!(detected.confidence, "inferred");
    }

    #[test]
    fn flow_usage_summary_reports_active_and_unreferenced_controller_services() {
        let temp_root = temp_path("flow-usage");
        fs::create_dir_all(&temp_root).expect("create temp root");
        let flow_path = temp_root.join("flow.json");
        fs::write(
            &flow_path,
            r#"{
              "flowContents": {
                "componentType": "PROCESS_GROUP",
                "identifier": "00000000-0000-0000-0000-000000000000",
                "name": "Root",
                "controllerServices": [
                  {
                    "identifier": "11111111-1111-1111-1111-111111111111",
                    "name": "Backend SSL",
                    "type": "org.apache.nifi.ssl.StandardRestrictedSSLContextService",
                    "componentType": "CONTROLLER_SERVICE",
                    "properties": {},
                    "propertyDescriptors": {}
                  },
                  {
                    "identifier": "22222222-2222-2222-2222-222222222222",
                    "name": "Unused SSL",
                    "type": "org.apache.nifi.ssl.StandardRestrictedSSLContextService",
                    "componentType": "CONTROLLER_SERVICE",
                    "properties": {},
                    "propertyDescriptors": {}
                  }
                ],
                "processors": [
                  {
                    "identifier": "33333333-3333-3333-3333-333333333333",
                    "name": "Invoke HTTPS",
                    "type": "org.apache.nifi.processors.standard.InvokeHTTP",
                    "componentType": "PROCESSOR",
                    "properties": {
                      "SSL Context Service": "11111111-1111-1111-1111-111111111111"
                    },
                    "propertyDescriptors": {
                      "SSL Context Service": {
                        "identifiesControllerService": true
                      }
                    }
                  }
                ]
              }
            }"#,
        )
        .expect("write flow");

        let summary = summarize_flow_usage(&flow_path, "versioned-flow-snapshot")
            .expect("summarize flow usage");

        assert!(summary.supported);
        assert_eq!(summary.controller_services.len(), 2);
        assert_eq!(summary.controller_services[0].name, "Backend SSL");
        assert_eq!(summary.controller_services[0].active_reference_count, 1);
        assert_eq!(summary.controller_services[0].distinct_referrer_count, 1);
        assert_eq!(
            summary.controller_services[0].referenced_by[0].component_name,
            "Invoke HTTPS"
        );
        assert_eq!(summary.controller_services[1].name, "Unused SSL");
        assert_eq!(summary.controller_services[1].active_reference_count, 0);
        assert_eq!(summary.controller_services[1].distinct_referrer_count, 0);

        fs::remove_dir_all(&temp_root).expect("cleanup temp root");
    }

    #[test]
    fn flow_json_fixture_usage_summary_handles_nested_groups_without_descriptors() {
        let temp_root = temp_path("flow-fixture-usage");
        fs::create_dir_all(&temp_root).expect("create temp root");
        let flow_path = temp_root.join("fixture-flow.json");
        fs::write(
            &flow_path,
            r#"{
              "rootGroup": {
                "id": "root-1",
                "name": "Root",
                "processGroups": [
                  {
                    "id": "pg-1",
                    "name": "API",
                    "controllerServices": [
                      {
                        "id": "ssl-active",
                        "name": "Backend SSL",
                        "type": "org.apache.nifi.ssl.StandardRestrictedSSLContextService",
                        "properties": {}
                      },
                      {
                        "id": "ssl-unused",
                        "name": "Unused SSL",
                        "type": "org.apache.nifi.ssl.StandardRestrictedSSLContextService",
                        "properties": {}
                      }
                    ],
                    "processors": [
                      {
                        "id": "invoke-1",
                        "name": "Call Backend",
                        "type": "org.apache.nifi.processors.standard.InvokeHTTP",
                        "properties": {
                          "SSL Context Service": "ssl-active",
                          "Remote URL": "https://example.test"
                        }
                      }
                    ]
                  }
                ]
              }
            }"#,
        )
        .expect("write flow");

        let summary =
            summarize_flow_usage(&flow_path, "flow-json-fixture").expect("summarize flow usage");

        assert!(summary.supported);
        assert_eq!(summary.controller_services.len(), 2);
        assert_eq!(summary.controller_services[0].name, "Backend SSL");
        assert_eq!(summary.controller_services[0].active_reference_count, 1);
        assert_eq!(summary.controller_services[0].distinct_referrer_count, 1);
        assert_eq!(
            summary.controller_services[0].referenced_by[0].component_name,
            "Call Backend"
        );
        assert_eq!(
            summary.controller_services[0].path,
            "rootGroup/Root/processGroups/API/controllerServices/Backend SSL"
        );
        assert_eq!(summary.controller_services[1].name, "Unused SSL");
        assert_eq!(summary.controller_services[1].active_reference_count, 0);
        assert_eq!(summary.controller_services[1].distinct_referrer_count, 0);

        fs::remove_dir_all(&temp_root).expect("cleanup temp root");
    }

    #[test]
    fn small_migration_reports_keep_full_findings_in_preview() {
        let temp_root = temp_path("report-preview");
        fs::create_dir_all(&temp_root).expect("create temp root");
        let report_path = temp_root.join("migration-report.json");

        let findings = (0..30)
            .map(|index| {
                serde_json::json!({
                    "ruleId": format!("rule-{index}"),
                    "class": "manual-change",
                    "severity": "warning",
                    "component": {
                        "id": format!("00000000-0000-0000-0000-{:012}", index),
                        "name": format!("Component {index}"),
                        "type": "org.apache.nifi.ssl.StandardRestrictedSSLContextService",
                        "scope": "controller-service",
                        "path": format!("flowContents/controllerServices/Component {index}")
                    },
                    "message": "StandardRestrictedSSLContextService is deprecated for removal."
                })
            })
            .collect::<Vec<_>>();

        fs::write(
            &report_path,
            serde_json::json!({
                "apiVersion": "nifi.flowupgrade/v1alpha1",
                "kind": "MigrationReport",
                "metadata": {"name": "test", "generatedAt": "2026-03-25T00:00:00Z"},
                "source": {"path": "/tmp/source.json", "format": "versioned-flow-snapshot", "nifiVersion": "2.7.0"},
                "target": {"nifiVersion": "2.8.0"},
                "rulePacks": [],
                "summary": {"totalFindings": 30, "byClass": {"manual-change": 30}},
                "findings": findings
            })
            .to_string(),
        )
        .expect("write report");

        let preview =
            load_report_preview(report_path.to_string_lossy().as_ref()).expect("load preview");
        let preview_findings = preview
            .get("findings")
            .and_then(|value| value.as_array())
            .expect("preview findings");
        assert_eq!(preview_findings.len(), 30);
        assert_eq!(
            preview
                .get("preview")
                .and_then(|value| value.get("truncated"))
                .and_then(|value| value.as_bool()),
            Some(false)
        );

        fs::remove_dir_all(&temp_root).expect("cleanup temp root");
    }
}
