# CodingFire

把 AI 编程烧掉的 token 变成桌面上的一把像素篝火 —— 烧得越快，火越旺。

[![Release](https://img.shields.io/github/v/release/wangsrGit119/codingfire)](../../releases)
[![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux%20%7C%20macOS-lightgrey)](#系统要求)
[![CI](https://github.com/wangsrGit119/codingfire/actions/workflows/ci.yml/badge.svg)](../../actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)

[English](README.md) · **简体中文**

<p align="center">
  <img src="assets/example_01.gif" width="344" alt="CodingFire —— 偏绿的火苗">
  <img src="assets/example_02.gif" width="344" alt="CodingFire —— 经典橙色火苗">
</p>

## 功能

- **火势即当前消耗速率。** 多个 AI 工具同时跑也只有一把火 —— 速率加总。
- **鼠标悬停在火上**，显示当日用量、实时 tok/s 和当前档位。
- **右键或双击托盘图标**打开控制台：统计（当日总量、逐小时时间线、按来源拆分、峰值速率）、数据源、设置、关于。
- **常驻顶层，默认开启鼠标穿透**，免得篝火把本该点给下面桌面的点击吃掉。在火上按住左键即可拖动。
- **随桌面启动**，不想要就在菜单里关掉。
- **四种界面语言** —— 英文、简体中文、日本語、한국어，也可以跟随系统。
- **只读、离线。** 不联网、不上传、无遥测；不读 prompt、代码或文件内容。
- **占用低。** 火焰隐藏后不再重绘，兜底扫描每 4 秒一次，工作集约 33 MiB。
- **单个静态可执行文件。** 不需要 .NET、不引入 DLL、没有安装过程、不需要管理员权限。

## 系统要求

| 系统 | 需要什么 |
|---|---|
| Windows 11 / 10（64 位） | 无 |
| Linux（x64、arm64） | X11 会话 + 合成器；托盘还需要一个 StatusNotifier 宿主 |
| macOS（Intel、Apple 芯片） | 除了首次放行 Gatekeeper 之外，无 |

到 [Releases](../../releases) 下载对应平台的 zip，解压直接运行 —— 每个包里只有一个文件。
首次启动没有数据时火保持「余烬」状态，属正常。

Windows 7、Windows 8 以及任何 32 位 Windows，请改用
[C# 版](https://github.com/wangsrGit119/codingfire-win)。

**各平台都还没做**逐像素穿透：go-gui 把窗口当成一整块画面，所以穿透只能是全有或全无。
Linux 没有合成器会画在不透明矩形上；macOS 是未签名包，首次启动会被 Gatekeeper 拦下，
需要在「系统设置 › 隐私与安全性」里放行一次。`windows/arm64` 会构建发布但从未实机跑过。

## 支持的数据源

**23 个只读本地数据源（22 种工具）。** 不联网、不上传，不读 prompt 或源文件 ——
只取 token 数字与文件路径。

- Claude Code、Codex、Grok、Pi、Amp
- WorkBuddy、CodeBuddy、Qoder、Qwen Code、Kimi、GitHub Copilot CLI、ZCode、OpenCode、
  Gemini CLI、Droid、Cline / Roo Code / Kilo Code、DeepSeek Harness、Command Code、
  OpenClaw、Every Code

WorkBuddy 的国内版与国外版是两份独立安装、两个 home 目录，因此**分开统计** ——
这就是「23 个数据源、22 种工具」的来历。每个来源都支持环境变量覆盖日志根目录
（`CLAUDE_CONFIG_DIR`、`CODEX_HOME`、`WORKBUDDY_HOME` …），详见
`internal/data/adapters*.go`。

只计真实计费 token，缓存读/写单列，`reasoning` 不重复计；累计型来源按「同 id 见过的
最大总量」取差额，重启不重复计数。**刻意不采集**：Cursor、Kiro、Antigravity、QwenWork、
Trae，以及若干日志结构未能核实的工具。

## 数据目录

| 系统 | 目录 |
|---|---|
| Windows | `%APPDATA%\CodingFire\` |
| Linux | `$XDG_CONFIG_HOME/CodingFire/`（一般是 `~/.config/CodingFire/`） |
| macOS | `~/Library/Application Support/CodingFire/` |

这里就是 [C# 版](https://github.com/wangsrGit119/codingfire-win) 用的那个目录，所以两版可以
随意切换，但同一时刻只能跑一个。设 `CODINGFIRE_DATA_DIR` 可改用便携目录。此目录之外不写
任何东西 —— 只有开关打开时的那条开机自启项。

`CodingFire --dump 报告.txt` 可把同样的统计导出成纯文本。

## 从源码构建

需要 **Go 1.26 或更高**。Windows 上不需要 C 编译器；macOS 需要 Xcode 命令行工具，
因为 Metal 后端是 cgo。

```bash
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o dist/CodingFire .   # Windows、Linux
CGO_ENABLED=1 go build -trimpath -ldflags '-s -w' -o dist/CodingFire .   # macOS
go vet ./... && go test ./...
```

版本号只有一处出处：`internal/core/version.go`。推一个 tag，
[CI](../../actions/workflows/release.yml) 就会构建、打包并发布全部六个目标 —— 版本号与 tag
不一致会直接中止，任何一个平台编不过则**在往 release 挂任何东西之前**失败。Windows 那一半
也可以在本地用 `build.ps1` / `release.ps1` 做。

`third_party/go-gui` 与 `third_party/go-glyph` 是打过补丁的 fork，通过 `go.mod` 的
`replace` 指向本地目录，**故意提交进仓库**，这样 clone 下来不用联网就能构建。
**上游升级后每一处都要重新检查。**

## 致谢

本项目是 macOS 版 **[TinyFire](https://github.com/wdkwdkwdk/tinyfire)**（作者
[@wdkwdkwdk](https://github.com/wdkwdkwdk)，MIT 协议）的重写版，并移植到 Windows、
Linux 与 macOS。数据源口径对齐
[juejin-cn/juejin-usage](https://github.com/juejin-cn/juejin-usage)。桌面窗口、托盘与
控制台基于 [go-gui](https://github.com/go-gui-org/go-gui) 构建。

## 协议

MIT —— 详见 [LICENSE](./LICENSE)。
