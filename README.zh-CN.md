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

几点平台说明。Linux 的透明窗口依赖合成器 —— 没有合成器会画在不透明矩形上；没有
StatusNotifier 宿主（GNOME 需要装扩展）就没有菜单可以退出。macOS 发布的是未签名包，
首次启动会被 Gatekeeper 拦下，需要在「系统设置 › 隐私与安全性」里放行一次；程序常驻
菜单栏，没有 Dock 图标。**各平台都还没做**逐像素穿透：go-gui 把窗口当成一整块画面，
所以穿透只能是全有或全无。Windows 是打磨最充分的目标平台，`windows/arm64` 会构建发布
但从未实机跑过。

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

## 从源码构建

需要 **Go 1.26 或更高**。Windows 上不需要 C 编译器；macOS 需要 Xcode 命令行工具，
因为 Metal 后端是 cgo。

```bash
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o dist/CodingFire .   # Windows、Linux
CGO_ENABLED=1 go build -trimpath -ldflags '-s -w' -o dist/CodingFire .   # macOS
go vet ./... && go test ./...
```

Windows 上 `build.ps1` 把这些包好了（`-Run`、`-Dump`、`-Render`），
`release.ps1 -Version 1.1.0` 负责打包和打 tag。macOS 上 `CGO_ENABLED=0` 能编过，但一启动
就 panic：*"no native backend available"* —— go-gui 只在开启 cgo 时才选 Metal 后端，
所以 macOS 产物是在 macOS runner 上构建的。

版本号只有一处出处：`internal/core/version.go`。发版工作流从源码里读出这个常量，
与 tag 不一致就拒绝发布。每次 push 都会在三端构建并测试，Linux 那个 job 还会在 Xvfb 下
真的把程序启动起来 —— X11 窗口层没有任何编译期信号。有一个测试是默认跳过的：悬浮窗
内存探针，`CODINGFIRE_MEMORY_PROBE=1 go test ./internal/ui -run TestOverlayMemoryProbe`。

`third_party/go-gui` 与 `third_party/go-glyph` 是打过补丁的 fork，通过 `go.mod` 的
`replace` 指向本地目录，**故意提交进仓库**，这样 clone 下来不用联网就能构建。go-gui 的
补丁修了 Windows 托盘菜单整体错一位，以及在 X11 上补写 `_NET_WM_PID` —— 缺了它就无法把
自己的窗口和别的客户端区分开。go-glyph 的补丁在关闭控制台时释放字体缓存。
**上游升级后每一处都要重新检查。**

## 发版

推一个 tag，[`.github/workflows/release.yml`](.github/workflows/release.yml) 会自动构建、
打包并发布：

```bash
git tag -a v1.1.0 -m "CodingFire v1.1.0"
git push origin v1.1.0
```

如果 `version.go` 声明的版本与 tag 不一致，工作流会直接中止；任何一个平台编不过，
就**在往 release 挂任何东西之前**失败。目标：`windows`、`linux`、`darwin`，各 amd64 与
arm64，每个都是一个单文件 zip，且每个都在与自己匹配的 runner 上构建、并核对二进制里
记录的 `GOOS`/`GOARCH`。`release.ps1` 在本地做 Windows 那一半。

## 数据放在哪

| 系统 | 目录 |
|---|---|
| Windows | `%APPDATA%\CodingFire\` |
| Linux | `$XDG_CONFIG_HOME/CodingFire/`（一般是 `~/.config/CodingFire/`） |
| macOS | `~/Library/Application Support/CodingFire/` |

这里就是 [C# 版](https://github.com/wangsrGit119/codingfire-win) 用的那个目录，**有意共用**：
两边文件格式完全一致，所以它们是同一个应用的两种实现 —— 来回切换不丢历史，也不丢设置。
代价是同一时刻只能跑一个，而这正是想要的：两个进程往同一份库里追加，只会把它写重复。

`usage.ndjson` 是事件库（保留 45 天），`cursors.json` 记录每个文件的读取游标，
`settings.json` 是你的偏好设置，`codingfire.log` 只在出错时写。设
`CODINGFIRE_DATA_DIR` 可改用便携数据目录。

唯一写在这个目录之外的是开机自启项，且只在开关打开时才写。它不需要管理员权限，
关掉自启的瞬间就会被删掉：Windows 是 `HKCU\...\Run` 值，Linux 是 XDG `.desktop` 文件，
macOS 是 LaunchAgent。这些名字同样和 C# 版共用，所以两者不可能同时占住登录项。

无界面自检：

```bash
CodingFire --dump 报告.txt     # 纯文本统计报告，固定英文
CodingFire --render 目录       # 把各档火势渲染成 PNG
```

## CPU、显卡与内存占用

- 兜底日志扫描每 **4 秒**一次；文件变化通知会提前触发，已有有效游标且内容未变的文件
  在分配读取缓冲区之前就跳过。
- 火焰 **10 帧/秒**，余烬 **4 帧/秒**；隐藏后不再重绘。状态计算与鼠标检测保持 20 Hz。
- Windows 上火焰与悬浮卡片用可复用的 DIB，控制台用软件绘制，正常运行不创建 OpenGL
  上下文。非 Windows 全部由 go-gui 的 OpenGL 后端绘制，因此需要可用的 GL 驱动 ——
  Mesa 的 `llvmpipe` 可以，CI 用的就是它。
- 实测工作集：带火焰约 33 MiB，带悬浮卡片约 34 MiB，关闭控制台后约 37 MiB。详见
  [对比记录](perf-artifacts/memory-optimization-report.md)。

`CODINGFIRE_OVERLAY_BACKEND=gl` 可在 Windows 上切回旧的 OpenGL 路径；不设置则默认走
低内存后端。想再省一点就选小尺寸火焰，或从托盘隐藏火焰 —— 两种情况下 token 统计都照常。

## 致谢

本项目是 macOS 版 **[TinyFire](https://github.com/wdkwdkwdk/tinyfire)**（作者
[@wdkwdkwdk](https://github.com/wdkwdkwdk)，MIT 协议）的重写版，并移植到 Windows、
Linux 与 macOS。数据源口径对齐
[juejin-cn/juejin-usage](https://github.com/juejin-cn/juejin-usage)。桌面窗口、托盘与
控制台基于 [go-gui](https://github.com/go-gui-org/go-gui) 构建。

## 协议

MIT —— 详见 [LICENSE](./LICENSE)。
