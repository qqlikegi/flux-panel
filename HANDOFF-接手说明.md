# ForwardZ 接手说明

> 这份文档给 WorkBuddy 用。Claude 那边不再参与这个项目。

## 归属与边界

| | |
|---|---|
| 我的工作副本 | `C:\Users\Work\WorkBuddy\2026-11-11-10-46-16\workspaces\flux-panel`（可读可写） |
| Claude 原件 | `C:\Users\Work\AppData\Local\Claude-Data\claude_pzs8sxrjxfjjc\scratch-workspaces\f719cce0-...\0256a32e-...\scratch-2026-11-09-fd2f36\flux-panel`（**只读，一个字节都不许改**） |
| 分叉锚点 | 分支 `handoff/from-claude-20261111`：`800493d` = Claude 移交时那 6 个未提交改动；`3530a69` = 我接手的第一个提交（修构建） |
| 远端 | `origin` = `qqlikegi/forwardz`（自己的 fork）；`upstream` = `bqlpfy/flux-panel`（**不要推**） |

副本是完整复制（含 `.git`、`node_modules`），和原件没有硬链接、没有 junction，物理独立。

## 项目是什么

`flux-panel` 2.07 的二次开发，已经改名成自己的产品 **ForwardZ**：

- 转发内核从 gost 换名改造成自家的 **Forward**（程序名、服务名、指纹、提示文字全改，避免和别的 gost 撞）
- 部署从 Docker 全改成**二进制**（面板 / 节点 agent / 前端静态资源）
- 仓库：<https://github.com/qqlikegi/forwardz>

```
flux-panel/
├─ panel/            面板端（Go，单二进制，内嵌前端 dist，默认端口 6365）
├─ agent/            节点端（Go，装在节点机器上，连面板）
├─ vite-frontend/    前端（React + Vite + HeroUI + Tailwind + recharts）
├─ install.sh        节点一键安装
├─ panel_install.sh  面板一键安装（含端口安全检查）
├─ zf.sh             节点快捷菜单
└─ doc/              文档
```

前端 39 个 ts/tsx 文件。数据库 `data/forwardz.db`（SQLite）。

## 已经做完的

按提交倒序，都是可用的状态：

1. `b5fdf90` 前端 iOS 液态玻璃风格：浮空玻璃侧栏与底栏、玻璃卡片、胶囊按钮；数据总览的「用户流量」从表格改成每人一张卡片，支持按用户筛选
2. `a34f63b` 前端 UI 重构：中性灰阶主题、玻璃质感分组侧栏、胶囊控件、大圆角卡片、新登录页与 Logo
3. `cee8364` 安装脚本拒绝浏览器禁用端口，输错要重输
4. 节点端 swagger/proto 里的包名改成 ForwardZ
5. 去掉全部 go-gost 外部依赖，grpc 服务改名
6. 品牌全面升级 ForwardZ（面板、节点、脚本）
7. 节点指纹机制：重装按指纹/密钥识别归属，同面板覆盖更新、异面板共存安装（一台机器可以对接多个面板）
8. 隧道可编辑、实时生效并同步给所有使用者
9. 节点更换后隧道自动恢复
10. 系统配置里加了 CF R2 存储桶备份

## 正在进行：iOS 27 液态玻璃 UI 重构

Claude 是被**周用量限额**（11/11 18:00 恢复）中断在这里的，不是做完了。

已改完并亲眼确认过的：隧道管理、数据总览（含用户流量卡片）、手机端隧道页 + 浮空底栏。
类型检查和构建通过。

> ⚠️ **移交时其实是编译不过的**。`npm run build` 报 `user.tsx(114,22) TS6133: 'setPagination' is declared but its value is never read`
> ——改卡片式列表时把分页控件删了，但 state 没删干净。Claude 在最后一次编辑之后就被限额打断，没再跑构建。
> 已在 `3530a69` 修掉（移除未使用的 setter），现在 `tsc && vite build` 干净通过。

**顺手发现的隐患**：用户列表的 `size` 固定 10、`setPagination` 已无，页面上也没有分页控件 ——
**用户超过 10 个时，第 11 个之后在列表里看不到了**（接口还是按 current/size 分页取的）。这个需要你定方向：
加分页控件，还是一次性拉全（改成无限滚动 / 搜索）。

**没验证过**（改了同一套样式规则，但截图一直超时，没亲眼看）：节点监控、用户管理、转发卡片、深色模式。
用户下拉筛选只确认了「显示出来了」，没实际勾选测试。

**明确没做完的两条**：

| 尾巴 | 位置 |
|---|---|
| 手机底栏图标还是旧的实心图标，要换成和侧栏一致的线性（描边）图标 | `vite-frontend/src/layouts/h5.tsx` 第 30–68 行，图标是内联 SVG |
| 流量图曲线还是紫色 `#8b5cf6`，要改成 iOS 系统蓝 | `vite-frontend/src/pages/admin-overview.tsx` 第 228 行 `stroke="#8b5cf6"` |

## 怎么跑

```bash
# 前端开发
cd vite-frontend && npm install --legacy-peer-deps && npm run dev

# 前端构建（产物拷进 panel 一起打包）
cd vite-frontend && npm run build
rm -rf ../panel/web/dist && cp -r dist ../panel/web/dist

# 面板 / 节点（Linux，交叉编译）
cd panel && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o forwardz-panel-amd64 .
cd ../agent && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o forwardz-agent-amd64 .
```

本机现状：node v22.22.2 / npm 10.9.7 可用；**Go 没装**，后端要构建得先装 Go（或用 Linux 机器）。

环境变量：`SERVER_PORT`（默认 6365）、`DB_PATH`（默认 `data/forwardz.db`）、`LOG_DIR`、`JWT_SECRET`。

## 会话存档

Claude 那边关于这个项目的完整对话已转成 Markdown，放在
`..\..\claude_export\`（`01_会话分类新建.handoff.md` 是建仓与首次通读，`02_重构哆啦A梦.handoff.md` 是全部开发过程）。
要查某个决定当时是怎么定的，去那里翻。
