# HKG-3（懒猫云 10674）全链路纳管与原生部署实施计划

- 计划时间：2026-11-28 10:53 CST
- 工作区：`/opt/data/projects/vpn-probe`（`master` @ `e9bba42`）
- 关联仓库：`personal-vps-monitor`（`b04e77d`）、`ashan-node-sentinel`
- 状态：**PLAN ONLY** —— 本文件是唯一改动产物，项目代码零改动

---

## 1. 目标

把懒猫云实例 `10674`（`HKG-3`，`216.236.60.76`，`category=固定出口`，`region=香港`，`ssh_port=0`）接入现有自建代理全链路，实现：

1. 宿机（Alpine Linux / OpenRC / 1C-512M / 无 Docker / 无 python3）上以**原生单二进制 sing-box** 提供 VLESS+Reality 入站；
2. `personal-vps-monitor`（`:3338`）能对其执行原生部署、增删改入站、换公网端口、健康巡检；
3. `vpn-probe`（`:33335`）能纳管它、真实探测、按线路机（transit）分类、参与五维策略组与订阅发布；
4. `ashan-node-sentinel` 能识别该节点并纳入防送中/舰队台账；
5. 全链路可验证：节点在线、出口 HK 洁净、出现在 `ashan-transit` 与 `ashan-master` 订阅中。

---

## 2. 已实证事实基线

### 2.1 宿机 10674

| 项 | 值 |
|---|---|
| 系统 | Alpine Linux + OpenRC，BBR 已开，`/dev/net/tun` 存在 |
| 规格 | 1C / 512M / 1024M 磁盘（余 1023.8M） |
| 缺失 | `docker` `podman` `sing-box` `xray` `openssl` `python3` 全部 MISSING |
| 监听 | 仅 `22`（sshd，`AllowTcpForwarding no`、`GatewayPorts no`）、`68/546`（dhcpcd） |
| 端口映射 | `port_limit=10`，现存 `{id:50285, tcp, private 30021 -> public 30021}`，**无进程监听** |
| SSH | `ssh_port=0`（未映射公网 22）→ **只能走 `:3338` WebSocket PTY 通道** |
| 流量 | 329.05 / 760.00 GB（2026-12-05 重置） |

### 2.2 代码现状（已读实证）

**personal-vps-monitor**

- `pkg/core/sshexec/client.go:125-155` `fillDefaults`：`lazycat` 保留 `SSHPort=0`（正确，勿改）。
- `pkg/core/sshexec/client.go:229-258` `ExecuteCommand`：`ProviderID=="lazycat"` 已优先走 `lazyCatExecutorFn`（WS PTY），失败且无 22 映射时直接返回错误 —— **该通道可直接用于 10674**。
- `pkg/core/sshexec/inspector.go:47-122` `InspectProxies`：已扫描 `/etc/sing-box/config.json`，并调用 `providers.PortMapper.ListPortMappings` 做 NAT 私网→公网端口映射。
- `pkg/core/sshexec/inspector.go:229-247`：NAT 容器未映射端口的代理**不生成链接**（行为正确）。
- `pkg/core/sshexec/inspector.go:444-560` `parseSingBoxInbounds`：已能解析 `inbounds[].tls.reality.{public_key,short_id}`。
- `pkg/core/sshexec/inspector.go:688-727` `GenerateProxyLink`：节点名 = `inst.Name + "-" + remark`。
- `pkg/core/sshexec/lifecycle.go:64-177` `ChangeProxyPort`：3x-ui 走 DB；原生走 `sed -i '"listen_port": OLD'`（**依赖固定空格**），随后 `ss -tulpn | grep` 校验，**完全未触碰 NAT 端口映射**。
- `pkg/core/sshexec/lifecycle.go:217-283` `AddProxy` / `421-457` `DeleteProxy`：**强绑定 3x-ui SQLite**，非 3x-ui 直接报 `unsupported host environment`。
- `pkg/providers/lazycat/provider.go:105-269` `enrichInstance`：`category` 由 `isResidential()` 决定，`10674` 落 `固定出口`；`region` 经 `detectLazyCatRegion` 正确得到 `香港`。
- `pkg/providers/lazycat/provider.go:299-302` `isResidential`：命中 `residential` / `家宽` / `global` 才判家宽 —— `HKG-Premium` 不含这些词，**判定安全**。
- `pkg/providers/lazycat/provider.go:362-420` `SyncEgressForInstance`：硬编码 `python3 /opt/lazycat-multi-egress/sync_egress.py`（**仅适用 11433 家宽机，10674 无 python3，必须绕开**）。
- `pkg/providers/lazycat/client.go:470-492` `AddPortMapping`：`POST /instances/{id}/ports {privatePort, protocol, remark}` → 返回 `mapping.publicPort`。
- `pkg/providers/lazycat/client.go:494-499` `DeletePortMapping`：`DELETE /instances/{id}/ports/{portId}`，需带 `{}` body。
- `pkg/api/lazycat.go:18-43` `resolveLazyCatInstanceID`：`preferResidential=true` 时优先 `11433`；仅在未指定 id 时有影响。
- `pkg/api/lazycat.go:218-264` `POST /api/lazycat/terminal/exec`：`{instance_id, command, timeout_sec}` → `{output, exit_code}`。
- `pkg/api/router.go:143` `GET /api/instances`；`:184-185` `/api/lazycat/terminal/{ws,exec}`（exec 超时 120s，豁免全局 45s）。

**vpn-probe**

- `internal/vpsmgr/manager.go:240-291` 同步 `:3338` 实例：`ProviderID=="lazycat"` **直接 `continue` 跳过**；`SSHPort<=0` 强改 `22`；新实例要求 `pass != ""` 且 `hi.IP != ""`。
- `internal/vpsmgr/detector.go:47-83` `RunSSH`：即使有 `sshRunner` 注入点，默认仍是直连 `ssh.Dial(IP:SSHPort)`。
- `internal/vpsmgr/detector.go:168-189` native 分支：仅凭 `which` 输出含 `/sing-box` 即置 `Status="running"`，**未校验进程/端口**。
- `internal/api/server.go:2027-2043` `GET /api/vps/manage` → `ListVPSInstances` / `RefreshVPSInstances`。
- `internal/api/server.go:2046-2101` `manage/install`、`manage/update`；`:2104-2129` `blocked-analysis`；`:2132-2161` `heal/port`（成功后异步 `RunAllChecks`）；`:2164-2191` `lifecycle`。
- `internal/monitor/scheduler.go:896`：`model.IsResidentialNode(...) || knownResi[...]` 决定 `residential` / `transit`。
- `internal/monitor/scheduler.go:913-919`：`FaultPortBlocked` 自愈**仅对 `!isResi`** 生效。
- `internal/monitor/scheduler.go:1384-1411` `getHostGroupKey`：仅显式识别三台旧线路机，其余退化为 `host`/`name`。
- `internal/monitor/scheduler.go:1535-1647` `ClassifyIntoFiveGroups`：线路机入 `ashan-transit`；直连家宽/链式按 `ResolveCountry` 入 US/JP/Other。
- `internal/model/node.go:219-220` `resolveRegionText` 已支持 `HK`/`HKG`/`香港` → `"HK"`（`HKG-3` 自动剥离数字）。
- `internal/model/node.go:248-260` `IsResidentialNode`：命中 `"LZ-"` **即判家宽**。
- `internal/store/tab_store.go:57`（`transit`）、`:74`（`residential` 含 `LZ-`）、`:117`（`other_region`）：关键字集合中**均无 `HK`/`HKG`/`香港`**。
- `internal/substore/client.go:431-514` `SyncFiveGroups`：固定同步 5 组 + `ashan-master` / `ashan-纯净` / `col-business`。
- `internal/substore/profile_sync.go`：`col-<profile>` 绑定文件（`Q/q`、`ashan/Ashan`、`business/Business`）。
- 线上 `:33335` 现状（2026-11-28 实测）：`/api/vps/manage` count=0；`/api/monitor/status` 13 节点（3 transit + 10 residential）；`/api/orchestration/atoms` 43；9 个 Tab；8 个策略组 —— **全量无 HKG-3**。

**ashan-node-sentinel**

- `data/fleet_registry.json`：`nodes` 仅 `us-lax` / `jp-iij` / `jp-sb` / `lazycat-global(11433)`；`substore.clean_own_servers` 无 `216.236.60.76`。
- `app/fleet_registry.py:53+` `update_lazycat_ingress` 仅处理 `lazycat-global`。
- `app/lazycat_probe.py:139-144` `get_our_host_status()` 硬编码只查 `Global-2`。
- `app/plugins/proxy_pool_allocator.py:43` 已消费 `:33335/api/monitor/status?reveal=true`（无需改）。
- `app/plugins/proxy_songzhong.py:74` IP→国家回退仅含 `45.192.242.72` / `156.231.113.91`。

---

## 3. 关键设计决策

### D1 命名硬规则（安全前提）

节点名 **必须** 形如 `HK-HKG3-NAT`。

禁止出现：`LZ-`、`动态`、`家宽`、`residential`、`global`、`Global`。
理由：`model/node.go:248-260` 的 `IsResidentialNode` 与 `tab_store.go:74` 的 `residential` 关键字都会把含这些词的名字判为家宽，导致线路机被错分类、`FaultPortBlocked` 自愈被跳过。

### D2 宿机不装 Docker，不装 python3

- sing-box 用官方静态二进制（`linux-amd64`，CGO disabled）放 `/usr/local/bin/sing-box`；
- 所有远程脚本只用 BusyBox `sh` 内建 + `wget`/`tar`/`rc-service`；
- **密钥生成用宿机自身** `sing-box generate reality-keypair` 与 `sing-box generate uuid`，避免 Go `ecdh` 输出与 sing-box 期望编码不一致的风险；Go 侧只负责解析 stdout。
  （备选：`pkg/core/sshexec/lifecycle.go:44-62` 的 `GenerateRealityKeyPair()` 已在生产验证过 Xray 兼容，作为 fallback。）

### D3 换端口策略：改 NAT 公网端口，不改宿机配置

懒猫云 NAT 实例私网端口可保持 `30021` 不变。端口被墙时：

1. `DeletePortMapping(10674, mappingID=50285)`
2. `AddPortMapping(10674, 30021, "tcp", "hkg3-vless")` → 新 `publicPort`
3. 用新 `publicPort` 重建节点 URL，写回 vpn-probe 节点库并触发 `SyncFiveGroups`

优点：不重启 sing-box、不改配置文件、无 `sed` 空格依赖、原子且可回滚（失败即删新映射、恢复旧映射）。
注意：`port_limit=10`，每次换端口净占用不增加。

### D4 配置写入：Go 全量渲染 + 原子替换 + 预检回滚

`sig-box config.json` 由 Go 侧按结构体渲染整份 JSON，通过 `base64` 传输避免 heredoc 转义：

```
printf '%s' '<b64>' | base64 -d > /etc/sing-box/config.json.new
sing-box check -c /etc/sing-box/config.json.new
mv config.json.new config.json && rc-service sing-box restart
```

失败路径：`mv config.json.bak config.json && rc-service sing-box restart`。

不使用 `sed -i`（`lifecycle.go:130-160` 现有实现对空格敏感，是本计划的已知缺陷）。

### D5 `:3338` 作为 10674 的唯一控制通道

vpn-probe 侧所有对 10674 的远程命令统一走：

```
POST http://<pvm-host>:3338/api/lazycat/terminal/exec
{ "instance_id": "10674", "command": "...", "timeout_sec": 60 }
```

该端点已有 120s 超时豁免（`router.go:185`）。需要配置一份服务间凭据（存 `.env`，不落库、不进仓库、不写日志）。

### D6 分类归属

- 10674 = **线路机 / transit**（`固定出口`，不是家宽）；
- 承载能力：`1C/512M` + `760GB/月` → 定位为**香港落地/前置中继**，承担链式 `HK 中继 ➔ 家宽` 与 HK 直连；
- 不参与 `ashan-us-resi` / `ashan-jp-resi`；HK 直连节点归 `ashan-transit`，HK 链式归 `ashan-other`（`ResolveCountry` 已能解析 HK）。

---

## 4. 分阶段 TDD 任务

> 每阶段顺序固定：**先写失败测试 → 最小实现 → 跑测试 → 真机验证 → 提交**。
> 提交粒度：每阶段 1 个 commit；`personal-vps-monitor` 与 `vpn-probe` 分别提交、分别打 tag。

---

### Phase 0 — 基线冻结（0.5h）

1. 记录当前基线：
   ```bash
   cd /opt/data/projects/vpn-probe && git rev-parse HEAD && go build ./... && go test ./... 2>&1 | tail -20
   cd /opt/data/projects/personal-vps-monitor && git rev-parse HEAD && go build ./... && go test ./... 2>&1 | tail -20
   ```
2. 快照线上配置（只读）：`/mnt/cache/appdata/vpn-probe/config.yaml`、Sub-Store 当前 `subscriptions`/`collections` 列表；存 `tmp/20261128/hkg3/baseline/`。
3. 快照 10674 现场（只读，经 `:3338` exec）：
   ```sh
   uname -a; cat /etc/alpine-release; rc-status; ss -tlnp; ls -la /etc/init.d/; cat /etc/init.d/vpsm-firstboot
   ```
4. 确认 `:3338` `/api/instances` 可返回 `10674`，且 `terminal/exec` 可执行 `echo ok`。

**通过标准**：两条命令退出码 0；`echo ok` 经 WS PTY 返回 `ok`。

---

### Phase 1 — `personal-vps-monitor`：原生 sing-box 生命周期（核心）

#### 1.1 新增 `pkg/core/sshexec/singbox_native.go`

先写测试 `singbox_native_test.go`（纯函数，无网络）：

| 测试 | 断言 |
|---|---|
| `TestRenderSingBoxConfig_HKG3` | 输出 JSON 可 `json.Unmarshal`；`inbounds[0].listen_port==30021`；`type=="vless"`；`tls.reality.enabled==true`；`users[0].flow=="xtls-rprx-vision"` |
| `TestRenderSingBoxConfig_RejectsBadPort` | `port<=0 \|\| port>65535` 返回 error |
| `TestParseRealityKeyPair` | 解析 `sing-box generate reality-keypair` 真实输出样本 → `private/public/short_id` 三字段非空 |
| `TestSingBoxInstallScript_NoPythonNoDocker` | 渲染出的安装脚本**不含** `python3`、`docker`、`podman`、`jq`、`sed -i`；含 `rc-service`、`/usr/local/bin/sing-box` |
| `TestSingBoxInstallScript_ArchGuard` | 非 `x86_64` 环境时脚本以非 0 退出并输出明确原因 |

实现函数（签名固定）：

```go
type SingBoxInbound struct {
    Tag         string
    ListenPort  int
    UUID        string
    Flow        string
    SNI         string
    PrivateKey  string
    ShortID     string
}

func RenderSingBoxConfig(in SingBoxInbound) ([]byte, error)
func BuildInstallScript(cfg []byte, arch string) (string, error)   // 返回 sh 脚本
func GenerateRealityKeyPairRemote(ctx, inst) (priv, pub, sid string, err error)
```

#### 1.2 `pkg/core/sshexec/inspector.go` 增强

测试 `TestParseSingBoxInbounds_NativeHKG3`：喂真实 `sing-box` 配置（含 `listen_port`、`reality`）→ 断言解析出 `Port=30021`、`Security="reality"`、`SNI`、`UUID`、`Flow` 非空。

修改点：
- `parseSingBoxInbounds` 增加对 `listen` 缺失（默认 `0.0.0.0`）的容忍；
- `parseListeningPorts`（`:287+`）增加 BusyBox `netstat -tlnp` 输出格式分支（`0.0.0.0:30021` 形式，当前 `portRegex = [:](\d+)\s+` 对 `ss` 与 `netstat` 两种格式都要覆盖）—— 增加用例 `TestParseListeningPorts_BusyBoxNetstat`。

#### 1.3 `pkg/core/sshexec/lifecycle.go` 拆分与扩展

新增（保留原 `ChangeProxyPort` 不动，避免回归）：

```go
// 新增：NAT 实例改公网端口（D3）
func RepublishNATPort(ctx context.Context, inst *providers.Instance, privatePort int, oldMappingID int, remark string) (newPublicPort int, newMappingID int, err error)

// 新增：原生 sing-box 增删入站（D4）
func AddNativeSingBoxInbound(ctx, inst, in SingBoxInbound) (*ProxyInfo, error)
func DeleteNativeSingBoxInbound(ctx, inst, tag string) error
func NativeSingBoxHealth(ctx, inst) (running bool, listeningPorts []int, version string, err error)
```

测试：

- `TestRepublishNATPort_RollbackOnAddFailure`：注入 fake PortMapper，`Add` 失败 → 断言调用了 `Delete(newID)` 并恢复旧映射；
- `TestAddNativeSingBoxInbound_RejectsDuplicateTag`；
- `TestDeleteNativeSingBoxInbound_RefusesLastInbound`（不允许删到 0 入站，防断流）。

#### 1.4 关键修复：`SyncEgressForInstance` 不得对 10674 生效

`provider.go:362-420` 目前会被任何调用方以空 `instanceID` 触发，并 fallback 到 `defaultInst` / `""` / `"11433"`。
修复：仅当 `isResidential(...)==true` 时才允许走 `sync_egress.py`；否则返回 `ErrNotResidentialInstance`。

测试：`TestSyncEgress_RejectsFixedExitInstance`。

#### 1.5 HTTP 层

`pkg/api/lazycat.go` 新增（放同一文件，保持内聚）：

```
GET  /api/lazycat/singbox/health?instance_id=10674
POST /api/lazycat/singbox/deploy       {instance_id, port, node_name}
POST /api/lazycat/singbox/inbound      {instance_id, add|delete, ...}
POST /api/lazycat/ports/republish      {instance_id, private_port, mapping_id, remark}
```

测试 `pkg/api/lazycat_singbox_test.go`：httptest + fake provider，断言 400（缺参）、409（重复 tag）、200（成功返回 `public_port`）。

**Phase 1 真机验证**（经 `:3338` exec）：
```sh
/usr/local/bin/sing-box version
rc-service sing-box status
ss -tlnp | grep 30021
```
从 Unraid 用 `curl -x` 或 `vpn-probe` 探针验证 `216.236.60.76:30021` 可出网。

**通过标准**：`version` 有输出；`status` 为 `started`；`30021` 在 LISTEN；外部探测 `Online=true`、`ExitCountry=HK`。

---

### Phase 2 — `vpn-probe`：纳管 / 探测 / 自愈

#### 2.1 配置增加 `:3338` 执行器

`internal/config/config.go` 增加（字段名待实施时按现有风格定）：

```yaml
vps_hub:
  base_url: "http://<pvm>:3338"
  token: ""        # 从环境变量注入，禁止落 yaml
```

测试：`TestConfig_VPSHubTokenFromEnv`。

#### 2.2 `internal/vpsmgr/exec_remote.go`（新文件）

```go
func (d *Detector) runViaVPSHub(ctx context.Context, inst *VPSInstance, cmd string, timeout time.Duration) (string, error)
```

行为：
1. `inst.ProviderID == "lazycat" || inst.SSHPort <= 0` → `POST {hub}/api/lazycat/terminal/exec`；
2. 解析 `{output, exit_code}`；`exit_code != 0` 返回错误但**保留 output**（供诊断）；
3. 直连 SSH 路径保持不变作为 fallback。

测试 `exec_remote_test.go`：`httptest.Server` 模拟 `:3338`
- `TestRunViaVPSHub_Success`
- `TestRunViaVPSHub_NonZeroExitKeepsOutput`
- `TestRunViaVPSHub_Timeout`
- `TestRunSSH_RoutesLazycatToHub`（断言 `detector.RunSSH` 对 lazycat 不发 `ssh.Dial`）

#### 2.3 `internal/vpsmgr/manager.go` 放行固定出口

`manager.go:240-291` 改为：

| 原行为 | 新行为 |
|---|---|
| `ProviderID=="lazycat"` → `continue` | `category=="全球家宽"` → `continue`（家宽网关仍不纳管为标准 SSH 机）；`固定出口` → **纳入** |
| `SSHPort<=0` → 改 `22` | `lazycat` 保持 `0`（由 `exec_remote` 识别），非 `lazycat` 仍改 `22` |
| 新实例要求 `pass != ""` | 对 `lazycat` 放开（口令仅用于 WS PTY 之外的兜底；无口令时依赖 provider token） |

新增字段 `VPSInstance.ExecMode string`（`"ssh"` | `"hub"`），在同步时按 provider 设定。

测试 `manager_test.go` 追加：
- `TestSyncFromVPSHub_IncludesFixedExitLazycat`
- `TestSyncFromVPSHub_SkipsResidentialLazycat`
- `TestSyncFromVPSHub_LazycatKeepsZeroSSHPort`

#### 2.4 `detector.go` native 真实存活检测

`:168-189` 改为：识别到 binary 后追加执行

```sh
sing-box version; rc-service sing-box status || systemctl is-active sing-box; ss -tlnp | grep -c ':PORT'
```

判定：`status` 含 started/active **且** 端口在 LISTEN → `Status="running"`；否则 `"installed_stopped"`。

测试：`TestInspectVPS_NativeRunning` / `TestInspectVPS_NativeStopped`（fixture 字符串）。

#### 2.5 `auto_healer.go` 放行 NAT 换端口

`:176`（行号以实施时 `grep -n lazycat` 为准）当前硬禁全部 `lazycat`。
改为：`category=="固定出口"` 允许 `NAT Republish` 路径；`全球家宽` 仍硬禁。

测试：`TestAutoHealer_PortBlocked_FixedExitAllowed` / `TestAutoHealer_PortBlocked_ResidentialForbidden`。

#### 2.6 拓扑 / 分组 / Tab

| 文件 | 修改 |
|---|---|
| `internal/probe/detector_topology.go`（约 `:137-149`） | 增加 `216.236.` → `"HK"` |
| `internal/monitor/scheduler.go:1384-1411` | `getHostGroupKey` 增加 `host=="216.236.60.76" \|\| strings.Contains(name,"HKG")` → `"HKG-3"` |
| `internal/publish/compiler.go`（约 `:187-200`） | 线路机别名增加 `HKG` / `HK` / `香港` |
| `internal/monitor/orchestrator.go`（约 `:1109-1134`） | 同上 |
| `internal/store/tab_store.go:57` | `transit` 关键字增加 `"HK"`, `"HKG"`, `"香港"` |
| `internal/store/tab_store.go:117` | `other_region` 增加 `"HK"`, `"HKG"`, `"香港"` |
| `internal/store/tab_store.go:74` | **不动**（`LZ-` 仍需判家宽） |

测试：`TestGetHostGroupKey_HKG3`、`TestDetectorTopology_216_236_is_HK`、`TestTabStore_TransitMatchesHKG3`、`TestClassifyIntoFiveGroups_HKG3GoesToTransit`。

#### 2.7 单测中的防回归

新增 `internal/model/node_test.go` 用例：`IsResidentialNode("HK-HKG3-NAT", ...) == false`；`ResolveCountry("", "HK-HKG3-NAT") == "HK"`。

**Phase 2 通过标准**：
- `go test ./...` 全绿；
- 线上 `GET :33335/api/vps/manage` → `count >= 1` 且含 `10674`；
- `GET :33335/api/monitor/status` 出现 `HK-HKG3-NAT`，`node_type=="transit"`、`online==true`、`exit_country=="HK"`、`china_exit==false`；
- `POST :33335/api/orchestration/sync-five-groups` 后 `GET /api/substore/nodes?name=ashan-transit` 含该节点。

---

### Phase 3 — 订阅 / 策略组 / 前端可见性

1. `internal/substore/client.go:444`：`ashan-transit` 无需改（自动包含），但**验收**必须看到 HK 节点。
2. `ashan-other` 策略组（线上 `:33335/api/v1/policy-groups`）：`countries` 增加 `"HK"`。改动位置在策略组默认定义（`custom_group.go` / 线上配置），实施时以 `grep -rn "ashan-other"` 定位。
3. `internal/api/server.go` `handleGetTabs` 返回的 9 个 Tab 中 `transit` / `other_region` 自动带上新关键字（来自 `tab_store` 默认值）；**线上已有 tab 数据需一次性迁移**：提供 `POST /api/orchestration/tabs/migrate-hk`（幂等：仅当关键字缺失时追加）。
   - 若不想加端点，则用一次性只读导出 → 修改 → 写回，并在计划外单独确认（写入线上配置需用户批准）。
4. 前端（Steel CDP 验证）：拓扑/节点列表/策略组页面能看到 `HK-HKG3-NAT`，无 `undefined`、无 `>=400` 请求。

**通过标准**：`/api/substore/nodes?name=ashan-transit` 含 HK 节点；`/api/orchestration/tabs` 的 `transit` 关键字含 `HKG`；`/api/v1/policy-groups` 的 `ashan-other` 含 `HK`；Steel 截图 + Network 面板无异常。

---

### Phase 4 — `ashan-node-sentinel` 适配

1. `data/fleet_registry.json` 新增：

```json
"hk-hkg3": {
  "id": "hk-hkg3",
  "name": "香港-HKG3-固定出口",
  "provider": "LazyCatCloud",
  "instance_id": 10674,
  "role": "ingress_transit",
  "host": "216.236.60.76",
  "region": "HK",
  "category": "固定出口",
  "ssh_port": 0,
  "exec_channel": "vps-hub:3338"
}
```

2. `substore.clean_own_servers` 追加 `216.236.60.76`；`songzhong_ip_blacklist` **不**追加。
3. `app/lazycat_probe.py:139-144` `get_our_host_status()` 改为从 registry 读取目标 host 名列表，兼容 `Global-2` 与 `HKG-3`。
4. `app/plugins/proxy_songzhong.py:74` IP→国家回退增加 `216.236.60.76 → "Hong Kong"`。
5. 测试：`pytest` 若已有测试目录则补 `test_fleet_registry_hkg3.py`（断言节点存在、`clean_own_servers` 含该 IP、`get_our_host_status` 返回非 None）。

**通过标准**：sentinel 启动无异常；`/api`（若暴露）能返回 `hk-hkg3`；防送中插件对该 IP 判定为洁净。

---

### Phase 5 — 端到端验收与发布

| # | 验收项 | 方法 | 通过标准 |
|---|---|---|---|
| 1 | 宿机服务 | `:3338` exec | `rc-service sing-box status` = started |
| 2 | 端口可达 | 外部 TCP 探测 `216.236.60.76:30021` | 握手成功 |
| 3 | 节点协议 | vpn-probe 探针 | `online=true`，`latency < 300ms` |
| 4 | 出口归属 | 探针 `exit_ip` / `exit_country` | `216.236.60.76` / `HK`（或上游 HK 落地） |
| 5 | 送中 | 探针 `china_exit` | `false` |
| 6 | 纳管 | `GET :33335/api/vps/manage` | 含 `10674`，`software_type=sing-box`，`deploy_mode=native`，`status=running` |
| 7 | 分组 | `GET :33335/api/substore/nodes?name=ashan-transit` | 含 `HK-HKG3-NAT` |
| 8 | 策略组 | `GET :33335/api/v1/policy-groups` | `ashan-transit` 含该节点 |
| 9 | 自愈 | `POST /api/vps/heal/port {node_name:"HK-HKG3-NAT"}` | 返回新 `public_port`，旧映射已删，新映射可连 |
| 10 | 订阅 | 客户端拉 `ashan-master` | 含 HK 节点且可连通 |
| 11 | 前端 | Steel CDP（`192.168.8.23:19223`） | DOM 见节点；Network 无 `>=400`；Console 无 error |
| 12 | 回归 | `go test ./...` × 2 仓库 | 全绿 |

发布顺序：`personal-vps-monitor` → 重建容器 → `vpn-probe` → 重建容器 → sentinel。
（重建容器前需用户确认；`/mnt/user/appdata/personal-vps-monitor` 与 `/mnt/cache/appdata/vpn-probe/data` 严禁覆盖。）

---

## 5. 风险与回滚

| 风险 | 影响 | 缓解 |
|---|---|---|
| 512M 内存不足，sing-box OOM | 节点掉线 | 单入站、无日志落盘（`log.level=warn`, `output=""`）、`GOGC` 默认；监控 `dmesg | grep -i oom` |
| Alpine musl 与 sing-box 动态链接不兼容 | 二进制无法启动 | 只用官方 `linux-amd64` 静态构建；安装后立即 `sing-box version` 校验 |
| 无 `openssl`/`python3` 导致脚本失败 | 部署中断 | Phase 1 测试强制断言脚本不含这些命令 |
| NAT 映射删除顺序错误导致断流 | 节点不可用 | D3 顺序：先加后删（新映射成功才删旧），失败自动回滚 |
| `LZ-` 命名触发家宽误判 | 自愈被跳过 | D1 命名硬规则 + `node_test.go` 防回归 |
| 线上 tabs/policy-groups 写入 | 影响现有 3 台线路机 | 迁移端点幂等 + 写前导出快照 + 写后立刻 `RunAllChecks` 校验旧节点仍在 |
| 流量消耗 | 用户配额 | 只在夜间/联调窗口执行；单次探测 < 50MB；不做全量测速 |

回滚：
- 代码：每阶段独立 commit，`git revert <sha>` 即可；
- 宿机：`rc-service sing-box stop && rm -f /usr/local/bin/sing-box /etc/sing-box/config.json /etc/init.d/sing-box`，NAT 映射恢复 `50285`；
- 线上配置：Phase 0 快照目录直接回灌。

---

## 6. 明确不做的事

- 不在 10674 安装 Docker / podman / python3 / 3x-ui；
- 不改造 `11433`（全球家宽）的任何现有行为；
- 不给懒猫云实例做任何销毁/重装操作；
- 不实现"宿机内多入站热插拔文件目录"（过度设计，当前只需 1 个入站）；
- 不改 `tab_store.go:74` 的 `LZ-` 家宽规则；
- 不把任何 token / 密码 / UUID / `pbk` / `sid` 写进代码、计划或提交信息（一律 env 或运行时读取）。

---

## 7. 交付物

1. `personal-vps-monitor`：`pkg/core/sshexec/singbox_native.go`、`lifecycle.go` 扩展、`inspector.go` BusyBox 兼容、`pkg/api/lazycat.go` 新端点、`derive` 测试文件；
2. `vpn-probe`：`internal/vpsmgr/exec_remote.go`、`manager.go`/`detector.go`/`auto_healer.go` 修复、拓扑与 Tab 关键字、测试；
3. `ashan-node-sentinel`：`fleet_registry.json` + 插件适配；
4. 本计划文件与 Phase 0 基线快照。
