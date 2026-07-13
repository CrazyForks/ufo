use anyhow::{Context, Result, anyhow};
use reqwest::header::{ACCEPT, AUTHORIZATION, HeaderMap, HeaderValue, USER_AGENT};
use reqwest::{Client, StatusCode};
use serde::Deserialize;
use serde_json::{Value, json};
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};
use tokio::time::sleep;

use crate::{git, git_env};

#[derive(Debug, Deserialize)]
#[allow(dead_code)]
pub struct AcceptedForgeAction {
    pub id: String,
    pub kind: String,
    pub provider: String,
    pub base_url: String,
    pub repo: String,
    pub head_branch: String,
    pub base_branch: String,
    #[serde(default)]
    pub commit_sha: String,
    #[serde(default)]
    pub title: String,
    #[serde(default)]
    pub body: String,
    #[serde(default)]
    pub credential_kind: String,
    #[serde(default)]
    pub credential_name: String,
    #[serde(default)]
    pub checks_commands: Vec<String>,
    #[serde(default)]
    pub checks_timeout_seconds: i64,
    #[serde(default)]
    pub ship_base_sync: String,
    pub lease_seconds: u64,
    #[serde(default)]
    pub operation_id: String,
    #[serde(default)]
    pub operation_worktree_name: String,
    #[serde(default)]
    pub operation_created_at: String,
}

#[derive(Debug, Default)]
pub struct ForgeComplete {
    pub status: String,
    pub remote_url: String,
    pub remote_number: Option<i32>,
    pub result_sha: String,
    pub commit_sha: String,
    pub message: String,
    pub pr_status: String,
    pub head_sha: String,
    pub mergeable: Option<bool>,
    pub ci_status: String,
    pub pr_title: String,
    pub metadata: Value,
}

pub async fn accept_forge_action(
    client: &Client,
    hub: &str,
    token: &str,
) -> Result<Option<AcceptedForgeAction>> {
    let url = format!("{}/v1/forge-actions/accept", hub.trim_end_matches('/'));
    let res = client
        .post(&url)
        .header("Authorization", format!("Bearer {token}"))
        .send()
        .await
        .context("forge-actions accept")?;
    if res.status() == StatusCode::NO_CONTENT {
        return Ok(None);
    }
    if !res.status().is_success() {
        let status = res.status();
        let body = res.text().await.unwrap_or_default();
        return Err(anyhow!("forge-actions accept {status}: {body}"));
    }
    Ok(Some(res.json().await.context("decode forge action")?))
}

pub async fn report_forge_action(
    client: &Client,
    hub: &str,
    token: &str,
    id: &str,
    report: &ForgeComplete,
) -> Result<()> {
    let url = format!("{}/v1/forge-actions/{id}", hub.trim_end_matches('/'));
    let mut body = json!({
        "status": report.status,
        "remote_url": report.remote_url,
        "result_sha": report.result_sha,
        "commit_sha": report.commit_sha,
        "message": report.message,
        "metadata": report.metadata,
        "pr_status": report.pr_status,
        "head_sha": report.head_sha,
        "ci_status": report.ci_status,
        "pr_title": report.pr_title,
    });
    if let Some(n) = report.remote_number {
        body["remote_number"] = json!(n);
    }
    if let Some(m) = report.mergeable {
        body["mergeable"] = json!(m);
    }
    let mut backoff = 1u64;
    loop {
        match client
            .patch(&url)
            .header("Authorization", format!("Bearer {token}"))
            .json(&body)
            .send()
            .await
        {
            Ok(res) if res.status().is_success() => {
                return Ok(());
            }
            Ok(res)
                if res.status().is_client_error()
                    && !matches!(
                        res.status(),
                        StatusCode::REQUEST_TIMEOUT | StatusCode::TOO_MANY_REQUESTS
                    ) =>
            {
                let status = res.status();
                let text = res.text().await.unwrap_or_default();
                return Err(anyhow!("forge-actions complete {status}: {text}"));
            }
            _ => {}
        }
        sleep(Duration::from_secs(backoff)).await;
        backoff = (backoff * 2).min(30);
    }
}

pub async fn heartbeat_forge_action(
    client: &Client,
    hub: &str,
    token: &str,
    id: &str,
    lease_seconds: u64,
) -> Result<()> {
    let url = format!(
        "{}/v1/forge-actions/{id}/heartbeat",
        hub.trim_end_matches('/')
    );
    let interval = env::var("UFO_ROVER_HEARTBEAT_SECONDS")
        .ok()
        .and_then(|v| v.parse::<u64>().ok())
        .filter(|&n| n > 0)
        .unwrap_or(5)
        .min((lease_seconds / 2).max(1));
    let mut last_renewed = Instant::now();
    loop {
        sleep(Duration::from_secs(interval)).await;
        let renewed = match client
            .put(&url)
            .header("Authorization", format!("Bearer {token}"))
            .send()
            .await
        {
            Ok(res) if res.status().is_success() => true,
            Ok(res)
                if matches!(
                    res.status(),
                    StatusCode::NOT_FOUND | StatusCode::UNAUTHORIZED
                ) =>
            {
                return Err(anyhow!("forge action lease lost"));
            }
            Ok(res) if res.status() == StatusCode::UPGRADE_REQUIRED => {
                let status = res.status();
                let text = res.text().await.unwrap_or_default();
                return Err(anyhow!("forge action heartbeat {status}: {text}"));
            }
            _ => false,
        };
        if renewed {
            last_renewed = Instant::now();
        } else if forge_lease_renewal_timed_out(last_renewed.elapsed(), lease_seconds) {
            return Err(anyhow!("forge action lease renewal timed out"));
        }
    }
}

pub(crate) fn forge_lease_renewal_timed_out(elapsed: Duration, lease_seconds: u64) -> bool {
    elapsed >= Duration::from_secs((lease_seconds / 2).max(1))
}

pub async fn execute_forge_action(
    client: &Client,
    action: &AcceptedForgeAction,
    operation_directory: Option<&Path>,
    source_worktree: Option<&Path>,
) -> ForgeComplete {
    let token = match resolve_token(action) {
        Ok(t) => t,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: e.to_string(),
                ..Default::default()
            };
        }
    };
    let work_dir = operation_directory
        .filter(|p| p.is_dir())
        .or(source_worktree.filter(|p| p.is_dir()));

    match action.kind.as_str() {
        "push_head_branch" => match work_dir {
            Some(dir) => push_head_branch(dir, action, &token).await,
            None => ForgeComplete {
                status: "failed".into(),
                message: "no operation or source directory for push".into(),
                ..Default::default()
            },
        },
        "open_pull_request" => open_pull_request(client, action, &token).await,
        "sync_pull_request" => sync_pull_request(client, action, &token).await,
        "discover_pull_request" => discover_pull_request(client, action, &token).await,
        "merge_pull_request" => merge_pull_request(client, action, &token).await,
        "update_base_branch" => match work_dir.or(source_worktree) {
            Some(dir) => update_base_branch(dir, action, &token).await,
            None => ForgeComplete {
                status: "failed".into(),
                message: "no directory for update_base_branch".into(),
                ..Default::default()
            },
        },
        "merge_head_into_base_branch" => match work_dir {
            Some(dir) => merge_head_into_base_branch(dir, action, &token).await,
            None => ForgeComplete {
                status: "failed".into(),
                message: "no directory for merge_head_into_base_branch".into(),
                ..Default::default()
            },
        },
        other => ForgeComplete {
            status: "failed".into(),
            message: format!("unknown forge action kind: {other}"),
            ..Default::default()
        },
    }
}

fn resolve_token(action: &AcceptedForgeAction) -> Result<String> {
    let name = if action.credential_name.is_empty() {
        "UFO_ROVER_FORGE_TOKEN"
    } else {
        action.credential_name.as_str()
    };
    env::var(name).with_context(|| format!("missing credential env {name}"))
}

async fn run_pre_ship_checks(dir: &Path, action: &AcceptedForgeAction) -> Option<ForgeComplete> {
    crate::run_bash_check_commands(
        dir,
        &action.checks_commands,
        action.checks_timeout_seconds,
        "pre-ship",
    )
    .await
    .map(|message| ForgeComplete {
        status: "failed".into(),
        message,
        ..Default::default()
    })
}

fn api_headers(token: &str) -> HeaderMap {
    let mut h = HeaderMap::new();
    h.insert(USER_AGENT, HeaderValue::from_static("ufo-rover"));
    h.insert(
        ACCEPT,
        HeaderValue::from_static("application/vnd.github+json"),
    );
    if let Ok(v) = HeaderValue::from_str(&format!("Bearer {token}")) {
        h.insert(AUTHORIZATION, v);
    }
    h
}

async fn push_head_branch(dir: &Path, action: &AcceptedForgeAction, token: &str) -> ForgeComplete {
    if let Some(fail) = run_pre_ship_checks(dir, action).await {
        return fail;
    }
    let remote = match remote_https_url(action) {
        Ok(u) => u,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: e.to_string(),
                ..Default::default()
            };
        }
    };
    let branch = action.head_branch.trim();
    if branch.is_empty() {
        return ForgeComplete {
            status: "failed".into(),
            message: "head_branch required".into(),
            ..Default::default()
        };
    }
    let want = action.commit_sha.trim();
    let sha = if !want.is_empty() {
        if let Err(e) = git(
            dir,
            &["rev-parse", "--verify", &format!("{want}^{{commit}}")],
        )
        .await
        {
            return ForgeComplete {
                status: "failed".into(),
                message: format!("commit_sha {want} not in source repo: {e:#}"),
                ..Default::default()
            };
        }
        want.to_string()
    } else {
        git(dir, &["rev-parse", "HEAD"])
            .await
            .unwrap_or_default()
            .trim()
            .to_string()
    };
    if sha.is_empty() {
        return ForgeComplete {
            status: "failed".into(),
            message: "no commit to push".into(),
            ..Default::default()
        };
    }
    if let Err(e) = git(dir, &["branch", "-f", branch, &sha]).await {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("git branch -f {branch} {sha}: {e:#}"),
            ..Default::default()
        };
    }
    let lease_tip = remote_branch_lease_tip(dir, &remote, branch, token).await;
    let lease = match lease_tip {
        Some(tip) => format!("--force-with-lease=refs/heads/{branch}:{tip}"),
        None => format!("--force-with-lease=refs/heads/{branch}:"),
    };
    if let Err(e) = git_auth(
        dir,
        token,
        &[
            "push",
            lease.as_str(),
            remote.as_str(),
            &format!("{sha}:refs/heads/{branch}"),
        ],
    )
    .await
    {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("git push: {e:#}"),
            ..Default::default()
        };
    }
    ForgeComplete {
        status: "succeeded".into(),
        commit_sha: sha.clone(),
        result_sha: sha,
        message: format!("pushed {branch}"),
        ..Default::default()
    }
}

async fn remote_branch_lease_tip(
    dir: &Path,
    remote: &str,
    branch: &str,
    token: &str,
) -> Option<String> {
    if git_auth(
        dir,
        token,
        &["fetch", remote, &format!("refs/heads/{branch}")],
    )
    .await
    .is_ok()
    {
        let tip = git(dir, &["rev-parse", "FETCH_HEAD"])
            .await
            .ok()?
            .trim()
            .to_string();
        if !tip.is_empty() {
            return Some(tip);
        }
    }
    None
}

fn git_url_scheme(base_url: &str) -> &'static str {
    if base_url
        .trim_start()
        .to_ascii_lowercase()
        .starts_with("http://")
    {
        "http"
    } else {
        "https"
    }
}

fn git_host(base_url: &str, default_host: &str) -> String {
    base_url
        .trim_start_matches("https://")
        .trim_start_matches("http://")
        .trim_start_matches("HTTPS://")
        .trim_start_matches("HTTP://")
        .split('/')
        .next()
        .filter(|h| !h.is_empty())
        .unwrap_or(default_host)
        .to_string()
}

pub(crate) fn remote_https_url(action: &AcceptedForgeAction) -> Result<String> {
    let provider = action.provider.to_lowercase();
    let repo = action.repo.trim().trim_matches('/');
    let scheme = git_url_scheme(&action.base_url);
    match provider.as_str() {
        "github" => {
            let host = if action.base_url.contains("api.github.com") {
                "github.com".to_string()
            } else {
                git_host(&action.base_url, "github.com")
            };
            Ok(format!("{scheme}://x-access-token@{host}/{repo}.git"))
        }
        "gitlab" => {
            let host = git_host(&action.base_url, "gitlab.com");
            let path = repo.trim_start_matches('/');
            Ok(format!("{scheme}://oauth2@{host}/{path}.git"))
        }
        other => Err(anyhow!("unsupported provider {other}")),
    }
}

fn askpass_script() -> Result<PathBuf> {
    let dir = env::temp_dir();
    #[cfg(not(windows))]
    let path = dir.join(format!("ufo-git-askpass-{}.sh", std::process::id()));
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::write(
            &path,
            "#!/bin/sh\nprintf %s \"$UFO_GIT_ASKPASS_PASSWORD\"\n",
        )?;
        fs::set_permissions(&path, fs::Permissions::from_mode(0o700))?;
    }
    #[cfg(windows)]
    {
        let path = dir.join(format!("ufo-git-askpass-{}.cmd", std::process::id()));
        fs::write(
            &path,
            "@echo off\r\n<nul set /p=%UFO_GIT_ASKPASS_PASSWORD%\r\n",
        )?;
        return Ok(path);
    }
    #[cfg(not(windows))]
    {
        Ok(path)
    }
}

async fn git_auth(dir: &Path, token: &str, args: &[&str]) -> Result<String> {
    let askpass = askpass_script()?;
    let mut owned: Vec<String> = Vec::with_capacity(args.len() + 2);
    owned.push("-c".into());
    owned.push("credential.helper=".into());
    for a in args {
        owned.push((*a).to_string());
    }
    let arg_refs: Vec<&str> = owned.iter().map(String::as_str).collect();
    let env = [
        ("GIT_TERMINAL_PROMPT", "0".into()),
        ("GIT_ASKPASS", askpass.to_string_lossy().into_owned()),
        ("UFO_GIT_ASKPASS_PASSWORD", token.to_string()),
        ("GCM_INTERACTIVE", "never".into()),
    ];
    let result = git_env(dir, &arg_refs, &env).await;
    let _ = fs::remove_file(&askpass);
    result
}

pub(crate) fn percent_encode(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for b in s.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(b as char)
            }
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
}

async fn open_pull_request(
    client: &Client,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    match action.provider.to_lowercase().as_str() {
        "github" => open_github_pr(client, action, token).await,
        "gitlab" => open_gitlab_mr(client, action, token).await,
        other => ForgeComplete {
            status: "failed".into(),
            message: format!("unsupported provider {other}"),
            ..Default::default()
        },
    }
}

async fn open_github_pr(
    client: &Client,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    let base = action.base_url.trim_end_matches('/');
    let url = format!("{base}/repos/{}/pulls", action.repo.trim_matches('/'));
    let res = client
        .post(&url)
        .headers(api_headers(token))
        .json(&json!({
            "title": if action.title.is_empty() { &action.head_branch } else { &action.title },
            "head": action.head_branch,
            "base": action.base_branch,
            "body": action.body,
        }))
        .send()
        .await;
    let res = match res {
        Ok(r) => r,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: e.to_string(),
                ..Default::default()
            };
        }
    };
    let status = res.status();
    let body: Value = res.json().await.unwrap_or(json!({}));
    if status.is_success() {
        return github_pr_complete(&body, "opened pull request");
    }
    if status.as_u16() == 422 {
        let mut recovered = sync_github_pr(client, action, token).await;
        if recovered.status == "succeeded" {
            recovered.message = "recovered existing pull request".into();
            return recovered;
        }
    }
    ForgeComplete {
        status: "failed".into(),
        message: format!("github open PR {status}: {body}"),
        ..Default::default()
    }
}

fn github_pr_complete(body: &Value, message: &str) -> ForgeComplete {
    ForgeComplete {
        status: "succeeded".into(),
        remote_url: body
            .get("html_url")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        remote_number: body
            .get("number")
            .and_then(|v| v.as_i64())
            .map(|n| n as i32),
        head_sha: body
            .pointer("/head/sha")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        pr_status: if body.get("draft").and_then(|v| v.as_bool()) == Some(true) {
            "draft".into()
        } else {
            "open".into()
        },
        pr_title: body
            .get("title")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        mergeable: body.get("mergeable").and_then(|v| v.as_bool()),
        ci_status: "pending".into(),
        message: message.into(),
        ..Default::default()
    }
}

async fn open_gitlab_mr(
    client: &Client,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    let base = action.base_url.trim_end_matches('/');
    let project = action.repo.trim_matches('/').replace('/', "%2F");
    let url = format!("{base}/projects/{project}/merge_requests");
    let mut headers = HeaderMap::new();
    headers.insert(USER_AGENT, HeaderValue::from_static("ufo-rover"));
    if let Ok(v) = HeaderValue::from_str(token) {
        headers.insert("PRIVATE-TOKEN", v);
    }
    let res = client
        .post(&url)
        .headers(headers)
        .json(&json!({
            "title": if action.title.is_empty() { &action.head_branch } else { &action.title },
            "source_branch": action.head_branch,
            "target_branch": action.base_branch,
            "description": action.body,
        }))
        .send()
        .await;
    let res = match res {
        Ok(r) => r,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: e.to_string(),
                ..Default::default()
            };
        }
    };
    let status = res.status();
    let body: Value = res.json().await.unwrap_or(json!({}));
    if status.is_success() {
        return gitlab_mr_complete(&body, "opened merge request");
    }
    if status.as_u16() == 409 {
        let mut recovered = sync_gitlab_mr(client, action, token).await;
        if recovered.status == "succeeded" {
            recovered.message = "recovered existing merge request".into();
            return recovered;
        }
    }
    ForgeComplete {
        status: "failed".into(),
        message: format!("gitlab open MR {status}: {body}"),
        ..Default::default()
    }
}

fn gitlab_mr_complete(body: &Value, message: &str) -> ForgeComplete {
    ForgeComplete {
        status: "succeeded".into(),
        remote_url: body
            .get("web_url")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        remote_number: body.get("iid").and_then(|v| v.as_i64()).map(|n| n as i32),
        head_sha: body
            .get("sha")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        pr_status: "open".into(),
        pr_title: body
            .get("title")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        mergeable: body
            .get("detailed_merge_status")
            .and_then(|v| v.as_str())
            .map(|s| s == "mergeable"),
        ci_status: "pending".into(),
        message: message.into(),
        ..Default::default()
    }
}

async fn sync_pull_request(
    client: &Client,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    match action.provider.to_lowercase().as_str() {
        "github" => sync_github_pr(client, action, token).await,
        "gitlab" => sync_gitlab_mr(client, action, token).await,
        other => ForgeComplete {
            status: "failed".into(),
            message: format!("unsupported provider {other}"),
            ..Default::default()
        },
    }
}

async fn discover_pull_request(
    client: &Client,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    match action.provider.to_lowercase().as_str() {
        "github" => discover_github_pr(client, action, token).await,
        "gitlab" => discover_gitlab_mr(client, action, token).await,
        other => ForgeComplete {
            status: "failed".into(),
            message: format!("unsupported provider {other}"),
            ..Default::default()
        },
    }
}

pub(crate) fn discover_head_sha_matches(want_sha: &str, head_sha: &str) -> bool {
    let want = want_sha.trim().to_ascii_lowercase();
    if want.is_empty() {
        return true;
    }
    head_sha.trim().to_ascii_lowercase() == want
}

pub(crate) fn github_pr_mergeable(detail: &Value) -> Option<bool> {
    detail.get("mergeable").and_then(|v| v.as_bool())
}

pub(crate) fn gitlab_mr_mergeable(detail: &Value) -> Option<bool> {
    detail
        .get("detailed_merge_status")
        .and_then(|v| v.as_str())
        .map(|s| s == "mergeable")
}

pub(crate) fn github_list_pr_identity(pr: &Value) -> (String, String, String) {
    let base = pr
        .pointer("/base/ref")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();
    let head = pr
        .pointer("/head/ref")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();
    let sha = pr
        .pointer("/head/sha")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();
    (base, head, sha)
}

pub(crate) fn gitlab_list_mr_identity(mr: &Value) -> (String, String, String) {
    let base = mr
        .get("target_branch")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();
    let head = mr
        .get("source_branch")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();
    let sha = mr
        .get("sha")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();
    (base, head, sha)
}

async fn discover_github_pr(
    client: &Client,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    let base = action.base_url.trim_end_matches('/');
    let repo = action.repo.trim_matches('/');
    let owner = repo.split('/').next().unwrap_or("");
    let head = action.head_branch.trim();
    let want_base = action.base_branch.trim();
    let url = format!(
        "{base}/repos/{repo}/pulls?head={owner}:{}&state=open&per_page=20",
        percent_encode(head)
    );
    let res = match client.get(&url).headers(api_headers(token)).send().await {
        Ok(r) => r,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: e.to_string(),
                ..Default::default()
            };
        }
    };
    if !res.status().is_success() {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("github list pulls {}", res.status()),
            ..Default::default()
        };
    }
    let body: Value = match res.json().await {
        Ok(body) => body,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: format!("decode github pull request list: {e}"),
                ..Default::default()
            };
        }
    };
    let want_sha = action.commit_sha.trim().to_ascii_lowercase();
    let mut matches: Vec<Value> = Vec::new();
    let Some(pull_requests) = body.as_array() else {
        return ForgeComplete {
            status: "failed".into(),
            message: "decode github pull request list: expected array".into(),
            ..Default::default()
        };
    };
    for pr in pull_requests {
        let (pr_base, pr_head, pr_sha) = github_list_pr_identity(pr);
        if pr_head != head {
            continue;
        }
        if !want_base.is_empty() && pr_base != want_base {
            continue;
        }
        if !discover_head_sha_matches(&want_sha, &pr_sha) {
            continue;
        }
        matches.push(pr.clone());
    }
    match matches.len() {
        1 => {
            let pr = &matches[0];
            let number = pr.get("number").and_then(|v| v.as_i64()).map(|n| n as i32);
            let mut mergeable = None;
            if let Some(n) = number {
                let one = format!("{base}/repos/{repo}/pulls/{n}");
                if let Ok(r) = client.get(&one).headers(api_headers(token)).send().await
                    && let Ok(p) = r.json::<Value>().await
                {
                    mergeable = github_pr_mergeable(&p);
                }
            }
            ForgeComplete {
                status: "succeeded".into(),
                remote_url: pr
                    .get("html_url")
                    .and_then(|v| v.as_str())
                    .unwrap_or("")
                    .to_string(),
                remote_number: number,
                head_sha: pr
                    .pointer("/head/sha")
                    .and_then(|v| v.as_str())
                    .unwrap_or("")
                    .to_string(),
                pr_status: "open".into(),
                pr_title: pr
                    .get("title")
                    .and_then(|v| v.as_str())
                    .unwrap_or("")
                    .to_string(),
                mergeable,
                ci_status: "unknown".into(),
                message: "discovered open pull request".into(),
                ..Default::default()
            }
        }
        0 => ForgeComplete {
            status: "failed".into(),
            message: if want_sha.is_empty() {
                format!("no open pull request for {head} → {want_base}")
            } else {
                format!("no open pull request for {head} → {want_base} at {want_sha}")
            },
            ..Default::default()
        },
        n => ForgeComplete {
            status: "failed".into(),
            message: format!("{n} open pull requests for {head} → {want_base}; not linking"),
            ..Default::default()
        },
    }
}

async fn discover_gitlab_mr(
    client: &Client,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    let base = action.base_url.trim_end_matches('/');
    let project = action.repo.trim_matches('/').replace('/', "%2F");
    let head = action.head_branch.trim();
    let want_base = action.base_branch.trim();
    let url = format!(
        "{base}/projects/{project}/merge_requests?source_branch={}&state=opened&per_page=20",
        percent_encode(head)
    );
    let mut headers = HeaderMap::new();
    headers.insert(USER_AGENT, HeaderValue::from_static("ufo-rover"));
    if let Ok(v) = HeaderValue::from_str(token) {
        headers.insert("PRIVATE-TOKEN", v);
    }
    let res = match client.get(&url).headers(headers).send().await {
        Ok(r) => r,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: e.to_string(),
                ..Default::default()
            };
        }
    };
    if !res.status().is_success() {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("gitlab list MRs {}", res.status()),
            ..Default::default()
        };
    }
    let body: Value = match res.json().await {
        Ok(body) => body,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: format!("decode gitlab merge request list: {e}"),
                ..Default::default()
            };
        }
    };
    let want_sha = action.commit_sha.trim().to_ascii_lowercase();
    let mut matches: Vec<Value> = Vec::new();
    let Some(merge_requests) = body.as_array() else {
        return ForgeComplete {
            status: "failed".into(),
            message: "decode gitlab merge request list: expected array".into(),
            ..Default::default()
        };
    };
    for mr in merge_requests {
        let (mr_base, mr_head, mr_sha) = gitlab_list_mr_identity(mr);
        if mr_head != head {
            continue;
        }
        if !want_base.is_empty() && mr_base != want_base {
            continue;
        }
        if !discover_head_sha_matches(&want_sha, &mr_sha) {
            continue;
        }
        matches.push(mr.clone());
    }
    match matches.len() {
        1 => {
            let mr = &matches[0];
            ForgeComplete {
                status: "succeeded".into(),
                remote_url: mr
                    .get("web_url")
                    .and_then(|v| v.as_str())
                    .unwrap_or("")
                    .to_string(),
                remote_number: mr.get("iid").and_then(|v| v.as_i64()).map(|n| n as i32),
                head_sha: mr
                    .get("sha")
                    .and_then(|v| v.as_str())
                    .unwrap_or("")
                    .to_string(),
                pr_status: "open".into(),
                pr_title: mr
                    .get("title")
                    .and_then(|v| v.as_str())
                    .unwrap_or("")
                    .to_string(),
                mergeable: gitlab_mr_mergeable(mr),
                ci_status: "unknown".into(),
                message: "discovered open merge request".into(),
                ..Default::default()
            }
        }
        0 => ForgeComplete {
            status: "failed".into(),
            message: if want_sha.is_empty() {
                format!("no open merge request for {head} → {want_base}")
            } else {
                format!("no open merge request for {head} → {want_base} at {want_sha}")
            },
            ..Default::default()
        },
        n => ForgeComplete {
            status: "failed".into(),
            message: format!("{n} open merge requests for {head} → {want_base}; not linking"),
            ..Default::default()
        },
    }
}

async fn sync_github_pr(
    client: &Client,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    let base = action.base_url.trim_end_matches('/');
    let repo = action.repo.trim_matches('/');
    let owner = repo.split('/').next().unwrap_or("");
    let url = format!(
        "{base}/repos/{repo}/pulls?head={owner}:{}&state=open",
        percent_encode(&action.head_branch)
    );
    let res = client.get(&url).headers(api_headers(token)).send().await;
    let res = match res {
        Ok(r) => r,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: e.to_string(),
                ..Default::default()
            };
        }
    };
    if !res.status().is_success() {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("github list pulls {}", res.status()),
            ..Default::default()
        };
    }
    let body: Value = match res.json().await {
        Ok(body) => body,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: format!("decode github pull request list: {e}"),
                ..Default::default()
            };
        }
    };
    let Some(pull_requests) = body.as_array() else {
        return ForgeComplete {
            status: "failed".into(),
            message: "decode github pull request list: expected array".into(),
            ..Default::default()
        };
    };
    let matching: Vec<&Value> = pull_requests
        .iter()
        .filter(|pr| {
            let (base, head, sha) = github_list_pr_identity(pr);
            head == action.head_branch
                && (action.base_branch.is_empty() || base == action.base_branch)
                && discover_head_sha_matches(&action.commit_sha, &sha)
        })
        .collect();
    if matching.len() != 1 {
        return ForgeComplete {
            status: "failed".into(),
            message: format!(
                "expected one open pull request for {} → {}; found {}",
                action.head_branch,
                action.base_branch,
                matching.len()
            ),
            ..Default::default()
        };
    }
    let pr = matching[0].clone();
    let number = pr.get("number").and_then(|v| v.as_i64()).map(|n| n as i32);
    let sha = pr
        .pointer("/head/sha")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();
    let ci = if sha.is_empty() {
        "unknown".to_string()
    } else {
        let checks_url = format!("{base}/repos/{repo}/commits/{sha}/status");
        match client
            .get(&checks_url)
            .headers(api_headers(token))
            .send()
            .await
        {
            Ok(r) => {
                let st: Value = r.json().await.unwrap_or(json!({}));
                match st
                    .get("state")
                    .and_then(|v| v.as_str())
                    .unwrap_or("pending")
                {
                    "success" => "success".into(),
                    "failure" | "error" => "failure".into(),
                    "pending" => "pending".into(),
                    _ => "unknown".into(),
                }
            }
            Err(_) => "unknown".into(),
        }
    };
    let mut mergeable = github_pr_mergeable(&pr);
    if let Some(n) = number {
        let one = format!("{base}/repos/{repo}/pulls/{n}");
        if let Ok(r) = client.get(&one).headers(api_headers(token)).send().await
            && let Ok(p) = r.json::<Value>().await
        {
            mergeable = github_pr_mergeable(&p);
        }
    }
    ForgeComplete {
        status: "succeeded".into(),
        remote_url: pr
            .get("html_url")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        remote_number: number,
        head_sha: sha,
        pr_status: "open".into(),
        pr_title: pr
            .get("title")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        mergeable,
        ci_status: ci,
        message: "synced pull request".into(),
        ..Default::default()
    }
}

async fn sync_gitlab_mr(
    client: &Client,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    let base = action.base_url.trim_end_matches('/');
    let project = action.repo.trim_matches('/').replace('/', "%2F");
    let url = format!(
        "{base}/projects/{project}/merge_requests?source_branch={}&state=opened",
        percent_encode(&action.head_branch)
    );
    let mut headers = HeaderMap::new();
    headers.insert(USER_AGENT, HeaderValue::from_static("ufo-rover"));
    if let Ok(v) = HeaderValue::from_str(token) {
        headers.insert("PRIVATE-TOKEN", v);
    }
    let res = match client.get(&url).headers(headers.clone()).send().await {
        Ok(r) => r,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: e.to_string(),
                ..Default::default()
            };
        }
    };
    if !res.status().is_success() {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("gitlab list MRs {}", res.status()),
            ..Default::default()
        };
    }
    let body: Value = match res.json().await {
        Ok(body) => body,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: format!("decode gitlab merge request list: {e}"),
                ..Default::default()
            };
        }
    };
    let Some(merge_requests) = body.as_array() else {
        return ForgeComplete {
            status: "failed".into(),
            message: "decode gitlab merge request list: expected array".into(),
            ..Default::default()
        };
    };
    let matching: Vec<&Value> = merge_requests
        .iter()
        .filter(|mr| {
            let (base, head, sha) = gitlab_list_mr_identity(mr);
            head == action.head_branch
                && (action.base_branch.is_empty() || base == action.base_branch)
                && discover_head_sha_matches(&action.commit_sha, &sha)
        })
        .collect();
    if matching.len() != 1 {
        return ForgeComplete {
            status: "failed".into(),
            message: format!(
                "expected one open merge request for {} → {}; found {}",
                action.head_branch,
                action.base_branch,
                matching.len()
            ),
            ..Default::default()
        };
    }
    let mr = matching[0].clone();
    let pipeline = mr
        .pointer("/head_pipeline/status")
        .and_then(|v| v.as_str())
        .unwrap_or("unknown");
    let ci = match pipeline {
        "success" => "success",
        "failed" | "canceled" => "failure",
        "running" | "pending" | "created" => "pending",
        _ => "unknown",
    };
    ForgeComplete {
        status: "succeeded".into(),
        remote_url: mr
            .get("web_url")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        remote_number: mr.get("iid").and_then(|v| v.as_i64()).map(|n| n as i32),
        head_sha: mr
            .get("sha")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        pr_status: "open".into(),
        pr_title: mr
            .get("title")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string(),
        mergeable: mr
            .get("detailed_merge_status")
            .and_then(|v| v.as_str())
            .map(|s| s == "mergeable"),
        ci_status: ci.into(),
        message: "synced merge request".into(),
        ..Default::default()
    }
}

async fn merge_pull_request(
    client: &Client,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    match action.provider.to_lowercase().as_str() {
        "github" => {
            let synced = sync_github_pr(client, action, token).await;
            let Some(n) = synced.remote_number else {
                return ForgeComplete {
                    status: "failed".into(),
                    message: "cannot find PR number to merge".into(),
                    ..Default::default()
                };
            };
            let base = action.base_url.trim_end_matches('/');
            let url = format!(
                "{base}/repos/{}/pulls/{n}/merge",
                action.repo.trim_matches('/')
            );
            let res = client
                .put(&url)
                .headers(api_headers(token))
                .json(&json!({"merge_method": "merge"}))
                .send()
                .await;
            match res {
                Ok(r) if r.status().is_success() => {
                    let body: Value = r.json().await.unwrap_or(json!({}));
                    ForgeComplete {
                        status: "succeeded".into(),
                        remote_number: Some(n),
                        remote_url: synced.remote_url,
                        result_sha: body
                            .get("sha")
                            .and_then(|v| v.as_str())
                            .unwrap_or("")
                            .to_string(),
                        pr_status: "merged".into(),
                        message: "merged pull request".into(),
                        ..Default::default()
                    }
                }
                Ok(r) => ForgeComplete {
                    status: "failed".into(),
                    message: format!("github merge {}", r.status()),
                    remote_number: Some(n),
                    ..Default::default()
                },
                Err(e) => ForgeComplete {
                    status: "failed".into(),
                    message: e.to_string(),
                    ..Default::default()
                },
            }
        }
        "gitlab" => {
            let synced = sync_gitlab_mr(client, action, token).await;
            let Some(n) = synced.remote_number else {
                return ForgeComplete {
                    status: "failed".into(),
                    message: "cannot find MR iid to merge".into(),
                    ..Default::default()
                };
            };
            let base = action.base_url.trim_end_matches('/');
            let project = action.repo.trim_matches('/').replace('/', "%2F");
            let url = format!("{base}/projects/{project}/merge_requests/{n}/merge");
            let mut headers = HeaderMap::new();
            headers.insert(USER_AGENT, HeaderValue::from_static("ufo-rover"));
            if let Ok(v) = HeaderValue::from_str(token) {
                headers.insert("PRIVATE-TOKEN", v);
            }
            match client.put(&url).headers(headers).send().await {
                Ok(r) if r.status().is_success() => {
                    let body: Value = r.json().await.unwrap_or(json!({}));
                    let result_sha = body
                        .get("merge_commit_sha")
                        .and_then(|v| v.as_str())
                        .filter(|s| !s.is_empty())
                        .or_else(|| body.get("sha").and_then(|v| v.as_str()))
                        .unwrap_or(synced.head_sha.as_str())
                        .to_string();
                    ForgeComplete {
                        status: "succeeded".into(),
                        remote_number: Some(n),
                        remote_url: synced.remote_url,
                        result_sha,
                        pr_status: "merged".into(),
                        message: "merged merge request".into(),
                        ..Default::default()
                    }
                }
                Ok(r) => ForgeComplete {
                    status: "failed".into(),
                    message: format!("gitlab merge {}", r.status()),
                    remote_number: Some(n),
                    ..Default::default()
                },
                Err(e) => ForgeComplete {
                    status: "failed".into(),
                    message: e.to_string(),
                    ..Default::default()
                },
            }
        }
        other => ForgeComplete {
            status: "failed".into(),
            message: format!("unsupported provider {other}"),
            ..Default::default()
        },
    }
}

async fn update_base_branch(
    dir: &Path,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    let remote = match remote_https_url(action) {
        Ok(u) => u,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: e.to_string(),
                ..Default::default()
            };
        }
    };
    let base = action.base_branch.trim();
    let reference = action.head_branch.trim();
    if base.is_empty() {
        return ForgeComplete {
            status: "failed".into(),
            message: "base_branch required".into(),
            ..Default::default()
        };
    }
    if reference.is_empty() {
        return ForgeComplete {
            status: "failed".into(),
            message: "head_branch (ship_base.reference) required".into(),
            ..Default::default()
        };
    }
    let sync = match action.ship_base_sync.trim().to_ascii_lowercase().as_str() {
        "" | "merge" => "merge",
        "rebase" => "rebase",
        "reset" => "reset",
        other => {
            return ForgeComplete {
                status: "failed".into(),
                message: format!("unsupported ship_base.sync {other}"),
                ..Default::default()
            };
        }
    };
    if let Err(e) = git_auth(
        dir,
        token,
        &[
            "fetch",
            remote.as_str(),
            &format!("+refs/heads/{reference}:refs/ufo/ship-ref"),
        ],
    )
    .await
    {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("fetch reference {reference}: {e:#}"),
            ..Default::default()
        };
    }
    if let Err(e) = git(dir, &["rev-parse", "refs/ufo/ship-ref"]).await {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("resolve reference {reference}: {e:#}"),
            ..Default::default()
        };
    }
    let base_remote = git_auth(
        dir,
        token,
        &[
            "fetch",
            remote.as_str(),
            &format!("+refs/heads/{base}:refs/ufo/ship-base"),
        ],
    )
    .await
    .is_ok();
    let lease_tip = if base_remote {
        git(dir, &["rev-parse", "refs/ufo/ship-base"])
            .await
            .unwrap_or_default()
            .trim()
            .to_string()
    } else {
        String::new()
    };
    if base_remote {
        if let Err(e) = git(dir, &["checkout", "-B", base, "refs/ufo/ship-base"]).await {
            return ForgeComplete {
                status: "failed".into(),
                message: format!("checkout {base}: {e:#}"),
                ..Default::default()
            };
        }
    } else if let Err(e) = git(dir, &["checkout", "-B", base, "refs/ufo/ship-ref"]).await {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("create {base} from {reference}: {e:#}"),
            ..Default::default()
        };
    }
    if base_remote {
        let sync_err = match sync {
            "reset" => git(dir, &["reset", "--hard", "refs/ufo/ship-ref"])
                .await
                .err(),
            "merge" => git(
                dir,
                &[
                    "merge",
                    "--no-edit",
                    "-m",
                    &format!("UFO sync {base} from {reference}"),
                    "refs/ufo/ship-ref",
                ],
            )
            .await
            .err(),
            _ => git(dir, &["rebase", "refs/ufo/ship-ref"]).await.err(),
        };
        if let Some(e) = sync_err {
            let _ = git(dir, &["rebase", "--abort"]).await;
            let _ = git(dir, &["merge", "--abort"]).await;
            return ForgeComplete {
                status: "conflicted".into(),
                message: format!("ship base sync ({sync}) conflict: {e:#}"),
                ..Default::default()
            };
        }
    }
    let sha = git(dir, &["rev-parse", "HEAD"])
        .await
        .unwrap_or_default()
        .trim()
        .to_string();
    if !lease_tip.is_empty() && lease_tip == sha {
        return ForgeComplete {
            status: "succeeded".into(),
            result_sha: sha,
            message: format!("{base} already tracks {reference}"),
            ..Default::default()
        };
    }
    let lease = if lease_tip.is_empty() {
        format!("--force-with-lease=refs/heads/{base}:")
    } else {
        format!("--force-with-lease=refs/heads/{base}:{lease_tip}")
    };
    if let Err(e) = git_auth(
        dir,
        token,
        &[
            "push",
            lease.as_str(),
            remote.as_str(),
            &format!("{sha}:refs/heads/{base}"),
        ],
    )
    .await
    {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("push {base}: {e:#}"),
            ..Default::default()
        };
    }
    ForgeComplete {
        status: "succeeded".into(),
        result_sha: sha,
        message: format!("synced {base} from {reference} ({sync})"),
        ..Default::default()
    }
}

async fn merge_head_into_base_branch(
    dir: &Path,
    action: &AcceptedForgeAction,
    token: &str,
) -> ForgeComplete {
    let remote = match remote_https_url(action) {
        Ok(u) => u,
        Err(e) => {
            return ForgeComplete {
                status: "failed".into(),
                message: e.to_string(),
                ..Default::default()
            };
        }
    };
    let head = action.head_branch.trim();
    let base = action.base_branch.trim();
    if head.is_empty() || base.is_empty() {
        return ForgeComplete {
            status: "failed".into(),
            message: "head_branch and base_branch required".into(),
            ..Default::default()
        };
    }
    if let Err(e) = git_auth(
        dir,
        token,
        &[
            "fetch",
            remote.as_str(),
            &format!("+refs/heads/{base}:refs/ufo/integrate-base"),
        ],
    )
    .await
    {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("fetch base {base}: {e:#}"),
            ..Default::default()
        };
    }
    if let Err(e) = git_auth(
        dir,
        token,
        &[
            "fetch",
            remote.as_str(),
            &format!("+refs/heads/{head}:refs/ufo/integrate-head"),
        ],
    )
    .await
    {
        let _ = e;
    }
    if let Err(e) = git(dir, &["checkout", "-B", base, "refs/ufo/integrate-base"]).await {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("checkout {base} from fetched tip: {e:#}"),
            ..Default::default()
        };
    }
    let merge_ref = if git(dir, &["rev-parse", "--verify", "refs/ufo/integrate-head"])
        .await
        .is_ok()
    {
        "refs/ufo/integrate-head"
    } else {
        head
    };
    match git(
        dir,
        &[
            "merge",
            "--no-ff",
            merge_ref,
            "-m",
            &format!("UFO integrate {head}"),
        ],
    )
    .await
    {
        Ok(_) => {}
        Err(e) => {
            let _ = git(dir, &["merge", "--abort"]).await;
            return ForgeComplete {
                status: "conflicted".into(),
                message: format!("merge conflict: {e:#}"),
                ..Default::default()
            };
        }
    }
    if let Err(e) = git_auth(
        dir,
        token,
        &["push", remote.as_str(), &format!("HEAD:refs/heads/{base}")],
    )
    .await
    {
        return ForgeComplete {
            status: "failed".into(),
            message: format!("push base: {e:#}"),
            ..Default::default()
        };
    }
    let sha = git(dir, &["rev-parse", "HEAD"])
        .await
        .unwrap_or_default()
        .trim()
        .to_string();
    ForgeComplete {
        status: "succeeded".into(),
        result_sha: sha,
        message: format!("integrated {head} into {base}"),
        ..Default::default()
    }
}
