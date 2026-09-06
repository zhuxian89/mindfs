---
doc_type: audit-runtime-verification
audit: 2026-09-06-relay-upstream-compatibility
created: 2026-09-06
status: current
scope: 用户授权后的本机运行进程、配置与原发现触发条件核验
---

# 本机运行态核验

## 结论修正

**用户当前正常使用与前述条件性发现并不矛盾。本机没有启用新版内建自启动，E2EE 也未开启；本机浏览器存在直连节点的连接，当前本地 JS/CSS 资源均可读取。没有发现前述问题正在本机触发的证据。**

上一轮先审源码、未先核对运行态，把潜在边界问题突出为上线注意事项，不能据此判断用户当前服务有故障。保留源码发现，但本机适用性以本记录为准。

## 实际运行程序

| 项目 | 安全核验结果 |
|---|---|
| 进程 PID | 82054 |
| 进程父 PID | 1 |
| 可执行文件 | `/Users/hongweizhang/.mindfs/mindfs` |
| 安装版自报版本 | `mindfs version: v0.5.0`（只执行 `-version`，未启动新服务） |
| 实际启动参数 | `-foreground -addr 127.0.0.1:7331` |
| 启动时间 | 2026-09-05 10:33:46，本次检查前后相同 |
| 监听地址 | `127.0.0.1:7331` |
| 二进制构建元数据 | `vcs.revision=f6e2423b4e8dbd071da2dc56556f2aa4a0a2a965`，`vcs.modified=true` |

运行安装产物与今天快进更新的仓库不是同一个对象。Git pull 没有替换或重启这个进程。由于构建标记 modified=true，不能把运行二进制等同于该 commit 的无修改源码，也不能简单断言“它太旧，尚未包含所有新功能”。

实际服务的 JS bundle 包含 `session-lists`、`multi-root`、`event_cursor` 等标记，说明至少相关前端能力已经在当前安装产物中。

## 本机 HTTP 与连接证据

只向 `http://127.0.0.1:7331` 发起 GET，禁用 HTTP 代理；未查询远端 Relay、账户列表或生产资产目录。只输出 JSON 白名单状态字段，不输出节点 ID、密钥或 token。

- `GET /health` → **200，正文 `ok`**。
- `GET /api/relay/status` → **200**，安全字段：

  ```json
  {
    "relay_bound": true,
    "no_relayer": false,
    "token_station_bound": true,
    "e2ee_required": false
  }
  ```

- 状态中的 `relay_base_url` 指向用户自研 Relay，`node_url` 使用 `/n/{nodeId}/` 形式；敏感节点标识未记录。
- 本地首页引用：
  - `./assets/index-Cfwl2jiA.js` → **200**，2,331,222 字节。
  - `./assets/index-Cdk4SFKm.css` → **200**，101,568 字节。
- `lsof` 观察到 Chrome 进程与 `127.0.0.1:7331` 的两个已建立连接。它证明本机存在浏览器直连链路，不证明用户没有其他远程页面，也不替代读取浏览器当前 URL。
- 对同一本地首页额外带 `X-MindFS-Relayed: 1` 做无副作用 GET，实际引用变为 `/mindfs-assets/`，确认安装版具备 release 资源重写行为；没有由此推断远端资源目录是否完整。

## 本机持久配置核验

只使用 JSON/plist 白名单解析，或定向检查变量名；没有读取完整凭据文件或输出完整配置内容。

| 路径 / 配置 | 结果 |
|---|---|
| `~/Library/LaunchAgents/com.a9gent.mindfs.plist` | 不存在 |
| `~/Library/Application Support/mindfs/autostart-environment.json` | 不存在 |
| `~/Library/Application Support/mindfs/agents.json` | 不存在 |
| `~/.mindfs/agents.json` | 存在，`relayBaseURL` 仍为官方地址 `https://relay.a9gent.com` |
| 当前检查 shell 的 `MINDFS_RELAY_BASE_URL` | 已设置为自研 Relay，与运行状态一致 |
| 当前检查 shell 的 `MINDFS_INTERNAL_RESTART` | `1` |
| 当前检查 shell 的 `MINDFS_AGENTS_CONFIG` | 未设置 |
| 实际进程 `--config` / `--agent-config` | 未提供 |

macOS 默认用户配置目录由 `os.UserConfigDir()` 决定，位于 `~/Library/Application Support/mindfs/`，不是 Linux 的 `~/.config/mindfs/`。

默认 `.zprofile`、`.zshrc` 存在，但定向检查未发现 `MINDFS_RELAY_BASE_URL`；`.zshenv`、`.zlogin` 不存在。没有扩查被 source 的脚本或历史启动环境，因此自定义 Relay 环境变量的最初设置来源未确认。

当前安装配置仍指官方地址，而运行状态指向自研地址，与 Relay Manager 环境变量优先规则一致。此记录没有直接转储运行进程的完整环境，也没有推断其他 hosted API 的实际访问目的地。

## 原发现对本机的适用性

| 原项 | 本机实际情况 | 本机结论 |
|---|---|---|
| finding-01：新版内建自启动丢 Relay env | 进程无 `--internal-autostart`；LaunchAgent 与 snapshot 均不存在；当前地址已正确生效 | **触发条件不成立，不是当前故障**。将来改变启动方式时再关注。 |
| finding-02：同 origin 多节点串缓存 | 当前 bundle 有列表缓存代码；未读取浏览器缓存/历史，未验证 `/n/A/`→`/n/B/` 场景 | **未证明触发**。正常使用一个节点或直连本地不等于经过该场景；不能宣称用户只有一个节点。 |
| finding-03：编码路径 E2EE proof 失配 | 运行接口明确 `e2ee_required=false` | **E2EE 校验前提不成立**，所以该签名错误不影响当前未加密使用路径。这个事实仅解释现象，不把关闭 E2EE 推荐为修复。 |
| 远端 `/mindfs-assets/` 缺新 hash | 本地 JS/CSS 均 200；浏览器有本机直连；未查远端资源 | **本机资源正常，远端缺失只是未证实的部署条件**，不能说用户已经白屏或必须立刻同步。 |
| 仓库 typecheck 缺依赖 | 运行的是独立安装版和已构建 bundle，不需要当前仓库 node_modules 来服务页面 | 解释了仓库验证未全绿与当前应用正常并存；不代表已修复仓库依赖缺失。 |

## 边界与操作记录

未读取浏览器历史/IndexedDB、未切换节点、未检查远端资产目录、未测试线上 E2EE、未转储完整进程环境。未执行绑定/解绑、更新、重启、自启动注册或配置修改。检查结束后 PID 与进程启动时间未变。

**当前无需因为上一轮静态发现而中断已正常工作的服务或重写 Relay。若未来改用新版内建自启动、开启 E2EE、同域名切换节点或更新云端 bundle，再按对应条件做验证。**
