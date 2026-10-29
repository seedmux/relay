# 自己架中继:参数和手动部署

[English](self-hosting.md) | 中文

最简单的装法见 [README](../README.zh-CN.md):一行命令装好,打印出 `ws://IP:8080/` 填进 Seedmux 就行。这一页给想手动部署、或者调参数的人。

## systemd(手动)

安装脚本写的就是这份,不用域名:

```ini
# /etc/systemd/system/seedmux-relay.service
[Unit]
Description=Seedmux relay
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/seedmux-relay -addr :8080 -data-dir /var/lib/seedmux-relay -turn
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

```sh
systemctl daemon-reload && systemctl enable --now seedmux-relay
seedmux-relay -public-ip     # 看这台服务器的公网 IP,地址就是 ws://<它>:8080/
```

用域名和 HTTPS 时,把 `-addr :8080` 换成 `-domain 你的域名`(80 和 443 端口要空着)。

## 参数

| 参数 | 默认 | 说明 |
|---|---|---|
| `-addr` | `:8080` | 中继监听的地址和端口,地址就是 `ws://<IP>:<端口>/` |
| `-data-dir` | (只存内存) | 存数据的目录(电脑的身份记录、TURN 密钥、证书)。开 TURN 或 HTTPS 时必填 |
| `-turn` | 关 | 打开 TURN,监听 3478 |
| `-turn-listen` | | TURN 换个地址,比如 `:3479` |
| `-turn-public-ip` | 自动 | 服务器的公网 IP。不填时:用了 `-domain` 就按域名查,否则问公共 STUN 服务器 |
| `-turn-ports` | `50000-50999` | TURN 中转用的 UDP 端口段 |
| `-turn-ttl` | `12h` | TURN 凭据的有效期 |
| `-turn-max-allocations` | 16 | 每台电脑同时最多开几个 TURN 中转 |
| `-domain` | (关) | 在 443 上提供 HTTPS,自动申请、续期 Let's Encrypt 证书(还要用 80 做验证)。域名要指向这台服务器 |
| `-require-desktop-key` | 关 | 拒绝不带身份密钥的老版本 Seedmux |
| `-max-conns-per-ip` | 64 | 每个 IP 同时最多几条连接,0 = 不限。手机运营商常常很多人共用一个出口 IP,用的人多就调大 |
| `-trust-proxy` | 开 | 放在本机的反向代理后面时,按代理给的 `X-Forwarded-For` 算每 IP 上限 |
| `-public-ip` | | 打印这台服务器的公网 IP 后退出 |
| `-version` | | 打印版本号后退出 |
| `-apns-*` | (关) | 只有官方中继用:iOS 推送的密钥 |

## 数据目录

`-data-dir` 里有:

- `desktop-keys.json`:每台电脑的身份记录。**要备份**:丢了的话,第一个声称是某台电脑的连接就能把它登记成自己的。
- `turn-secret`:TURN 凭据的签名密钥。丢了只会让已经发出去的凭据失效,电脑重连后会拿到新的。
- `certs/`:用 `-domain` 时的 HTTPS 证书,丢了会自动重新申请。

## 防火墙

放行 TCP 8080(或者你设的端口)、UDP 和 TCP 3478、UDP 50000–50999;用域名时再放行 TCP 80 和 443。云服务器还要在服务商的**安全组**里放行 —— 安全组没开 UDP,是 TURN「开了却不通」最常见的原因。

## 检查

- `seedmux-relay -version`:装的版本
- `curl http://<IP>:8080/health` → `{"ok":true}`
- `journalctl -u seedmux-relay` 里有 `TURN enabled`,后面写着用的公网 IP
- 用 iPhone 上的 Seedmux 走流量打开电脑的屏幕:画面左下角的绿字会显示连接状态和收到的帧数
