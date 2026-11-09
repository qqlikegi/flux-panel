# flux-panel转发面板 哆啦A梦转发面板

# 赞助商
<p align="center">
  <a href="https://vps.town" style="margin: 0 20px; text-align:center;">
    <img src="./doc/vpstown.png" width="300">
  </a>

  <a href="https://whmcs.as211392.com" style="margin: 0 20px; text-align:center;">
    <img src="./doc/as211392.png" width="300">
  </a>
</p>


本项目基于 [go-gost/gost](https://github.com/go-gost/gost) 和 [go-gost/x](https://github.com/go-gost/x) 两个开源库，实现了转发面板。
---
## 特性

- 支持按 **隧道账号级别** 管理流量转发数量，可用于用户/隧道配额控制
- 支持 **TCP** 和 **UDP** 协议的转发
- 支持两种转发模式：**端口转发** 与 **隧道转发**
- 可针对 **指定用户的指定隧道进行限速** 设置
- 支持配置 **单向或双向流量计费方式**，灵活适配不同计费模型
- 提供灵活的转发策略配置，适用于多种网络场景


## 部署流程
---
本分支基于 2.0.7 开发版二次开发，**不再使用 Docker，也不需要 Java**：

- **面板**：Go 编写的单个二进制文件，前端页面已内置，数据库为内置 SQLite，网页 / 接口 / 节点通信共用一个端口，由 systemd 管理。
- **节点端**：单个二进制文件（基于 gost），由 systemd 管理。
- 支持 Linux **amd64 / arm64**，二进制从本仓库 [Releases](https://github.com/qqlikegi/flux-panel/releases) 下载。

### 安装面板
```bash
curl -L https://raw.githubusercontent.com/qqlikegi/flux-panel/main/panel_install.sh -o panel_install.sh && chmod +x panel_install.sh && ./panel_install.sh
```
脚本菜单提供：安装 / 更新（失败自动回滚）/ 卸载 / 查看状态 / 重置管理员密码。也可以直接带参数运行：
```bash
./panel_install.sh install      # 安装（PANEL_PORT=6365 可跳过端口询问）
./panel_install.sh update       # 更新到最新 Release
./panel_install.sh status       # 查看状态
./panel_install.sh reset-admin  # 忘记密码时重置，之后打开面板重新设置管理员账号
./panel_install.sh uninstall    # 卸载（可选择保留数据库）
```

- 安装目录：`/opt/flux-panel`（程序 `flux-panel`，数据库 `data/gost.db`，日志 `logs/`）
- 配置文件：`/opt/flux-panel/flux-panel.env`（端口、JWT 密钥等，修改后 `systemctl restart flux-panel`）
- 查看日志：`journalctl -u flux-panel -f`
- 从旧版 Docker 部署升级：安装时会自动检测旧容器和旧数据库，按提示迁移即可，数据库结构完全兼容。

### 安装节点端
1. 登录面板，在「系统配置」里把面板地址设置为 `面板公网IP:端口`；
2. 在「节点管理」新增节点，点击「安装」复制安装命令，到节点服务器上执行即可。

### 手动运行（不用脚本）
```bash
chmod +x flux-panel-amd64
SERVER_PORT=6365 DB_PATH=./data/gost.db ./flux-panel-amd64
```
可用环境变量：`SERVER_PORT`（默认 6365）、`DB_PATH`（默认 `data/gost.db`）、`LOG_DIR`（默认 `logs`）、`JWT_SECRET`（不填则自动生成并保存在数据库同目录的 `jwt.secret`）。

### 从源码构建
```bash
cd vite-frontend && npm install --legacy-peer-deps && npm run build && cd ..
rm -rf panel/web/dist && cp -r vite-frontend/dist panel/web/dist
cd panel && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o flux-panel-amd64 .
```

#### 管理员账号

全新安装后第一次打开面板，会要求先设置管理员用户名和密码（复杂度不限），确定后直接进入面板。
从旧版本升级的面板如果还在用默认账号 `admin_user / admin_user`，打开时同样会要求重新设置。

#### 二开新增功能

- **节点换机自动恢复**：节点换了新机器，点「安装」重装节点端后，隧道、转发、限速自动恢复；改节点 IP / 端口范围后，相关隧道和转发自动迁移端口并实时生效。
- **隧道可编辑**：已有隧道的入口 / 中间跳 / 出口节点、协议、策略、类型都可以修改，保存后实时下发，隧道下所有用户的转发自动同步。
- **数据总览**：管理员首页显示在线节点、总使用流量、24 小时流量，以及每个用户的今日 / 本月流量。
- **云存储备份**：「系统配置」里填写 Cloudflare R2（或任意 S3 兼容存储）的终结点、存储桶和访问密钥，可按每天固定时间或每隔几小时自动备份数据库，最多保留 N 份（默认 3 份），自动删除最旧的。
- **用户到期时间**：在用户管理里修改用户的到期时间，会同步到该用户的全部隧道；在「用户隧道」里单独编辑则只改那一个隧道。


## 免责声明

本项目仅供个人学习与研究使用，基于开源项目进行二次开发。  

使用本项目所带来的任何风险均由使用者自行承担，包括但不限于：  

- 配置不当或使用错误导致的服务异常或不可用；  
- 使用本项目引发的网络攻击、封禁、滥用等行为；  
- 服务器因使用本项目被入侵、渗透、滥用导致的数据泄露、资源消耗或损失；  
- 因违反当地法律法规所产生的任何法律责任。  

本项目为开源的流量转发工具，仅限合法、合规用途。  
使用者必须确保其使用行为符合所在国家或地区的法律法规。  

**作者不对因使用本项目导致的任何法律责任、经济损失或其他后果承担责任。**  
**禁止将本项目用于任何违法或未经授权的行为，包括但不限于网络攻击、数据窃取、非法访问等。**  

如不同意上述条款，请立即停止使用本项目。  

作者对因使用本项目所造成的任何直接或间接损失概不负责，亦不提供任何形式的担保、承诺或技术支持。  


请务必在合法、合规、安全的前提下使用本项目。  

---
## ⭐ 喝杯咖啡！（USDT）

| 网络       | 地址                                                                 |
|------------|----------------------------------------------------------------------|
| BNB(BEP20) | `0x755492c03728851bbf855daa28a1e089f9aca4d1`                          |
| TRC20      | `TYh2L3xxXpuJhAcBWnt3yiiADiCSJLgUm7`                                  |
| Aptos      | `0xf2f9fb14749457748506a8281628d556e8540d1eb586d202cd8b02b99d369ef8`  |

[![Star History Chart](https://api.star-history.com/svg?repos=bqlpfy/flux-panel&type=Date)](https://www.star-history.com/#bqlpfy/flux-panel&Date)