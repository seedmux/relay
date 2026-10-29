# Seedmux 中继

[English](README.md) | 中文

[Seedmux](https://seedmux.com) 桌面端(和它的 `smx` 守护进程)与 iPhone 上的 Seedmux 不在同一个网络时,靠中继接上。可以自己架一台,把机器指过去;也可以用官方的。

- **只转发,不看内容。** 手机和桌面配对时用 X25519 交换密钥,之后每条消息都端到端加密(XChaCha20-Poly1305)。中继只看得到最外层的信封:谁和谁在说话、说了多少。终端、对话、屏幕的内容它都看不到。
- **内置 TURN。** 手机和桌面直连不上时(移动网络多是对称型 NAT,打洞打不通),看屏幕的 WebRTC 画面经中继的 TURN 转发。凭据短期有效,只发给验证过身份的桌面。
- **推送通知**(只有官方中继有):中继可以替桌面给 Seedmux iOS app 转发 APNs 推送。自己架的中继没有苹果的推送密钥,不要加 `-apns-*` 参数。

## 安装

需要一台有公网 IP 的服务器,和一个 DNS **A 记录**指向它的域名(或子域名)。

**一行命令(Linux,推荐)。** 装好二进制,配好 systemd 服务(自动 HTTPS 证书 + TURN)并启动:

```sh
curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh -s -- --domain relay.example.com
```

**Docker。** 用主机网络,TURN 的端口段就不用一个个映射:

```sh
docker run -d --name seedmux-relay --restart unless-stopped --network host \
  -v seedmux-relay:/data ghcr.io/seedmux/relay -domain relay.example.com -turn
```

**下载二进制。** 在 [Releases](https://github.com/seedmux/relay/releases) 下载对应的 `seedmux-relay-<系统>-<架构>`(Linux、macOS、Windows、FreeBSD 的 amd64 / arm64),用 `SHA256SUMS` 核对,然后:

```sh
seedmux-relay -domain relay.example.com -data-dir /var/lib/seedmux-relay -turn
```

**自己编译**(兜底):Go 1.26 或更新,`go build -o seedmux-relay .`

防火墙和云服务商的安全组里要放行:

| 端口 | 协议 | 用途 |
|---|---|---|
| 80 | TCP | 申请 HTTPS 证书(Let's Encrypt 验证) |
| 443 | TCP | 中继(走 TLS 的 WebSocket) |
| 3478 | UDP 和 TCP | TURN |
| 50000–50999 | UDP | TURN 中转的流量 |

然后把机器指过来:在 Mac 上的 Seedmux 里设置这台机器的中继(主机设置),或者在那台机器的 `smx.toml` 里写 `relay = "wss://relay.example.com/"`。

443 上已经跑着反向代理(Caddy、nginx)?不加 `-domain`,用 `-addr 127.0.0.1:8080` 放在代理后面,TURN 用 `-turn -turn-public-ip <IP>` 给出公网 IP。全部参数见 [docs/self-hosting.zh-CN.md](docs/self-hosting.zh-CN.md)。

## 安全

- **桌面身份「先到先得」。** 桌面连上时第一帧带一把随机的中继密钥,中继按设备号记下它的哈希(`<data-dir>/desktop-keys.json`);之后再有连接说自己是这个设备号,必须拿出同一把。这样别人冒充不了这台桌面,也占不了它的推送和 TURN 额度。没带密钥的老版本桌面照样能连(不算验证过,拿不到 TURN),除非加 `-require-desktop-key`。
- **TURN 进不了内网。** 只许转发到公网单播地址;本机回环、内网(RFC 1918)、链路本地(包括云厂商的元数据地址)、CGNAT、组播、未指定地址一律拒绝。每台桌面同时开的中转有上限。凭据用 TURN REST 的做法:用户名是「到期时间:设备号」,密码是用 `<data-dir>/turn-secret` 里的密钥做的 HMAC-SHA1。
- **防滥用。** 连上后 15 秒内必须完成认证;每个客户端 IP 最多 64 条连接(反向代理和中继在同一台机器上时,按代理给的 `X-Forwarded-For` 算);推送只许桌面发,每台每分钟 60 条,每条不超过 4KB;配对码不写进日志。

发现安全漏洞,请在本仓库的 **Security → Report a vulnerability** 私下报告,不要直接开 issue。

## 协议

[AGPL-3.0](LICENSE)。把改过的中继拿去给别人提供服务,就要向这些用户公开你改过的源码。
