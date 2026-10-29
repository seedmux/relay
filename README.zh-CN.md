# Seedmux 中继

[English](README.md) | 中文

[Seedmux](https://seedmux.com) 桌面端(和它的 `smx` 守护进程)与 iPhone 上的 Seedmux 不在同一个网络时,靠中继接上。可以自己架一台,把机器指过去;也可以用官方的。

- **只转发,不看内容。** 手机和桌面配对时用 X25519 交换密钥,之后每条消息都端到端加密(XChaCha20-Poly1305)。中继只看得到最外层的信封:谁和谁在说话、说了多少。终端、对话、屏幕的内容它都看不到。
- **内置 TURN。** 手机和桌面直连不上时(移动网络多是对称型 NAT,打洞打不通),看屏幕的 WebRTC 画面经中继的 TURN 转发。凭据短期有效,只发给验证过身份的桌面。
- **推送通知**(只有官方中继有):中继可以替桌面给 Seedmux iOS app 转发 APNs 推送。自己架的中继没有苹果的推送密钥,不要加 `-apns-*` 参数。

## 快速上手

```sh
go build -o seedmux-relay .
./seedmux-relay -addr 127.0.0.1:8080 -data-dir ./data
```

前面放一个做 TLS 的反向代理(WebSocket 升级原样转过去就行),比如 Caddy:

```caddy
relay.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

然后把机器指过来:在 Mac 上的 Seedmux 里设置这台机器的中继(主机设置),或者在那台机器的 `smx.toml` 里写 `relay = "wss://relay.example.com/"`。

要让手机走流量时也能看屏幕,打开 TURN:

```sh
./seedmux-relay -addr 127.0.0.1:8080 -data-dir /var/lib/seedmux-relay \
  -turn-listen :3478 -turn-public-ip 203.0.113.7 -turn-host relay.example.com
```

要对公网开放的端口:

| 端口 | 协议 | 用途 |
|---|---|---|
| 443(你的反向代理) | TCP | 走 TLS 的 WebSocket |
| `-turn-listen`(比如 3478) | UDP 和 TCP | TURN |
| `-turn-ports`(默认 50000–50999) | UDP | TURN 中转的流量 |

systemd 配置和全部参数见 [docs/self-hosting.zh-CN.md](docs/self-hosting.zh-CN.md)。

## 安全

- **桌面身份「先到先得」。** 桌面连上时第一帧带一把随机的中继密钥,中继按设备号记下它的哈希(`<data-dir>/desktop-keys.json`);之后再有连接说自己是这个设备号,必须拿出同一把。这样别人冒充不了这台桌面,也占不了它的推送和 TURN 额度。没带密钥的老版本桌面照样能连(不算验证过,拿不到 TURN),除非加 `-require-desktop-key`。
- **TURN 进不了内网。** 只许转发到公网单播地址;本机回环、内网(RFC 1918)、链路本地(包括云厂商的元数据地址)、CGNAT、组播、未指定地址一律拒绝。每台桌面同时开的中转有上限。凭据用 TURN REST 的做法:用户名是「到期时间:设备号」,密码是用 `<data-dir>/turn-secret` 里的密钥做的 HMAC-SHA1。
- **防滥用。** 连上后 15 秒内必须完成认证;每个客户端 IP 最多 64 条连接(反向代理和中继在同一台机器上时,按代理给的 `X-Forwarded-For` 算);推送只许桌面发,每台每分钟 60 条,每条不超过 4KB;配对码不写进日志。

发现安全漏洞,请在本仓库的 **Security → Report a vulnerability** 私下报告,不要直接开 issue。

## 协议

[AGPL-3.0](LICENSE)。把改过的中继拿去给别人提供服务,就要向这些用户公开你改过的源码。
