# Seedmux 中继

[English](README.md) | 中文

手机和电脑不在同一个网络时,[Seedmux](https://seedmux.com) 靠中继把它们接上。可以用官方的,也可以自己架一台。

- **只转发,看不到内容**:手机和电脑之间的消息端到端加密,中继只负责转交,看不到终端、对话和屏幕。
- **内置 TURN**:手机走流量看电脑屏幕时,直连打不通就经中继转发画面。

## 快速安装(Linux)

在服务器上运行:

```sh
curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh
```

它会下载程序、装成开机自启的服务、打开 TURN,最后打印出中继地址,形如:

```
Relay address to put in Seedmux:  ws://203.0.113.7:8080/
```

把这个地址填到 Seedmux 里这台电脑的中继设置;或者写进那台电脑的 `smx.toml`:`relay = "ws://203.0.113.7:8080/"`。

防火墙和云服务商的安全组里放行这些端口:

| 端口 | 协议 | 用途 |
|---|---|---|
| 8080 | TCP | 中继 |
| 3478 | UDP 和 TCP | TURN |
| 50000–50999 | UDP | TURN 中转的流量 |

安装脚本的选项(加在 `sudo sh -s --` 后面):`--port 9000` 换端口,`--no-turn` 不开 TURN。

## 自己编译

需要 Go 1.26 或更新:

```sh
git clone https://github.com/seedmux/relay && cd relay
go build -o seedmux-relay .
./seedmux-relay -addr :8080 -data-dir ./data -turn
```

macOS、Windows、FreeBSD 也可以直接在 [Releases](https://github.com/seedmux/relay/releases) 下载编好的程序,运行同样的命令。

## 可选

- **用域名和 HTTPS**:有域名并且 DNS 指向这台服务器时,安装加 `--domain 你的域名`(或者运行时加 `-domain 你的域名`),中继自动申请证书,在 443 上提供 `wss://你的域名/`,要放行 TCP 80 和 443。
- **Docker**:`docker run -d --restart unless-stopped --network host -v seedmux-relay:/data ghcr.io/seedmux/relay -addr :8080 -turn`
- **全部参数、systemd 配置**:[docs/self-hosting.zh-CN.md](docs/self-hosting.zh-CN.md)

## 安全

- **桌面身份**:电脑第一次连上时带一把自己的密钥,中继记下来;之后别人冒充这台电脑会被拒绝。
- **TURN 进不了内网**:只转发到公网地址,本机、内网、云厂商的元数据地址一律拒绝;每台电脑同时开的中转有上限。
- **防滥用**:连上后 15 秒内必须认证;每个 IP 最多 64 条连接;配对码不写进日志。
- **`ws://` 和 `wss://`**:内容都是端到端加密的。区别是用 `ws://` 时,电脑的身份密钥在网络上明文传输,能看到你流量的人可以拿它冒充这台电脑、让它连不上中继(仍然看不到内容)。在乎这一点就用域名开 HTTPS。

发现安全漏洞,请在本仓库 **Security → Report a vulnerability** 私下报告,不要开 issue。

## 协议

[AGPL-3.0](LICENSE)。把改过的中继拿去给别人提供服务,就要向这些用户公开你改过的源码。
