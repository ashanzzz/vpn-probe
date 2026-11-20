# vpn-probe

自包含的代理链接检测、送中研判、双模 MCP 与 SubStore 4分组自愈系统。

---

## ✨ 核心特性

- 📦 **完全自包含测试沙箱**：内置官方 `sing-box` 进程隔离启动器，无需宿主机额外配置代理内核，动态分配端口按需测试，测完即销毁。
- 🤖 **MCP 双模式开放（本地 + 远程 AI）**：
  - **stdio 模式**：通过管道直接挂载至本地 Antigravity CLI / Claude Desktop 等 AI 客户端。
  - **SSE / HTTP 模式**：提供 `/mcp/sse` 与 `/mcp/messages` 接口，供远程 AI Agent / 自动化工作流随时调用。
- 🩺 **双重核心判定**：
  - **是否在线**：TCP 握手与 `gstatic/generate_204` 真实 RTT 延迟采样。
  - **是否送中**：Google NCR（禁止跳转重定向判定）与 Cloudflare trace `loc=CN` 双路研判。
- 🌐 **扩展分流展示（仅供展示）**：
  - 自动探测 ChatGPT / Claude / YouTube / 百度直连连通性与响应耗时，不干扰分类逻辑。
- 🔄 **SubStore 4大精准分组同步**：
  - 🟢 **在线自有服务**
  - 🔴 **离线自有服务**
  - 🇨🇳 **在线自有服务送中**
  - 🌐 **在线自有服务不送中**
  - *(支持自动推送至远程 SubStore REST API，或由 SubStore 反向订阅 `/api/sub/:group`)*

---

## 🚀 快速使用

### 1. 命令行快速单测一条链接

```bash
# 快速单测一条链接
vpn-probe probe "trojan://password@example.com:443?sni=example.com#MyTrojan"

# 外部购买节点测试（非自有服务）
vpn-probe probe --self=false "vless://uuid@1.2.3.4:443?security=reality&sni=test.com&pbk=key#JP-Node"
```

### 2. 启动服务（REST API + 远程 MCP SSE）

```bash
vpn-probe server --config config.yaml
```
服务将在 `0.0.0.0:8080` 启动：
- REST API: `http://localhost:8080/api/probe`
- 远程 AI MCP SSE 地址: `http://localhost:8080/mcp/sse`
- 动态订阅拉取地址: `http://localhost:8080/api/sub/self-online-clean` 等

### 3. 启动本地 stdio MCP

在 AI 客户端配置中直接引用：
```json
{
  "mcpServers": {
    "vpn-probe": {
      "command": "/path/to/vpn-probe",
      "args": ["mcp", "--config", "/path/to/config.yaml"]
    }
  }
}
```

---

## 📡 REST API 说明

### 批量检测接口

- **路径**: `POST /api/probe`
- **请求体**:
```json
{
  "links": [
    "trojan://mypassword@example.com:443?sni=example.com#SG-01",
    "vless://uuid@host:port?security=reality&sni=host&pbk=key#HK-01"
  ],
  "is_self": true,
  "sync_substore": true,
  "check_detailed_split": true,
  "timeout_seconds": 8
}
```
- **响应体**:
```json
{
  "total": 2,
  "online": 2,
  "offline": 0,
  "china_exit": 0,
  "clean_exit": 2,
  "group_counts": {
    "在线自有服务不送中": 2
  },
  "substore_synced": true,
  "results": [
    {
      "id": "...",
      "name": "SG-01",
      "protocol": "trojan",
      "online": true,
      "latency_ms": 138,
      "china_exit": false,
      "exit_country": "SG",
      "category": "在线自有服务不送中",
      "split": {
        "chatgpt": "ok (189ms)",
        "claude": "ok (204ms)",
        "youtube": "ok (142ms)",
        "baidu": "fail (unreachable)"
      }
    }
  ]
}
```

---

## 🐳 Docker 部署（自包含 sing-box）

```bash
docker compose up -d
```
镜像构建时自动提取官方 `sing-box` 静态二进制，无需宿主机环境准备。
