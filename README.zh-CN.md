# Seedmux 中继

[English](README.md) | 中文

手机和电脑不在同一个网络时,[Seedmux](https://seedmux.com) 靠中继把它们接上。可以用官方中继,也可以在自己的服务器上架一台。

- **只转发,看不到内容**:手机和电脑之间的终端、对话、屏幕画面都是端到端加密的,中继只负责转交。
- **内置 TURN**:手机用流量看电脑屏幕、直连打不通时,画面经中继转发。

## 两种模式

| | `ws://IP` | `wss://域名` |
|---|---|---|
| 需要 | 一台有公网 IP 的 Linux 服务器 | 再加一个域名,A 记录指向这台服务器 |
| 填进 Seedmux 的地址 | `ws://服务器IP:9876/` | `wss://域名/` |
| 中继用的端口 | TCP 9876 | TCP 443,外加 TCP 80(申请证书用) |
| 证书 | 不需要 | 自动申请、自动续期 |
| 手机、电脑到中继这一段 | 不加密(内容本身已经端到端加密) | TLS 加密 |

两种模式都带 TURN,另外用 UDP 和 TCP 3478、UDP 50000–50999。

**怎么选**:没有域名就用 `ws://IP`,装好就能用。有域名就用 `wss://域名`:443 端口几乎哪里都放行,而有些公司网络只放行 443,这时 `wss://` 更稳。

**服务器在中国大陆**:云厂商会对 80、443、8080 这类常用网页端口查备案,没备案的域名会被拦。所以中继默认用 9876 端口。`wss://域名` 模式要用 80 和 443,域名得先备案;没备案就用 `ws://IP`,或者换香港、海外的服务器。

## 安装

在服务器上运行一条命令,不用回答任何问题。它会装成开机自启的服务、打开 TURN;服务器上开着 ufw 或 firewalld 的话,端口也一并放行。装完打印出要填进 Seedmux 的地址。

### `ws://IP`

```sh
curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh
```

装完显示(IP 是脚本自己查出来的):

```
  Put this address in Seedmux:  ws://203.0.113.7:9876/
```

### `wss://域名`

1. 在域名服务商那里加一条 A 记录,指向这台服务器的公网 IP。用 Cloudflare 这类 CDN 的,要关掉代理(只做 DNS 解析),不然 TURN 连不上。
2. 运行下面的命令,把最后的 `你的域名` 换成你的真实域名:

```sh
curl -fsSL https://github.com/seedmux/relay/releases/latest/download/install.sh | sudo sh -s -- --domain 你的域名
```

脚本会先检查域名是不是指向这台服务器,不对就停下来告诉你哪里不对;对了就申请证书,最后显示:

```
  Put this address in Seedmux:  wss://你的域名/
```

80 或 443 已经被 nginx、Caddy 占着,不用停掉它们,让它们把流量转给中继就行,见[放在 nginx 或 Caddy 后面](docs/self-hosting.zh-CN.md#放在-nginx-或-caddy-后面)。

### 云服务器:安全组

腾讯云、阿里云、AWS 这类云服务器,控制台里还有一层**安全组**,安装脚本改不到,要在网页上放行上面的端口。UDP 别漏:安全组没放行 UDP,是 TURN「开了却不通」最常见的原因。

## 填进 Seedmux

都在 Mac 上 Seedmux 的设置里填:

- **这台 Mac**:设置 → 手机 → 中继地址,粘贴地址后回车,再点「重启」。
- **手机**:不用动。它下次连上这台 Mac 时会自动换成新地址;要是手机已经连不上了,在手机上重新扫一次配对码。
- **Seedmux 管理的远程主机**(Linux 服务器等):设置 → 远程主机 → 选中那台主机 → 中继地址,填好点「切换」。

想换回官方中继:把中继地址清空(远程主机点「改回官方中继」)。

## 检查

- 浏览器打开 `http://服务器IP:9876/health`(域名模式打开 `https://你的域名/health`),显示 `{"ok":true}` 就说明中继在运行。
- 手机关掉 Wi-Fi、用流量,在 Seedmux 里打开电脑的屏幕:画面左下角的绿字会显示连接状态和收到的帧数。

## 自己编译

没有 Linux 服务器,或者不想用安装脚本时用。需要 Go 1.26 或更新:

```sh
git clone https://github.com/seedmux/relay && cd relay
go build -o seedmux-relay .
./seedmux-relay -addr :9876 -data-dir ./data -turn
```

这样就是 `ws://IP:9876/`。要用域名,把 `-addr :9876` 换成 `-domain 你的域名`。

macOS、Windows、FreeBSD 也可以在 [Releases](https://github.com/seedmux/relay/releases) 直接下载编好的程序,参数一样。Docker:

```sh
docker run -d --restart unless-stopped --network host -v seedmux-relay:/data ghcr.io/seedmux/relay -addr :9876 -turn
```

手动配 systemd、全部参数、排查问题,见 [docs/self-hosting.zh-CN.md](docs/self-hosting.zh-CN.md)。

## 安全

- **端到端加密**:手机和电脑之间的内容只在两端加密、解密,中继、机房和网络上的人都看不到。`ws://` 和 `wss://` 在这一点上一样。屏幕画面走 WebRTC,经 TURN 转发时也是加密的。
- **`ws://` 和 `wss://` 的差别只在到中继的这一段**:用 `ws://` 时,网络上的人能看到哪些设备连着这个中继,也能看到电脑登录中继用的密钥;拿到这把密钥可以冒充这台电脑占住中继、让它连不上,但看不到任何内容。在意这一点就用 `wss://`。
- **电脑身份**:电脑第一次连上时登记自己的密钥,之后冒充它的连接一律拒绝。
- **TURN 进不了内网**:只往公网地址转发,本机、内网、云厂商的元数据地址一律拒绝;每台电脑同时开的中转有上限。
- **防滥用**:连上后 15 秒内必须认证;每个 IP 最多 64 条连接;配对码不写进日志。

发现安全漏洞,请在本仓库 **Security → Report a vulnerability** 私下报告,不要开 issue。

## 协议

[AGPL-3.0](LICENSE)。把改过的中继拿去给别人提供服务,就要向这些用户公开你改过的源码。
