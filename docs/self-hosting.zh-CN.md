# 自己架中继

[English](self-hosting.md) | 中文

最省事的装法(一行命令、Docker、现成的二进制)见 [README](../README.zh-CN.md#安装)。这一页讲手动运行。

## 自己编译

需要 Go 1.26 或更新的版本。

```sh
git clone https://github.com/seedmux/relay && cd relay
go build -o seedmux-relay .
# 在 Mac 上给 Linux 服务器交叉编译:
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o seedmux-relay .
```

## systemd

带自动 HTTPS 的(一行命令安装时写的就是这份):

```ini
# /etc/systemd/system/seedmux-relay.service
[Unit]
Description=Seedmux relay
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/seedmux-relay -domain relay.example.com -data-dir /var/lib/seedmux-relay -turn
DynamicUser=yes
StateDirectory=seedmux-relay
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
Restart=always
RestartSec=2
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

放在自己的反向代理后面的:

```ini
# /etc/systemd/system/seedmux-relay.service
[Unit]
Description=Seedmux relay
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/seedmux-relay -addr 127.0.0.1:8080 -data-dir /var/lib/seedmux-relay \
  -turn-listen :3478 -turn-public-ip 203.0.113.7 -turn-host relay.example.com
DynamicUser=yes
StateDirectory=seedmux-relay
Restart=always
RestartSec=2
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

```sh
systemctl daemon-reload && systemctl enable --now seedmux-relay
```

`<data-dir>` 里有 `desktop-keys.json`、`turn-secret`,用了 `-domain` 还有 `certs/`(HTTPS 证书,丢了会自动重新申请)。前两样记得备份:

- `desktop-keys.json`:登记过的桌面。丢了的话,下一个声称是某个设备号的连接就能把它登记成自己的。
- `turn-secret`:TURN 凭据的签名密钥。丢了只会让已经发出去的凭据失效,桌面重连后会拿到新的。

## 参数

| 参数 | 默认 | 说明 |
|---|---|---|
| `-domain` | (关) | 在 443 上提供 HTTPS,自动申请、续期 Let's Encrypt 证书(还会监听 80 做验证)。域名要指向这台服务器 |
| `-addr` | `:8080` | 不带 TLS 的 WebSocket 监听地址。放在反向代理后面时只监听本机回环;用了 `-domain` 时,显式给了才监听 |
| `-turn` | 关 | 在 3478 上开 TURN(等于 `-turn-listen :3478`) |
| `-data-dir` | (只存内存) | 持久化目录。开 TURN 时必填 |
| `-require-desktop-key` | 关 | 拒绝不带中继密钥的桌面(老版本 Seedmux) |
| `-max-conns-per-ip` | 64 | 每个客户端 IP 同时最多几条连接,0 = 不限。手机运营商常常很多人共用一个出口 IP,用户多的话调大 |
| `-trust-proxy` | 开 | 反向代理在本机回环上时,按它给的 `X-Forwarded-For` 算每 IP 上限 |
| `-turn-listen` | (关) | TURN 的 UDP 和 TCP 监听地址,比如 `:3478` |
| `-turn-public-ip` | 从 `-domain` 解析 | 服务器的公网 IP;不给就按 `-domain` 的 DNS 记录查 |
| `-turn-host` | `-domain` 或 IP | 写进 TURN 地址里的主机名 |
| `-turn-ports` | `50000-50999` | TURN 中转用的 UDP 端口段 |
| `-turn-ttl` | `12h` | TURN 凭据的有效期 |
| `-turn-max-allocations` | 16 | 每台桌面同时最多开几个 TURN 中转 |
| `-apns-*` | (关) | 只有官方中继用:Seedmux iOS app 的 APNs 推送密钥 |
| `-version` | | 打印版本号后退出 |

## 防火墙

放行 TCP 80 和 443、TURN 端口(3478)的 UDP 和 TCP、TURN 中转用的 UDP 端口段(50000–50999)。云服务器还要在服务商控制台的安全组里放行:安全组没开 UDP,是 TURN「看着开了、其实不通」最常见的原因。

## 检查

- `seedmux-relay -version` 打印装的版本
- `curl https://relay.example.com/health` → `{"ok":true}`
- `curl -i https://relay.example.com/` → `426 Upgrade Required`(它只说 WebSocket)
- 中继启动日志里有 `TURN enabled` 一行
- 用 iPhone 上的 Seedmux 走手机流量打开一台 Mac 的屏幕:画面左下角的绿色调试行会显示连接状态和收到的帧数
