use anyhow::{Context, Result, anyhow};
use reqwest::header::{ACCEPT, AUTHORIZATION, HeaderMap, HeaderValue, USER_AGENT};
use reqwest::{Client, StatusCode};
use serde::Deserialize;
use serde_json::{Value, json};
use std::env;
use std::fs;
use std::path::{Path, PathBuf};

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
    let res = client
        .patch(&url)
        .header("Authorization", format!("Bearer {token}"))
        .json(&body)
        .send()
        .await
        .context("forge-actions complete")?;
    if !res.status().is_success() {
        let status = res.status();
        let text = res.text().await.unwrap_or_default();
        return Err(anyhow!("forge-actions complete {status}: {text}"));
    }
    Ok(())
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
        "push_branch" => match work_dir {
            Some(dir) => push_branch(dir, action, &token).await,
            None => ForgeComplete {
                status: "failed".into(),
                message: "no operation or source directory for push".into(),
                ..Default::default()
            },
        },
        "open_pull_request" => open_pull_request(client, action, &token).await,
        "sync_pull_request" => sync_pull_request(client, action, &token).await,
        "merge_pull_request" => merge_pull_request(client, action, &token).await,
        "ensure_base_branch" => match work_dir.or(source_worktree) {
            Some(dir) => ensure_base_branch(dir, action, &token).await,
            None => ForgeComplete {
                status: "failed".into(),
                message: "no directory for ensure_base_branch".into(),
                ..Default::default()
            },
        },
        "integrate_into_base" => match work_dir {
            Some(dir) => integrate_into_base(dir, action, &token).await,
            None => ForgeComplete {
                status: "failed".into(),
                message: "no directory for integrate_into_base".into(),
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

async fn push_branch(dir: &Path, action: &AcceptedForgeAction, token: &str) -> ForgeComplete {
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
    let env = [
        ("GIT_TERMINAL_PROMPT", "0".into()),
        ("GIT_ASKPASS", askpass.to_string_lossy().into_owned()),
        ("UFO_GIT_ASKPASS_PASSWORD", token.to_string()),
        ("GCM_INTERACTIVE", "never".into()),
    ];
    let result = git_env(dir, args, &env).await;
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
    let body: Value = res.json().await.unwrap_or(json!([]));
    let pr = body
        .as_array()
        .and_then(|a| a.first())
        .cloned()
        .unwrap_or(json!({}));
    if pr.is_null() || pr.get("number").is_none() {
        return ForgeComplete {
            status: "failed".into(),
            message: "no open pull request for head branch".into(),
            ..Default::default()
        };
    }
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
    let mut mergeable = pr.get("mergeable").and_then(|v| v.as_bool());
    if let Some(n) = number {
        let one = format!("{base}/repos/{repo}/pulls/{n}");
        if let Ok(r) = client.get(&one).headers(api_headers(token)).send().await
            && let Ok(p) = r.json::<Value>().await
        {
            mergeable = p.get("mergeable").and_then(|v| v.as_bool());
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
    let body: Value = res.json().await.unwrap_or(json!([]));
    let mr = body
        .as_array()
        .and_then(|a| a.first())
        .cloned()
        .unwrap_or(json!({}));
    if mr.get("iid").is_none() {
        return ForgeComplete {
            status: "failed".into(),
            message: "no open merge request for head branch".into(),
            ..Default::default()
        };
    }
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

async fn ensure_base_branch(
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

async fn integrate_into_base(
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
