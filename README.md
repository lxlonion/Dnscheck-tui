# Dnscheck

一个跨平台的终端 DNS 诊断工具。它用纯 Go 实现，不依赖 `dig`、`nslookup` 或 `ping`，可在交互式 TUI 中比较多个 DNS 服务器的性能、解析域名，并查看结果 IP 的地理位置与 ISP。

> English documentation is below. [Jump to English](#english)

## 功能概览

- **DNS 性能仪表盘**：并发测试全部已配置的 DNS 服务器；每个节点执行 3 次 A 记录查询，展示单次 RTT、平均 RTT、响应状态与成功率。
- **域名解析与 IP Geo**：按需选择域名，通过每台 DNS 服务器查询 A 和 AAAA 记录，并显示结果 IP 的国家、城市与 ISP。
- **四种 DNS 传输协议**：`UDP`、`TCP`、DoT（DNS over TLS）和 DoH（DNS over HTTPS / RFC 8484）。DoT 和 DoH 使用严格的 TLS 证书校验；DoH 强制使用 HTTP/2。
- **IPv4 与 IPv6**：支持 IPv6 DNS 服务器地址和 AAAA 查询。
- **独立运行**：性能测试与解析查询可以独立开始、重新测试或停止；慢节点不会阻塞其他节点。
- **查询保护**：IP 归属查询通过进程内缓存去重，并限制为最多 4 个并发请求，避免对公共 Geo API 的重复访问。
- **适配终端**：窄窗口会自动隐藏低优先级列、截断长内容；支持键盘和鼠标滚轮滚动。

## 快速开始

需要 Go 1.27 或更高版本。

```bash
git clone git@github.com:lxlonion/Dnscheck.git
cd Dnscheck
go build -o dnscheck ./cmd/dnscheck
./dnscheck
```

如果当前目录没有 `config.json`，首次启动时程序会自动创建一份可直接使用的默认配置。

使用自定义配置：

```bash
./dnscheck -config /path/to/config.json
```

也可从项目的 [Releases](https://github.com/lxlonion/Dnscheck/releases) 下载适配平台的预编译版本。

## 使用方式

启动后会进入 DNS 性能页，并自动开始测试。切换到“域名解析与 IP Geo”页后，先在域名选择器中勾选目标，再按 Enter 开始查询。

| 按键 | 操作 |
| --- | --- |
| `Tab` / `1` / `2` | 切换页面 |
| `R` | 重新运行当前页面的测试 |
| `S` | 停止或继续当前页面的测试 |
| `D` | 在解析页重新打开域名选择器 |
| `↑` / `↓` / `PgUp` / `PgDn` / `Home` / `End` | 滚动结果 |
| 鼠标滚轮 | 滚动结果 |
| `Q` / `Ctrl+C` | 退出 |

在域名选择器中：`↑` / `↓` 移动，`Space` 勾选或取消，`Enter` 确认并开始，`Esc` 关闭选择器。

## 配置

配置文件为 JSON，包含查询超时、DNS 服务器和待解析域名：

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
| --- | --- |
| `timeout_ms` | 单次 DNS 查询超时，单位为毫秒；默认值为 `2000`。 |
| `dns_servers[].name` | 界面中显示的服务器名称。 |
| `dns_servers[].address` | `udp`、`tcp`、`dot` 使用 `host:port`；IPv6 地址必须加方括号。`doh` 使用 `https://` 地址。 |
| `dns_servers[].protocol` | DNS 协议：`udp`、`tcp`、`dot` 或 `doh`。 |
| `domains` | 解析页可选择的域名列表。 |

程序会在启动时校验配置；格式错误、空字段或不支持的协议会给出带字段位置的错误信息。

## 开发与测试

测试使用本地 DNS 和 HTTP mock，不会访问真实 DNS 服务或 Geo API：

```bash
go test ./... -race
go vet ./...
```

手动交叉编译示例：

```bash
GOOS=darwin  GOARCH=arm64 go build -o dnscheck ./cmd/dnscheck
GOOS=linux   GOARCH=amd64 go build -o dnscheck ./cmd/dnscheck
GOOS=windows GOARCH=amd64 go build -o dnscheck.exe ./cmd/dnscheck
```

推送形如 `v1.0.0` 的 Git 标签会触发 GitHub Actions，为 macOS、Linux 和 Windows 构建可执行文件和校验和，并创建 Release。

## 项目结构

```text
cmd/dnscheck/  程序入口与命令行参数
config/        JSON 配置、默认值与校验
dnsclient/     UDP、TCP、DoT、DoH 解析与性能探测
geo/           IP 地理位置查询、缓存与并发控制
tui/           Bubble Tea 终端界面、表格与交互
```

---

<a id="english"></a>

# Dnscheck

A cross-platform terminal DNS diagnostic tool written in pure Go. It requires no `dig`, `nslookup`, or `ping`, and provides an interactive TUI for comparing DNS server performance, resolving domains, and looking up the location and ISP of returned IP addresses.

## Highlights

- **DNS performance dashboard** — concurrently probes every configured server with three A-record queries and shows individual RTTs, average RTT, response status, and success rate.
- **Domain resolution and IP Geo** — select domains on demand, resolve A and AAAA records through each DNS server, then view each returned IP's country, city, and ISP.
- **Four transports** — `UDP`, `TCP`, DoT (DNS over TLS), and DoH (DNS over HTTPS / RFC 8484). DoT and DoH use strict TLS certificate validation; DoH enforces HTTP/2.
- **IPv4 and IPv6** — supports IPv6 resolver addresses and AAAA lookups.
- **Independent pages** — performance tests and resolution lookups can be started, rerun, or stopped independently; a slow resolver does not block the rest.
- **Lookup safeguards** — IP Geo results are deduplicated in a process-local cache, with at most four concurrent API requests.
- **Responsive terminal layout** — lower-priority columns collapse in narrow terminals, with keyboard and mouse-wheel scrolling.

## Quick start

Go 1.27 or later is required.

```bash
git clone git@github.com:lxlonion/Dnscheck.git
cd Dnscheck
go build -o dnscheck ./cmd/dnscheck
./dnscheck
```

If `config.json` does not exist in the current directory, dnscheck creates a ready-to-use default configuration on first launch.

Use another configuration file:

```bash
./dnscheck -config /path/to/config.json
```

Prebuilt binaries are available from [Releases](https://github.com/lxlonion/Dnscheck/releases).

## Controls

The DNS performance page starts automatically. On the **Domain Resolution & IP Geo** page, select the domains you want to test and press Enter to begin.

| Key | Action |
| --- | --- |
| `Tab` / `1` / `2` | Switch pages |
| `R` | Rerun the active page |
| `S` | Stop or resume the active page |
| `D` | Reopen the domain picker on the resolution page |
| `↑` / `↓` / `PgUp` / `PgDn` / `Home` / `End` | Scroll results |
| Mouse wheel | Scroll results |
| `Q` / `Ctrl+C` | Quit |

In the domain picker, use `↑` / `↓` to move, `Space` to toggle a domain, `Enter` to confirm, and `Esc` to close it.

## Configuration

dnscheck reads a JSON file containing the query timeout, DNS servers, and domains:

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

| Field | Description |
| --- | --- |
| `timeout_ms` | Timeout for each DNS exchange in milliseconds; defaults to `2000`. |
| `dns_servers[].name` | Server label displayed in the UI. |
| `dns_servers[].address` | `host:port` for `udp`, `tcp`, and `dot`; bracket IPv6 literals. Use an `https://` URL for `doh`. |
| `dns_servers[].protocol` | One of `udp`, `tcp`, `dot`, or `doh`. |
| `domains` | Domains available for selection on the resolution page. |

Configuration is validated at startup. Invalid formats, missing fields, and unsupported protocols produce actionable errors with their field locations.

## Development

The test suite uses local DNS and HTTP mocks; it does not call public DNS resolvers or the Geo API:

```bash
go test ./... -race
go vet ./...
```

Example cross-compiles:

```bash
GOOS=darwin  GOARCH=arm64 go build -o dnscheck ./cmd/dnscheck
GOOS=linux   GOARCH=amd64 go build -o dnscheck ./cmd/dnscheck
GOOS=windows GOARCH=amd64 go build -o dnscheck.exe ./cmd/dnscheck
```

Pushing a Git tag such as `v1.0.0` triggers GitHub Actions to build macOS, Linux, and Windows binaries with checksums, then publish a GitHub Release.

## Project layout

```text
cmd/dnscheck/  Program entry point and command-line flags
config/        JSON configuration, defaults, and validation
dnsclient/     UDP, TCP, DoT, DoH resolution and performance probing
geo/           IP geolocation client, cache, and concurrency control
tui/           Bubble Tea terminal UI, tables, and interactions
```
