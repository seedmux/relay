# 自己架中继:进阶

[English](self-hosting.md) | 中文

一般用 [README](../README.zh-CN.md) 里的一条命令装就够了。这一页写给想知道安装脚本做了什么、已经有网站占着 443、要手动部署或者排查问题的人。

## 安装脚本做了什么

1. 按系统和 CPU 下载对应的程序,核对 `SHA256SUMS` 校验和,放到 `/usr/local/bin/seedmux-relay`。
2. 用域名时,检查域名是不是指向这台服务器、80 和 443 有没有被别的程序占用;不用域名时,检查 9876 有没有被占用。开 TURN 时还检查 3478。
3. 写 systemd 服务 `/etc/systemd/system/seedmux-relay.service`,开机自启。数据放在 `/var/lib/seedmux-relay`。
4. ufw 或 firewalld 开着的话,放行要用的端口。
5. 等中继启动起来;用域名时顺便申请证书。启动失败就打印日志。
6. 打印要填进 Seedmux 的地址和要用的端口。

再运行一次就是升级到最新版,配置按这次的选项重写。

选项加在 `sudo sh -s --` 后面:

| 选项 | 作用 |
|---|---|
| `--domain 你的域名` | 用 `wss://域名/`,不加就是 `ws://IP:9876/` |
| `--port 9000` | `ws://` 换个端口(默认 9876) |
| `--no-turn` | 不开 TURN(那样手机只能在同一个网络里看电脑屏幕) |
| `--no-firewall` | 不动 ufw、firewalld |
| `--no-service` | 只装程序,不装服务 |
| `--version v0.1.0` | 装指定版本 |

## 放在 nginx 或 Caddy 后面

服务器上已经有网站占着 443 时,用默认方式(不加 `--domain`)安装中继,再让 nginx 或 Caddy 把一个域名转给它。Seedmux 里填 `wss://你的域名/`,安全组里不用放行 9876。

Caddy(会自动申请证书):

```
你的域名 {
	reverse_proxy 127.0.0.1:9876
}
```

nginx(证书沿用你现在的配置):

```nginx
server {
    listen 443 ssl;
    server_name 你的域名;
    # ssl_certificate、ssl_certificate_key 照你现有的写

    location / {
        proxy_pass http://127.0.0.1:9876;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 1h;
    }
}
```

`X-Forwarded-For` 那一行别漏:不然所有连接看起来都来自本机,会一起撞上「每个 IP 最多 64 条连接」的上限。

## 手动部署(systemd)

和安装脚本写的一样。`ws://IP` 模式:

```ini
# /etc/systemd/system/seedmux-relay.service
[Unit]
Description=Seedmux relay
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/seedmux-relay -addr :9876 -data-dir /var/lib/seedmux-relay -turn
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

`wss://域名` 模式把 `ExecStart` 改成:

```ini
ExecStart=/usr/local/bin/seedmux-relay -domain 你的域名 -data-dir /var/lib/seedmux-relay -turn
```

然后:

```sh
systemctl daemon-reload && systemctl enable --now seedmux-relay
```

想再加固、写了 `RestrictAddressFamilies=` 的话,除了 `AF_INET AF_INET6` 还要带上 `AF_NETLINK`:TURN 启动时要列网卡,
少了它会报 `netlinkrib: address family not supported by protocol` 然后起不来。

## 参数

| 参数 | 默认 | 说明 |
|---|---|---|
| `-addr` | `:9876` | `ws://` 监听的地址和端口。用了 `-domain` 时,只有明确写了才另外监听 |
| `-domain` | (不用) | 在 443 上提供 `wss://`,自动申请、续期 Let's Encrypt 证书(还要用 80 做验证)。域名要指向这台服务器 |
| `-data-dir` | (只存内存) | 存数据的目录(电脑的身份记录、TURN 密钥、证书)。开 TURN 或用域名时必填 |
| `-turn` | 关 | 打开 TURN,监听 3478 |
| `-turn-listen` | | TURN 换个地址,比如 `:3479` |
| `-turn-public-ip` | 自动 | 服务器的公网 IP。不填时:用了 `-domain` 就按域名查,否则问公共 STUN 服务器 |
| `-turn-ports` | `50000-50999` | TURN 中转用的 UDP 端口段 |
| `-turn-ttl` | `12h` | TURN 凭据的有效期 |
| `-turn-max-allocations` | 16 | 每台电脑同时最多开几个 TURN 中转 |
| `-require-desktop-key` | 关 | 拒绝不带身份密钥的老版本 Seedmux |
| `-max-conns-per-ip` | 64 | 每个 IP 同时最多几条连接,0 = 不限。手机运营商常常很多人共用一个出口 IP,用的人多就调大 |
| `-trust-proxy` | 开 | 放在本机的 nginx、Caddy 后面时,按它们给的 `X-Forwarded-For` 算每个 IP 的上限 |
| `-public-ip` | | 打印这台服务器的公网 IP 后退出 |
| `-version` | | 打印版本号后退出 |
| `-apns-*` | (不用) | 只有官方中继用:iOS 推送的密钥 |

## 端口

| 端口 | 协议 | 用途 | 什么时候要 |
|---|---|---|---|
| 9876 | TCP | 中继 | `ws://IP` 模式 |
| 443 | TCP | 中继 | `wss://域名` 模式 |
| 80 | TCP | 申请、续期证书 | `wss://域名` 模式 |
| 3478 | UDP 和 TCP | TURN | 开 TURN 时 |
| 50000–50999 | UDP | TURN 中转的流量 | 开 TURN 时 |

## 数据目录

`-data-dir`(安装脚本用的是 `/var/lib/seedmux-relay`)里有:

- `desktop-keys.json`:每台电脑的身份记录。**要备份**:丢了的话,第一个声称是某台电脑的连接就能把它登记成自己的。
- `turn-secret`:TURN 凭据的签名密钥。丢了只会让已经发出去的凭据失效,电脑重连后会拿到新的。
- `certs/`:用 `-domain` 时的证书,丢了会自动重新申请。

## 排查

- **安装时说域名没指向这台服务器**:去域名服务商那里把 A 记录改成脚本说的 IP,等几分钟生效再装。用 CDN 的把代理关掉。
- **安装时说没拿到证书**:Let's Encrypt 要从外面连这台服务器的 80 端口,去安全组放行 TCP 80 和 443。放行后不用重装,下次有连接时会自动再申请。
- **电脑连不上中继**:浏览器打开 `/health` 那个地址(见 README 的「检查」)。打不开就是端口没放行,先看安全组。
- **手机用流量看屏幕连不上、在 Wi-Fi 下正常**:多半是安全组没放行 UDP 3478 和 UDP 50000–50999。
- **日志**:`journalctl -u seedmux-relay -f`。启动时有一行 `TURN enabled`,后面写着用的公网 IP。
