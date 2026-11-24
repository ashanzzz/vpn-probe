# 系统全景逻辑与动态化编排架构规格说明书
(System Architecture, Pipeline Logic & Dynamic Orchestration Specification)

> **目标受众**：高级系统架构师、后端 (Go) 工程师、前端 (React/TS) 工程师与全栈 AI。  
> **核心目标**：梳理现有 Sub-Store 订阅聚合、Nginx 容灾缓存、节点分流中继（线路机与家宽落地）及 `vpn-probe` 控制面的全景逻辑；并确立**节点类型动态化**（NAT 家宽、VPS 家宽、NAT 线路、VPS 线路等）与**策略组标签化编排（Tag-Based Policy Grouping）**的全新工程规范。

---

## 目录
1. [系统总体架构与双平面拓扑](#1-系统总体架构与双平面拓扑)
2. [Sub-Store (sunstore) 核心管道与流水线逻辑](#2-sub-store-sunstore-核心管道与流水线逻辑)
3. [节点分类与多阶策略组（节点组）逻辑](#3-节点分类与多阶策略组节点组逻辑)
4. [外部专线与自建节点架构逻辑](#4-外部专线与自建节点架构逻辑)
5. [自建线路机、自建家宽与链式中继 (dialer-proxy) 机制](#5-自建线路机自建家宽与链式中继-dialer-proxy-机制)
6. [全面动态化设计方案：面向高级 AI 的重构规范](#6-全面动态化设计方案面向高级-ai-的重构规范)
   - 6.1 多维节点分类模型 (Node Type Taxonomy)
   - 6.2 标签系统与元数据规范 (Tagging & Metadata Engine)
   - 6.3 声明式动态策略组编排 (Declarative Policy Group Compiler)
   - 6.4 后端 (Go) 架构重构蓝图与接口定义
   - 6.5 前端 (React + TS) 可视化交互与管理蓝图
7. [现存系统缺陷与高级 AI 修复指导 (Known Issues & Roadmap)](#7-现存系统缺陷与高级-ai-修复指导)

---

## 1. 系统总体架构与双平面拓扑

整个代理接入与管理体系由 **数据转发与发布平面** 和 **主动探针与自治控制平面** 构成：

```
                      +-----------------------------------------+
                      |               用户客户端                 |
                      |   (Clash Verge / OpenClash / Meta内核)  |
                      +-------------------+---------------------+
                                          |
                                          | HTTP 拉取订阅 (带 Meta UA)
                                          v
+------------------------------------------------------------------------------------+
| 1. 东京发布中枢 (156.231.113.91 - free.1992004.xyz)                                 |
|                                                                                    |
|   +----------------------------------------------------------------------------+   |
|   | Nginx 高可用反代与本地容灾缓存 (/opt/1panel/www/sites/free.1992004.xyz/index)   |   |
|   |   - /share/file/Q -> 优先命中 sub_cache/Q.yaml (静态容灾毫秒响应)             |   |
|   |   - 未命中或动态回源 -> 反代本地 127.0.0.1:3001 (Sub-Store 容器)              |   |
|   |   - 定时任务 crontab: 每 10 分钟自动拉取最新 Sub-Store 输出落盘至静态缓存      |   |
|   +-------------------------------------+--------------------------------------+   |
|                                         |                                          |
|   +-------------------------------------v--------------------------------------+   |
|   | Sub-Store 容器 (Node.js 后端 / 端口 3001)                                    |   |
|   |   - 数据库存储: /opt/1panel/apps/sub-store/data/root.json (sub-store.json)    |   |
|   |   - 流水线引擎: Script Operators (JS 管道处理器)                              |   |
|   |       [1] ashan_patch.js: MLKEM 兼容注入 + 链式 dialer-proxy 绑定 + 命名精简    |   |
|   |       [2] convert_openclash_v2.js: 构造 ClashMeta 策略组全集 + 自动测速         |   |
|   |       [3] classify_songzhong.js: Google NCR 研判 + 送中隔离 + 自建分组置底      |   |
|   +-------------------------------------+--------------------------------------+   |
+-----------------------------------------|------------------------------------------+
                                          |
                                          v
+------------------------------------------------------------------------------------+
| 2. 自治中枢与主动探测平面 (vpn-probe - Go 语言核心)                                |
|                                                                                    |
|   +----------------------------------------------------------------------------+   |
|   | 内置 sing-box 进程隔离沙箱: 动态分配端口单测 TCP / generate_204 延迟        |   |
|   | 双路送中研判引擎: Google NCR 302 重定向探测 + Cloudflare trace loc=CN 检查   |   |
|   | 自动自愈器 (Auto Healer): 监听故障、重启远程服务、更新 Sub-Store 订阅源      |   |
|   | 双模 MCP (Model Context Protocol): 本地 stdio + 远程 HTTP/SSE，供 AI 审计   |   |
|   | 现代化 Web 前端 (web/): 节点健康监控、拓扑分析、策略编排控制台              |   |
|   +----------------------------------------------------------------------------+   |
+------------------------------------------------------------------------------------+
```

---

## 2. Sub-Store (sunstore) 核心管道与流水线逻辑

Sub-Store 是整个订阅生成的聚合器与编译器，其核心模型由三层组成：

### 2.1 数据模型
1. **Subs (子订阅源)**：
   - `蜂窝云`：外部采购的商业机场专线订阅，提供数十至上百条商业 BGP/IPLC 线路。
   - `ashan-transit`：自建骨干前置 VPS（东京 IIJ、东京 SoftBank、美西洛杉矶 4837 等）。
   - `ashan-us-resi`：美国家宽落地机（Frontier、Verizon、Pavlov 等落地节点）。
   - `ashan-jp-resi`：日本家宽落地机（SoftBank、BIGLOBE、GLBB、KDDI 等落地节点）。
   - `ashan-other`：其他地区家宽与落地（台湾 HiNet、韩国 KT、新加坡 StarHub 等）。
2. **Collections (组合汇聚池)**：
   - `Q-汇总`：汇聚 `['ashan-transit', 'ashan-us-resi', 'ashan-jp-resi', 'ashan-other', '蜂窝云']`。
   - `ashan-纯净`：仅汇聚自建节点，排除任何外部第三方机场。
3. **Files (发布产出器)**：
   - `Q`：面向主客户端的完整生产订阅文件。
   - `ashan`：面向极客纯净环境的自建专用订阅。

### 2.2 JS 处理器流水线执行顺序与逻辑
Sub-Store 在向客户端输出 YAML 前，通过挂载的 JavaScript 脚本依次串行处理配置：

#### 阶段 1：`ashan_patch.js` (协议兼容、链式路由绑定与规范精简)
- **MLKEM 兼容性修复**：遍历所有 `vless` 且包含 `reality-opts` 的节点，强制注入 `client-fingerprint: chrome`，解决现代客户端握手异常。
- **前置 VPS 动态识别**：扫描所有非链式节点，根据正则提取当前可用的骨干前置机全名：
  - IIJ: `JP-Tokyo-IIJ-Reality`
  - LAX: `US-LAX-ACCK-4837-Reality`
  - SB: `JP-Tokyo-Softbank-Reality-Vision`
- **链式中继命名规范精简与 `dialer-proxy` 绑定**：
  - 老格式（如 `链式|JP-Tokyo-Softbank-Reality-Vision ➔ LZ-TW-HiNet-家宽`）被重构为规范的三段式精简命名：
    `[前置机代号]-[落地地区及类型]-[运营商]`（如 `SB-台宽-HiNet`、`IIJ-美宽-Frontier`、`LAX-美宽-Verizon`）。
  - **核心底层绑定**：自动在该节点 YAML 属性中注入：
    ```yaml
    dialer-proxy: "JP-Tokyo-IIJ-Reality" # 指定上层代理网关
    ```
- **全局去重与策略组引用更新**：将 `proxy-groups` 中对老节点名的引用同步替换为精简后的新名字，并对 `proxies` 数组执行 Set 去重。

#### 阶段 2：`convert_openclash_v2.js` (策略组构建器)
- 依据节点国家与协议，自动生成 Clash 策略组骨架：
  - 核心入口组：`选择代理`、`自动选择`、`手动选择`。
  - 专有业务组：`AI服务`、`Netflix`、`YouTube`、`谷歌服务`、`巴哈姆特`、`国外媒体`。
  - 地区/国家策略组：`香港节点`、`日本节点`、`美国节点`、`台湾节点`、`新加坡节点`、`韩国节点` 等。
  - 自动测速组（URL-Test）：按地区分配健康检查 URL (`gstatic/generate_204`)、容差 `tolerance: 50`。

#### 阶段 3：`classify_songzhong.js` (送中分类与自建置底重组)
- **送中检测与归类**：识别被 Google 判定送中的脏 IP 节点，移入 `自建-送中分组`。
- **策略组置底重排（用户明令铁律）**：
  - 严禁出现未经授权的流媒体/游戏等冗余自定义组。
  - 顶部严格保持用户控制组：`选择代理` 默认第一项为 `自动选择`。
  - 内部自建策略组全部置于策略组列表的最末尾（置底）：
    - `自建-美国家宽`
    - `自建-日本家宽`
    - `自建-台湾家宽`
    - `自建-家宽分组`
    - `自建-线路机`
    - `自建-送中`

---

## 3. 节点分类与多阶策略组（节点组）逻辑

在客户端（ClashMeta）中，流量经由多阶策略组进行决策路由：

```
                           [用户流量发起]
                                 |
                                 v
                        [策略组: 选择代理]
                                 |
        +------------------------+------------------------+
        |                        |                        |
        v                        v                        v
   [自动选择]               [手动选择]                [直连 DIRECT]
 (URL-Test 最优延迟)    (人工选择指定节点/分组)
        |                        |
        +------------------------+
                                 |
                +----------------+----------------+
                |                                 |
                v                                 v
         [外部商业专线]                     [自建专属专区]
       (蜂窝云 82 个节点)                  (43 个自建节点)
        /     |     \                      /      |      \
     [HK]   [JP]   [US] ...        [骨干线路]  [直连家宽]  [链式中继]
                                     (IIJ/LAX)  (原生ISP)  (dialer-proxy)
```

### 策略组的严格排序与层级：
1. **第 1 阶：全局流量统领组**
   - `选择代理`：默认首选 `自动选择`，备选为各地区组、自建组以及各独立节点。
   - `自动选择`：包含所有可用的外部专线节点与自建线路节点，自动测速（间隔 300s，容差 50ms）。
   - `手动选择`：平铺所有可用节点，按地区与类型自然排序。
2. **第 2 阶：业务规则分流组**
   - `AI服务`（OpenAI / Claude / Copilot）：
     - 路由优先级：`自建-美国家宽分组` -> `自建-家宽分组` -> `自建-线路分组` -> `自动选择` -> `选择代理`。
     - 业务逻辑：AI 模型风控严厉封锁数据中心机房 IP，优先强制使用美国原生家庭宽带出口。
   - `Netflix` / `国外媒体`：优先调度低延迟家宽与解锁专线。
3. **第 3 阶：底层自建运维专区（强制置底）**
   - 包含 `自建-线路分组`（3 个骨干前置 VPS）、`自建-美国家宽分组`、`自建-日本家宽分组`、`自建-家宽分组`、`自建-送中分组`。
   - 逻辑目的：将运维专区与日常使用专区解耦，避免普通分流误选未经中继的动态裸家宽。

---

## 4. 外部专线与自建节点架构逻辑

整个体系采取“**外部商业专线作底座，自建骨干家宽做核心突破**”的混合架构：

| 维度 | 外部专线节点 (以蜂窝云为例) | 自建骨干线路 (VPS Transit) | 自建家庭宽带 (Residential Landings) |
| :--- | :--- | :--- | :--- |
| **主要定位** | 大吞吐量下载、日常网页浏览、备用逃生通道 | 抗封锁前置中转、流量入口桥接、低延迟加速 | 业务解锁核心（ChatGPT/Claude/流媒体/银行风控） |
| **IP 属性** | 商业机房 IP / BGP Anycast | 商业数据中心 VPS IP (IIJ / 4837 / SoftBank) | 原生 ISP 住宅家庭宽带 IP (Frontier/Verizon/HiNet) |
| **抗封锁能力**| 极强（商业 IPLC/IEPL 内网专线，不经过 GFW） | 强（VLESS Reality + MLKEM 握手伪装） | 弱（原生单 IP 极易被 GFW 探测或端口封锁） |
| **网络稳定性**| 稳定（SLA 99.9%） | 稳定（顶级骨干机房） | 波动（家庭宽带受当地 ISP 调度与动态重拨影响） |
| **系统处理逻辑**| 仅做格式清洗与健康探测，直接纳入地区选择池 | 作为流量中继网关，负责承载 `dialer-proxy` 入口 | 绝不建议国内直接裸连，必须通过前置 VPS 进行中继链式代理 |

---

## 5. 自建线路机、自建家宽与链式中继 (dialer-proxy) 机制

### 5.1 痛点与架构成因
自建家庭宽带（美宽 Frontier/Verizon、日宽 SoftBank/KDDI 等）具备无可替代的**原生住宅 IP 纯净度**，但存在两大致命弱点：
1. **抗探测能力极弱**：直接暴露在公网与 GFW 之间，大流量翻墙或特征探测会导致家庭公网 IP 或特定端口直接被阻断；
2. **跨国延迟抖动大**：国内直连海外家庭宽带国际出口路由较差（无直连优化）。

### 5.2 链式代理 (Chained Relay) 核心原语
为彻底解决此问题，系统采用了 Clash 体系的 `dialer-proxy` 机制，构建**两跳式链式代理**：

```
[国内客户端]
    |
    | (1) VLESS Reality 安全隧道 (伪装 Chrome 握手，抗 GFW 审查)
    v
[自建前置线路机: US-LAX-4837 / JP-Tokyo-IIJ / JP-Tokyo-SoftBank]
    |
    | (2) 国际骨干内网或优质公网直连 (前置机作为 Dialer 发起第二跳)
    v
[自建家庭宽带落地机: US-Frontier / US-Verizon / JP-SoftBank / TW-HiNet]
    |
    | (3) 原生家庭宽带出口访问目标网站
    v
[目标互联网: chatgpt.com / claude.ai / netflix.com]
```

### 5.3 节点命名精简规范
为了消除原有冗长节点名（如 `链式|JP-Tokyo-Softbank-Reality-Vision ➔ LZ-TW-HiNet-家宽`），系统规范为简洁的三段式结构：
- `IIJ-美宽-Frontier`
- `LAX-美宽-Verizon`
- `LAX-美宽-Pavlov`
- `SB-日宽-SoftBank`
- `IIJ-日宽-BIGLOBE`
- `SB-台宽-HiNet`
- `IIJ-韩宽-KT`
- `IIJ-新宽-StarHub`

**YAML 输出形态示例**：
```yaml
proxies:
  # 1. 前置中转线路机
  - name: "JP-Tokyo-IIJ-Reality"
    type: vless
    server: 156.231.113.91
    port: 443
    uuid: "[REDACTED]"
    network: tcp
    tls: true
    udp: true
    reality-opts:
      public-key: "[REDACTED]"
      short-id: "[REDACTED]"
    client-fingerprint: chrome

  # 2. 链式住宅家宽落地节点 (dialer-proxy 挂载前置机)
  - name: "IIJ-美宽-Frontier"
    type: vless
    server: resi-us-frontier.example.com
    port: 8443
    uuid: "[REDACTED]"
    network: ws
    udp: true
    dialer-proxy: "JP-Tokyo-IIJ-Reality" # 关键：流量先经 IIJ，由 IIJ 转发至家宽
```

---

## 6. 全面动态化设计方案：面向高级 AI 的重构规范

> **用户核心需求**：
> “研究我们的目标，做好动态化，我可以在前端后端，增加任意类型节点：NAT 家宽节点、VPS 家宽节点、NAT 线路节点、VPS 线路节点，自定义调节节点组（比如自建美国家宽节点组，前端后端就可以配置好，节点组放哪些标签的节点等）。”

为满足此需求，必须将现存的“基于硬编码字符串正则（`IIJ`、`美宽`、`Frontier`）的过滤脚本”彻底重构为**基于元数据（Metadata）与声明式标签规则（Tagging Engine）的动态化编排系统**。

### 6.1 多维节点分类模型 (Node Type Taxonomy)

我们将节点解构为 4 个正交的核心维度：
1. **网络角色 (Role)**：
   - `transit`（骨干中继前置机，可被 dialer-proxy 引用）
   - `landing`（最终流量落地机）
   - `standalone`（独立直连单跳节点）
2. **基础设施架构类型 (InfraType)**：
   - `nat_resi`：NAT 家庭宽带（动态公网 IP / 端口映射 / 内网穿透型家宽）
   - `vps_resi`：原生住宅广播 IP 的独立 VPS（拥有住宅 ASN 且具备静态独立 IP）
   - `nat_line`：NAT 骨干专线（国内 NAT 中转机 / 共享端口中继线路）
   - `vps_line`：独立骨干 VPS 线路（4837 / 9929 / CMI / CN2 GIA / IIJ 优质机房线路）
   - `commercial`：第三方采购的商业订阅节点（如蜂窝云）
3. **地理区域 (Region)**：
   - `US`、`JP`、`HK`、`TW`、`SG`、`KR`、`UK`、`DE` 等标准 ISO 代码。
4. **归属权 (Ownership)**：
   - `self_hosted: true`（自建完全可控）
   - `self_hosted: false`（第三方外部订阅）

### 6.2 标签系统与元数据规范 (Tagging & Metadata Engine)

每一个节点在被系统收录时，均拥有结构化元数据（Metadata）：

```json
{
  "id": "node_us_frontier_01",
  "name": "US-Frontier-Resi-01",
  "display_name": "美宽-Frontier-01",
  "protocol": "vless",
  "link": "vless://...#US-Frontier-Resi-01",
  "meta": {
    "role": "landing",
    "infra_type": "nat_resi",
    "region": "US",
    "isp": "Frontier",
    "self_hosted": true,
    "tags": ["residential", "us-west", "ai-unlocked", "streaming-clean"],
    "relay": {
      "enabled": true,
      "preferred_transits": ["transit_lax_4837", "transit_iij_tokyo"],
      "auto_bind_dialer": true
    }
  },
  "status": {
    "online": true,
    "latency_ms": 168,
    "songzhong": false,
    "last_check_at": "2026-11-23T10:00:00Z"
  }
}
```

### 6.3 声明式动态策略组编排 (Declarative Policy Group Compiler)

用户不需要在前端或后端写死策略组代码，而是通过**声明式选择器（Selector Rules）**动态生成策略组：

#### 策略组定义结构 (JSON / YAML Schema)：
```yaml
custom_policy_groups:
  # 示例 1: 用户自定义的 "自建美国家宽节点组"
  - id: "grp_self_us_resi"
    name: "自建-美国家宽节点组"
    type: "select" # select | url-test | fallback | load-balance
    icon: "https://raw.githubusercontent.com/ashanzzz/.../us.png"
    position: "bottom" # top | middle | bottom (决定在 YAML 中的排布位置)
    selector:
      self_hosted: true
      infra_type: ["nat_resi", "vps_resi"]
      regions: ["US"]
      tags_include: ["residential"]
      tags_exclude: ["songzhong"]
      online_only: true
      include_relays: true # 是否自动包含该落地机生成的链式节点

  # 示例 2: 用户自定义的 "低延迟线路分组"
  - id: "grp_low_ping_lines"
    name: "自建-低延迟专线组"
    type: "url-test"
    url: "http://www.gstatic.com/generate_204"
    interval: 300
    tolerance: 50
    position: "bottom"
    selector:
      self_hosted: true
      infra_type: ["nat_line", "vps_line"]
      max_latency_ms: 100
      online_only: true

  # 示例 3: 业务分流组按标签动态绑定
  - id: "grp_ai_service"
    name: "AI服务"
    type: "select"
    position: "middle"
    static_priorities: ["自建-美国家宽节点组", "自建-低延迟专线组"]
    selector:
      tags_include: ["ai-unlocked"]
      online_only: true
```

#### 编译器编译逻辑（Compiler Algorithm）：
1. 遍历当前所有活跃节点；
2. 针对每个节点，核验其 `meta` 是否满足 `selector` 的交集条件；
3. 若节点为 `landing` 且 `include_relays: true`，编译器自动为该节点派生绑定的中继节点（如 `LAX-美宽-Frontier`），并一并加入该组；
4. 过滤并消除重复项，按 `position` 排序策略组（保证运维组永远处于置底位置）。

---

### 6.4 后端 (Go) 架构重构蓝图与接口定义

在 `vpn-probe` 项目的 Go 代码中，需对相关模块进行演进：

#### A. 核心模型重构 (`internal/model/node.go`)
```go
package model

// InfraType 基础设施类型
type InfraType string

const (
	InfraNatResi    InfraType = "nat_resi"    // NAT 家宽
	InfraVpsResi    InfraType = "vps_resi"    // VPS 家宽
	InfraNatLine    InfraType = "nat_line"    // NAT 线路
	InfraVpsLine    InfraType = "vps_line"    // VPS 线路
	InfraCommercial InfraType = "commercial"  // 外部商业专线
)

// NodeMeta 节点元数据
type NodeMeta struct {
	Role         string    `json:"role"`           // transit | landing | standalone
	InfraType    InfraType `json:"infra_type"`     // 核心基础设施类型
	Region       string    `json:"region"`         // US, JP, HK, TW, etc.
	ISP          string    `json:"isp"`            // Frontier, Verizon, SoftBank
	SelfHosted   bool      `json:"self_hosted"`    // 是否为自建
	Tags         []string  `json:"tags"`           // 标签池
	RelayTransit string    `json:"relay_transit"`  // 绑定的前置机 ID/Name (用于链式代理)
}

// CustomGroupConfig 动态策略组配置
type CustomGroupConfig struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`     // select, url-test, fallback
	Position  string         `json:"position"` // top, middle, bottom
	Selector  GroupSelector  `json:"selector"`
	CreatedAt time.Time      `json:"created_at"`
}

type GroupSelector struct {
	SelfHosted     *bool       `json:"self_hosted,omitempty"`
	InfraTypes     []InfraType `json:"infra_types,omitempty"`
	Regions        []string    `json:"regions,omitempty"`
	TagsInclude    []string    `json:"tags_include,omitempty"`
	TagsExclude    []string    `json:"tags_exclude,omitempty"`
	MaxLatencyMs   int         `json:"max_latency_ms,omitempty"`
	OnlineOnly     bool        `json:"online_only"`
	IncludeRelays  bool        `json:"include_relays"`
}
```

#### B. REST API 接口扩展 (`internal/api/routes.go`)
- `GET /api/v1/nodes`：获取所有节点及其 `meta` 标签。
- `POST /api/v1/nodes`：动态录入任意类型的节点（支持指定 `nat_resi`、`vps_line` 等分类与自定义标签）。
- `PATCH /api/v1/nodes/:id/meta`：动态修改单个节点的标签、前置机绑定关系。
- `GET /api/v1/policy-groups`：获取当前所有的动态策略组配置。
- `POST /api/v1/policy-groups`：创建全新的动态策略组（提供标签选择器规则）。
- `PUT /api/v1/policy-groups/:id`：调整策略组的选择器（如动态增减所放的标签）。
- `POST /api/v1/substore/compile-and-sync`：触发编译器，将当前动态配置编译并推送到东京 Sub-Store 或直接输出标准 ClashMeta YAML。

---

### 6.5 前端 (React + TS) 可视化交互与管理蓝图

在 `web/src` 中，前端控制台需扩展以下核心视图：

1. **节点录入与属性标记模态框 (Node Edit & Tag Modal)**：
   - 基础设施类型单选：`[NAT家宽] [VPS家宽] [NAT线路] [VPS线路] [外部专线]`
   - 区域选择器：下拉选择国家/地区。
   - 标签选择器 (Tag Input Chips)：可自由输入或选择现有标签（如 `美国家宽`、`Frontier`、`AI推荐`）。
   - 链式代理配置：若选择 `落地机 (landing)`，允许下拉选择绑定的前置中转机（`IIJ-Tokyo`、`LAX-4837`），系统自动实时预览生成的精简链式名称。
2. **策略组可视化调节工作台 (Policy Groups Studio)**：
   - 列表展示所有系统组与自定义组，标注其排布层级（置顶业务组 / 置底自建组）。
   - 规则编辑器：
     - 用户点击“新建策略组”：输入组名 `自建-美国家宽节点组`；
     - 勾选类型：`[x] NAT家宽 [x] VPS家宽`；
     - 选择地区：`[x] 美国 (US)`；
     - 标签过滤器：选择必须包含标签 `residential`；
     - **即时预览区 (Live Preview)**：右侧面板实时展示当前满足该筛选器的节点清单（含节点实时在线状态与延迟）。
   - 点击“发布生效”，直接同步到后端与 Sub-Store，客户端下次更新订阅立即生效。

---

## 7. 现存系统缺陷与高级 AI 修复指导

高级 AI 在接手工程实现时，需重点解决当前系统发现的 3 个历史遗留 Bug：

### 缺陷 1：Sub-Store 组合订阅报重复节点警告
- **位置**：`/opt/1panel/apps/sub-store/data/sub-store.json` 与 `root.json`
- **问题**：`ashan-us-resi` 和 `ashan-jp-resi` 中物理混入了前置机节点，而组合订阅又包含了 `ashan-transit`，导致汇聚时出现 `US-LAX-ACCK-4837-Reality` 冲突警告。
- **解法**：从子订阅内容中删除前置机的重复定义，使每个子订阅严格单一职责。

### 缺陷 2：`ashan` 订阅与 `Q` 订阅配置不同构
- **位置**：`sub-store.json` 的 `files` 映射。
- **问题**：`ashan` 仅绑定了 `ashan-纯净`（43 个节点），缺少蜂窝云；而用户目标要求“Q 和 ashan 一样，都是外部连接+自建连接”。
- **解法**：统一 `ashan` 的 `sourceName`，使其同构聚合蜂窝云与自建链式节点。

### 缺陷 3：旧脚本无法正确识别精简后的链式节点送中状态
- **位置**：`/opt/1panel/www/sites/free.1992004.xyz/index/classify_songzhong.js`
- **问题**：判断链式节点仍依赖 `➡️` 箭头分割，对 `IIJ-美宽-Frontier` 格式直接失效。
- **解法**：升级正则表达式，适配 `^(IIJ|LAX|SB)-(美宽|日宽|台宽|韩宽|新宽)` 前缀，正确剥离前置机并识别落地机真实送中属性。

---
*本文档由 QA 审计与架构分析引擎生成，作为项目底层演进的权威实施蓝图。*
