# Dnscheck

[English](README.md) | [简体中文](README.zh-CN.md)

跨平台、基于终端 UI（TUI）的多协议 DNS 性能测试与 IP 地理位置查询工具 —— 纯 Go 实现，不依赖 `nslookup`、`dig`、`ping` 等系统自带组件。

## 功能特性

- **双 Tab TUI 界面**（基于 [bubbletea](https://github.com/charmbracelet/bubbletea)）：
  - **Page 1 — DNS 节点性能仪表盘**：针对每台配置的 DNS 服务器测量应用层 DNS RTT（每节点 3 次重复，含单次与平均耗时）、响应状态（`NOERROR` / `SERVFAIL` / `NXDOMAIN` / `TIMEOUT` / `ERROR`）与成功率/失败率统计。所有服务器并发探测，单节点超时不会阻塞其他节点。
  - **Page 2 — 域名解析与 IP Geo**：用每台配置的 DNS 服务器解析选中的域名（A + AAAA 记录），并通过 [ip-api.com](https://ip-api.com) 展示每个结果 IP 的国家、城市与 ISP 归属。
- **四种 DNS 传输协议**（[miekg/dns](https://github.com/miekg/dns) + `net/http`）：
  - `UDP` / `TCP` —— 传统 53 端口
  - `DoT`（DNS over TLS）—— 853 端口，**严格执行 TLS 证书校验**（不提供跳过开关）
  - `DoH`（DNS over HTTPS）—— RFC 8484 报文格式，**强制 HTTP/2**，严格证书校验
- **Geo 查询防护**：进程级内存缓存对同一 IP 去重（缓存不持久化，随进程销毁），Worker Pool 限制并发（每次最多 4 个请求）避免触发 API 限流。
- **独立测试模式**：两页测试完全解耦 —— 可仅测 Page 1、仅测 Page 2，或同时运行。`[R]` / `[S]` 只作用于当前页。
- **IPv4 / IPv6 双栈**：原生支持 IPv6 字面量地址（`[2001:4860:4860::8888]:53`）与 AAAA 查询。
- **JSON 配置驱动**：校验失败给出精确定位；首次启动若配置缺失会自动创建默认配置。
- **自适应布局**：窄终端下自动裁剪低优先级列并截断溢出内容；支持键盘与鼠标滚轮滚动。

## 快速开始

环境要求：Go 1.27+（或任意较新的 Go 工具链）。

```bash
git clone git@github.com:lxlonion/Dnscheck.git
cd Dnscheck
go build -o dnscheck ./cmd/dnscheck
./dnscheck
```

- 无需参数：默认读取 `./config.json`；文件不存在时自动创建默认配置（覆盖四种协议的 Google/Cloudflare 服务器 + 两个探测域名）。
- 指定配置：`./dnscheck -config /path/to/config.json`

## 配置文件

```json
{
  "timeout_ms": 2000,
  "dns_servers": [
    { "name": "Google UDP", "address": "8.8.8.8:53", "protocol": "udp" },
    { "name": "Google TCP", "address": "8.8.8.8:53", "protocol": "tcp" },
    { "name": "Cloudflare DoT", "address": "1.1.1.1:853", "protocol": "dot" },
    { "name": "Google DoH", "address": "https://dns.google/dns-query", "protocol": "doh" },
    { "name": "Google IPv6 UDP", "address": "[2001:4860:4860::8888]:53", "protocol": "udp" }
  ],
  "domains": ["google.com", "github.com"]
}
```

| 字段 | 说明 |
|---|---|
| `timeout_ms` | 全局单次查询超时（毫秒），默认 `2000`。 |
| `dns_servers[].name` | 仪表盘中显示的名称。 |
| `dns_servers[].address` | `udp`/`tcp`/`dot` 用 `host:port`（IPv6 需方括号包裹），`doh` 用 `https://` URL。 |
| `dns_servers[].protocol` | `udp`、`tcp`、`dot`、`doh` 之一。 |
| `domains` | Page 2 解析测试的目标域名列表。 |

非法配置会快速失败并给出精确定位，例如：`invalid config config.json: dns_servers[2].protocol "icmp": must be one of udp, tcp, dot, doh`。

## 按键说明

| 按键 | 功能 |
|---|---|
| `Tab` / `1` / `2` | 切换页面 |
| `R` | 重新触发当前页测试 |
| `S` | 止/启当前页测试 |
| `D` | 在 Page 2 重新打开域名选择器 |
| `↑` `↓` `PgUp` `PgDn` `Home` `End`、鼠标滚轮 | 滚动结果（页脚显示位置） |
| `Q` / `Ctrl+C` | 退出程序 |

首次进入 Page 2 会打开**域名选择器**：`↑`/`↓` 移动、`空格` 勾选/取消、`回车` 开始解析选中域名、`Esc` 取消。

## 项目结构

```
cmd/dnscheck/        入口（参数解析与程序启动）
config/              JSON 配置加载、默认值与校验
dnsclient/           DNS 解析器（UDP/TCP/DoT/DoH）、RTT 探测、域名解析
geo/                 ip-api.com 客户端（内存缓存 + Worker Pool）
tui/                 bubbletea 模型、双页面、域名选择器、自适应表格
```

## 测试

测试完全离线 —— DNS 与 HTTP 端点均为本地 mock（miekg/dns 本地服务器、`httptest` TLS/HTTP2 服务器），`go test` 不访问外网：

```bash
go test ./... -race
go vet ./...
```

## 跨平台构建

```bash
GOOS=darwin  go build -o dnscheck ./cmd/dnscheck
GOOS=linux   go build -o dnscheck ./cmd/dnscheck
GOOS=windows go build -o dnscheck.exe ./cmd/dnscheck
```
