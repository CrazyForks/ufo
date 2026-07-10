<h1 align="center">UFO：统一舰队编排引擎</h1>

<p align="center"><strong>开源的无人值守自动化平台</strong> 🦾🩶</p>

<p align="center">
把 AI 会话串成无人值守的工作闭环：保留上下文，分派工作，并跨迭代交接！
</p>

<p align="center">
  <a href="https://github.com/fengsi/ufo/actions/workflows/ci.yml"><img alt="Build" src="https://img.shields.io/github/actions/workflow/status/fengsi/ufo/ci.yml?logo=github&style=flat-square"></a>
  <a href="https://github.com/fengsi/ufo/releases"><img alt="Release" src="https://img.shields.io/github/v/release/fengsi/ufo?style=flat-square"></a>
  <a href="https://crates.io/crates/ufo-cli"><img alt="crates.io" src="https://img.shields.io/crates/v/ufo-cli?style=flat-square"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/fengsi/ufo?style=flat-square"></a>
  <a href="CHANGELOG.md"><img alt="Status" src="https://img.shields.io/badge/status-beta-blue?style=flat-square"></a>
  <a href="apps/api/go.mod"><img alt="Go" src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&style=flat-square"></a>
  <a href="apps/web/package.json"><img alt="Node" src="https://img.shields.io/badge/Node-20.9%2B-5FA04E?logo=node.js&style=flat-square"></a>
  <a href="apps/rover/Cargo.toml"><img alt="Rust" src="https://img.shields.io/badge/Rust-2024-B7410E?logo=rust&style=flat-square"></a>
  <a href="https://gitmoji.dev"><img alt="Gitmoji" src="https://img.shields.io/badge/commits-gitmoji-FDD563?style=flat-square"></a>
</p>

<p align="center"><strong><a href="README.md">English</a> | 简体中文</strong></p>

![UFO orchestrating a unified
fleet](.github/assets/banner.png)

> **公开 beta。** 核心闭环已经可用。建议使用
> [tagged releases](https://github.com/fengsi/ufo/releases)；1.0 之前API 和 schema
> 仍可能变化，升级注意事项见 [CHANGELOG.md](CHANGELOG.md)。

---

## UFO 是什么？

UFO 把多个 AI 会话串成一个面向复杂工作的无人值守闭环：不只写代码，也能承接日常业务和系统操作。工作落在 **看板** 上，上下文会持续沉淀，每次迭代
都能干净交接；项目数据与凭证继续留在你自己的机器上。

三层结构：

| 层 | 作用 |
| --- | --- |
| **舰队（Fleet）** | 信任边界：人、航行器、任务与行动 |
| **中枢（Hub）** | 控制平面：API 与舰队状态 |
| **看板（Board）** | 面向舰队的 Web UI |

**任务（Mission）** 框定项目。**行动（Operation）** 是看板上的工作项。
**巡航（Routine）** 按计划或在完成后触发工作。**航行器（Rover）** 在本地
运行 **领航员（Pilot）**（Claude Code、Codex、Grok Build 等）。需要时人
依然在舰队中；产品的方向是走向无人值守。

---

## 为什么是 UFO？

大多数 agent 工作流仍然分布在不同会话里：聊天标签页、终端、本地工作树和人工笔记各记一段。单次运行可以完成工作，但交接之间缺少一张统一的全局视图。

| 独立 AI 会话 | UFO |
| --- | --- |
| 上下文留在各自会话里 | **行动** 在舰队里保留共享历史 |
| 交接主要靠人手整理 | **巡航** 和 **乘组** 启动下一段工作 |
| 本地运行缺少共享状态 | **航行器** 把本地运行环境接入同一个中枢 |
| "谁跑了什么？"靠口口相传 | **看板**、**信号**、diff、成员关系 |

人类继续使用 **Claude Code**、**Codex**、**Grok Build** 和其他工具。UFO 做的是 **舰队** 层。一个中枢，一张
看板，人与航行器同属一条信任边界。

---

## 功能

- **派发行动。** 创建一个 **行动**，指定 **领航员**；航行器将本地运行环境接入舰队。
- **看板。** Kanban、列表与泳道；评论、资产、标签、关联关系与 **信号**。
- **本地仍在本地。** 代码和密钥留在人类控制的主机上；不强制依赖云。
- **隔离工作树。** 每次运行都有自己的检出；准备好后再应用回源码、提交到分支，或从源码刷新。
- **自主航段。** **巡航** 在行动 **已完成** 后继续出航；可选自动提交分支，用于无人值守的再出航循环，并带停滞与失败即停防护。
- **技能。** 舰队上的可复用 `SKILL.md` 包，绑定到行动或 **乘组**；执行时写入工作树给领航员使用。
- **乘组与成员。** 舰队、角色、邮件邀请、乘组（领航员 + 人类成员）。
- **自带领航员。** Claude Code、Codex、Antigravity、Grok Build、Cursor Agent、GitHub
  Copilot、Amp Code、OpenCode、OpenClaw、Hermes、Pi、Kimi、Kiro、CodeBuddy Code（可执行文件在
  `PATH` 上）。
- **Forge。** 连接 GitHub 或 GitLab（云或自托管），绑定任务，由航行器在本机用令牌推送并处理合并请求。

---

## 快速开始（本地）

不需要云账号。先在这台机器上启动 **中枢**，再连接一台 **航行器**。

**需要：** [Docker](https://docs.docker.com/get-docker/) 和
[Rust/Cargo](https://rustup.rs)（航行器运行在 **host** 上，这样才能使用本地文件和 AI CLI）。

### 1. 启动中枢

```bash
git clone https://github.com/fengsi/ufo.git
cd ufo
scripts/dev.sh up          # Postgres + API + web (live reload)
```

- 看板：**http://localhost:3000**
- 中枢 API（rover `--hub`）：**http://localhost:8080**

### 2. 注册

打开 **http://localhost:3000** 创建账号。UFO 会创建个人 **舰队**，并带一个默认的 **Launch Bay**
**任务**。

### 3. 接入航行器

```bash
scripts/dev.sh rover enroll
```

按提示在浏览器中批准接入。之后启动：

```bash
scripts/dev.sh rover
```

> **航行器（Rover）。** 连接本地运行环境的节点；它从中枢接受行动，调用本地 AI CLI **领航员（Pilot）** 在隔离工作树里执行，并把
> 状态和 diff 回传到看板。

同一台主机上的航行器可以保存多份接入，包括接入不同中枢。启动一次航行器后，每份已保存的接入都会保持待命；按航行器配置并发单元（`units`），即可用同一套
本地 AI CLI 同时承接多个并发行动。

### 4. 把领航员放到 PATH 上

安装至少一个受支持的 CLI，并确保它在 `PATH` 上，例如 `claude`、`codex`、`grok`、`copilot` 等。航行器只会运行它能
找到的领航员。

### 5. 派发第一个行动

1. 打开一个 **任务**（舰队里的项目框架）。
2. 放入一个 **行动**（工作项）。
3. 指定一个 **领航员**。
4. 看着看板流转：queued → accepted → running → review/done；过程中会有实时更新，代码变更也会显示 diff。

这就是基本闭环。巡航、技能、乘组和 auto-commit 都建立在它之上。

---

## 截图

**看板（Board）**

<picture>
  <source
    media="(prefers-color-scheme: dark)"
    srcset=".github/assets/hub-light.png"
  >
  <source
    media="(prefers-color-scheme: light)"
    srcset=".github/assets/hub-dark.png"
  >
  <img alt="看板" src=".github/assets/hub-dark.png">
</picture>

**航行器（Rover）**

![航行器 TUI](.github/assets/rover.png)

---

## 航行器 CLI（Rover CLI，可选）

两个 `ufo rover` 命令都需要一个正在运行的中枢。当前公开 beta 的路径是先用 `scripts/dev.sh up` 启动本地中枢；之后可以
用开发脚本，也可以用发布版 CLI 连接航行器。

```bash
# macOS / Linux
curl -fsSL https://getufo.dev/install.sh | sh
# or: brew install fengsi/ufo/ufo-cli

# with the local Hub already running from scripts/dev.sh up
ufo rover enroll --hub http://localhost:8080
ufo rover start
```

要把同一台主机接入另一个中枢，再对那个中枢执行一次`ufo rover enroll`；也可以用多个带接入码的 `--config`。
`ufo rover start` 会从 `~/.ufo/rovers.json` 加载已保存的接入。

**Windows：** 从 [Releases](https://github.com/fengsi/ufo/releases)下载对应压缩包，把
`ufo.exe` 放到 `PATH` 上，然后使用同样的`enroll` / `start` 命令。详情见
[apps/rover/README.md](apps/rover/README.md)。

发布版本提供适用于 macOS、FreeBSD、Linux 和 Windows 的航行器可执行文件；常规 CI 只在 macOS、Linux 和
Windows 上运行测试。

---

## 看板核心术语

| 术语 | 含义 |
| --- | --- |
| **舰队（Fleet）** | 信任边界：人、航行器、任务与行动 |
| **中枢（Hub）** | 控制平面：API 与舰队状态 |
| **看板（Board）** | 面向舰队的 Web UI |
| **任务（Mission）** | 舰队里的项目框架，代码形如 `MSJ-123` |
| **行动（Operation）** | 看板上的一个工作项 |
| **航行器（Rover）** | 连接本地运行环境、接受行动并运行领航员的节点 |
| **领航员（Pilot）** | 航行器调用的本地 AI CLI |
| **巡航（Routine）** | 重复启动模式：出航计划或完成后继续出航 |
| **技能（Skill）** | 绑定到行动或乘组的可复用指令包 |
| **乘组（Crew）** | 领航员 + 人类成员组成的分配目标 |

```mermaid
flowchart LR
    human["人类"] --> board["看板"]
    board --> hub["中枢"]
    hub --> rover["航行器"]
    rover --> pilot["领航员"]
    rover -- 遥测 --> hub
```

---

## 组件关系

| 组件 | 作用 |
| --- | --- |
| [`apps/web`](apps/web) | 看板 |
| [`apps/api`](apps/api) | 中枢（auth、queues、OpenAPI） |
| [`apps/rover`](apps/rover) | 航行器（`ufo-cli`）：本地运行环境，运行领航员 |

```mermaid
flowchart TD
    web["看板<br/>Next.js"] <--> api["中枢<br/>Go API"]
    api <--> db["PostgreSQL<br/>舰队状态"]
    api <--> rover["航行器<br/>Rust 宿主"]
    rover --> pilot["领航员 CLI<br/>Claude / Codex / Grok"]
```

**信任说明：** 舰队里的任何成员都可以把行动派给该舰队的航行器。领航员会以启动航行器的 OS 用户身份运行。严肃舰队建议使用专用账号或主机；见
[SECURITY.md](SECURITY.md)。

---

## 配置

复制 [`.env.example`](.env.example) 到 `.env` 来覆盖默认配置。

| 变量 | 默认值 | 使用方 |
| --- | --- | --- |
| `UFO_HUB_URL` | `http://localhost:8080` | 航行器, web |
| `UFO_HUB_DATABASE_URL` | local Docker Postgres | api |
| `UFO_HUB_JWT_PRIVATE_KEY` | production 必填 | api |
| `UFO_HUB_JWT_ALLOW_EPHEMERAL` | 本地开发可设 `1` | api |
| `UFO_ROVER_FORGE_TOKEN` | 未设置 | 航行器（forge 推送/MR） |

`UFO_ROVER_FORGE_TOKEN` 是 forge 凭证在航行器主机上的默认环境变量名（GitHub PAT、GitLab token 等）。
Integrations 里可改成其他名称；在运行 `ufo rover start` 的环境中导出对应变量。中枢只保存变量名，不保存密钥。

完整列表见 [`.env.example`](.env.example) 和
[`.env.production.example`](.env.production.example)。

---

## 进阶：host-only API/web

Host 上需要 Go ≥ 1.26 和 Node ≥ 20.9；Postgres 仍由 Docker 运行：

```bash
scripts/dev.sh db
scripts/dev.sh api
scripts/dev.sh web
scripts/dev.sh rover enroll
```

贡献者流程见 [CONTRIBUTING.md](CONTRIBUTING.md)。

---

## 排障

| 现象 | 尝试 |
| --- | --- |
| Web 打不开 | `docker compose ps` · `docker compose logs -f web api postgres` |
| API 连不上 DB | `scripts/dev.sh up` 或 `db`；检查 `UFO_HUB_DATABASE_URL` |
| 登录后浏览器请求失败 | 将 `UFO_HUB_ALLOWED_ORIGINS` 设为 web origin；secure cookies 只用于 HTTPS |
| 航行器无法接入 | `--hub` 必须是 **API** origin；在浏览器里批准 |
| 在线但空闲 | 是否指定领航员？CLI 在 `PATH` 上？Tags 匹配吗？ |
| 清空本地 Docker 数据 | `scripts/dev.sh down -v && scripts/dev.sh up`（破坏性） |

---

## 文档

| 文档 | 内容 |
| --- | --- |
| [Rover CLI](apps/rover/README.md) | 安装、接入、TUI、headless |
| [OpenAPI](apps/api/internal/spec/openapi.yaml) | HTTP 契约 |
| [Contributing](CONTRIBUTING.md) | PR、monorepo、beta DB 注意事项 |
| [Security](SECURITY.md) | 舰队信任边界与航行器风险 |
| [Changelog](CHANGELOG.md) | 发布记录 |

---

## 参与贡献

欢迎提交 issue 和 PR；请先阅读[CONTRIBUTING.md](CONTRIBUTING.md)。

schema 变更请使用 `apps/api/internal/migrate/migrations/` 下的 SQL migration，Hub 启动时自
动应用。详见[CONTRIBUTING.md](CONTRIBUTING.md)。若发布说明要求 schema reset，升级前请备份或清空本地 DB。

---

## 许可证

本项目使用 [BSD 3-Clause](LICENSE) 许可证。第三方许可声明见
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
