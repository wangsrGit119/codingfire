# CodingFire

把 AI 编程烧掉的 token 变成桌面上的一把像素篝火 —— 烧得越快，火越旺。

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux%20%7C%20macOS-lightgrey)](#系统要求)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8)](#从源码构建)
[![Release](https://img.shields.io/github/v/release/wangsrGit119/codingfire)](../../releases)
[![CI](https://github.com/wangsrGit119/codingfire/actions/workflows/ci.yml/badge.svg)](../../actions/workflows/ci.yml)
[![Release workflow](https://github.com/wangsrGit119/codingfire/actions/workflows/release.yml/badge.svg)](../../actions/workflows/release.yml)

[English](README.md) · **简体中文**

<p align="center">
  <img src="assets/example_01.gif" width="344" alt="CodingFire —— 偏绿的火苗">
  <img src="assets/example_02.gif" width="344" alt="CodingFire —— 经典橙色火苗">
</p>

## 功能

**火势即速率。** 一个常驻桌面顶层的小篝火，读取 AI 编程工具本来就写在磁盘上的 token
用量日志。火势跟着**当前**消耗速率走 —— 一段忙碌的 agent 会话会把它从余烬推成旺火，
闲下来又慢慢烧回余烬。多个客户端同时跑也只有一把火（速率加总）。

**悬停卡片。** 鼠标移到火上，显示当日用量、实时 tok/s 和当前档位。

**统计控制台。** 右键或双击托盘图标：

| 页签 | 内容 |
|---|---|
| **统计** | 当日总量、逐小时时间线图表、按来源拆分、峰值速率、近期趋势 |
| **数据源** | 每个找过的来源、是否找到、贡献了多少 token |
| **设置** | 火焰尺寸与配色、各来源圆点颜色、鼠标穿透、开机自启、语言、位置 |
| **关于** | 版本号与项目主页 |

**桌面行为。** 常驻顶层；**默认开启鼠标穿透**，免得篝火把本该点给下面桌面图标的点击
吃掉。把鼠标移到篝火上按住左键即可拖动。默认随桌面启动，不想要就在菜单里关掉。

**四种界面语言** —— 英文、简体中文、日本語、한국어，也可以跟随系统。

**只读、离线。** 不联网、不上传、无遥测。不读 prompt、代码或文件内容，只取 token
数字与它们所在的文件路径。

**单文件、零运行时。** 一个静态可执行文件 —— 不需要 .NET、不引入 DLL、没有安装过程、
不需要管理员权限。内置 **23 个本地数据源**，见[下文](#支持的数据源)。

## 系统要求

| 系统 | 需要什么 |
|---|---|
| Windows 11 / 10（64 位） | 无，单个自包含可执行文件 |
| Linux（x64、arm64） | X11 会话 + 合成器（透明窗口需要），托盘还需要一个 StatusNotifier 宿主 |
| macOS（Intel、Apple 芯片） | 除了首次放行 Gatekeeper 之外，无 |

Windows 7、Windows 8 以及任何 32 位 Windows，请改用
[C# 版](https://github.com/wangsrGit119/codingfire-win)。

Windows 版体积约 18 MB：以 `CGO_ENABLED=0` 编译、不链接 C 运行库，所以是完全静态的
单个文件。

**Windows 是打磨最充分的目标平台** —— 程序本来就是为它写的，也是唯一使用原生分层位图
窗口的版本。Linux 与 macOS 分别走 go-gui 的 X11 与 Metal 后端，外加一层自己的平台垫片
来补 go-gui 没暴露的窗口管理；各自的前置条件和已知粗糙之处见
[各平台说明](#各平台说明)。`windows/arm64` 会构建发布，但从未实机跑过。

## 下载与运行

到 [Releases](../../releases) 下载最新 zip，解压到任意目录直接运行。
压缩包里只有一个文件，没有安装过程。

首次启动没有数据时，火保持「余烬」状态，属正常。

## 各平台说明

悬浮窗不是一套实现，而是三套，分别位于 `internal/ui/win32_*.go`，对外是同一组接口。
三者都要自己找到窗口、置顶、做穿透、移动位置，而**这些都没有任何编译期信号**。

| | Windows | Linux | macOS |
|---|---|---|---|
| 后端 | Win32 + WGL | X11 + EGL | AppKit + Metal |
| 置顶 | `SetWindowPos(HWND_TOPMOST)` | `_NET_WM_STATE_ABOVE` | `setLevel:` |
| 穿透 | `WS_EX_TRANSPARENT` | 清空 `ShapeInput` 区域 | `setIgnoresMouseEvents:` |
| 自启 | `HKCU\...\Run` | XDG `.desktop` | LaunchAgent plist |
| 托盘 | `Shell_NotifyIcon` | StatusNotifierItem（D-Bus） | `NSStatusItem` |

- **Linux** 的透明窗口需要合成器：没有合成器时篝火会画在不透明矩形上。托盘走
  StatusNotifierItem，KDE / XFCE / LXQt 原生支持，GNOME 需要装扩展；没有宿主时程序
  照常运行，只是没有菜单可以退出。
- **macOS** 发布的是未签名包，首次启动会被 Gatekeeper 拦下，需要在「系统设置 ›
  隐私与安全性」里放行一次。程序没有 Dock 图标，常驻菜单栏。
- **各平台都还没做**逐像素穿透。go-gui 把窗口当成一整块画面并自己做命中测试，所以穿透
  只能是全有或全无。

## 支持的数据源

**23 个数据源（22 种工具），全部只读解析本地日志**，不联网、不上传、不读 prompt
或代码内容 —— 只取 token 数字与文件路径。

- **原版 macOS 就有** —— Claude Code、Codex、Grok、Pi、Amp
- **对齐 [juejin-cn/juejin-usage](https://github.com/juejin-cn/juejin-usage) 补齐** ——
  WorkBuddy、CodeBuddy、Qoder、Qwen Code、Kimi、GitHub Copilot CLI、ZCode、
  OpenCode、Gemini CLI、Droid、Cline / Roo Code / Kilo Code、DeepSeek Harness、
  Command Code、OpenClaw、Every Code

WorkBuddy 的国内版与国外版是两份独立安装、两个 home 目录（`~/.workbuddy` 与
`~/.workbuddy-ai`），因此**分开统计**，列表里显示为 `WorkBuddy（内）` /
`WorkBuddy（外）` —— 这就是「23 个数据源、22 种工具」的来历。

每个来源都支持环境变量覆盖日志根目录（`CLAUDE_CONFIG_DIR`、`CODEX_HOME`、
`WORKBUDDY_HOME`、`WORKBUDDY_AI_HOME` …），详见源码 `internal/data/adapters*.go`。

**统计口径** —— 只计真实计费 token，缓存读/写单列，`reasoning` 不重复计；累计型
来源按「同 id 见过的最大总量」取差额入库，重启不重复计数；ZCode 内嵌子代理消息
归各自的源统计，避免重复。

**刻意不采集** —— Cursor（用量只在云端 API 后）、Kiro / Antigravity / QwenWork
（本地无真实 token 元数据，上游靠字符数估算）、Trae（SQLCipher 加密），以及若干
库结构未核实的工具 —— 宁可不收也不乱收。

## 从源码构建

需要 **Go 1.26 或更高**。Windows 上不需要 C 编译器，也不需要 Visual Studio；
macOS 上 Xcode 命令行工具自带的 C 工具链**是必需的**，因为 Metal 后端是 cgo。

```powershell
powershell -ExecutionPolicy Bypass -File build.ps1            # vet + 构建 -> dist\
powershell -ExecutionPolicy Bypass -File build.ps1 -Run       # 构建后直接启动
powershell -ExecutionPolicy Bypass -File build.ps1 -Dump      # 构建后输出用量报告
powershell -ExecutionPolicy Bypass -File build.ps1 -Render    # 构建后渲染各档火势
powershell -ExecutionPolicy Bypass -File release.ps1 -Version 1.1.0   # 打包 + 打 tag + 发布
```

构建会先跑 `go vet ./...`，有告警就直接中止。产物是 `dist\CodingFire.exe`，带
`-trimpath` 与 `-ldflags "-s -w -H=windowsgui"`，所以双击不会弹控制台窗口。

非 Windows 平台没有对应脚本（`build.ps1` / `release.ps1` 是 Windows 脚本），直接 `go build`：

```bash
# Linux：纯 Go，不用 cgo，任何机器上都能交叉编译
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o dist/CodingFire .

# macOS：cgo 不是可选项，且需要 macOS SDK
CGO_ENABLED=1 go build -trimpath -ldflags '-s -w' -o dist/CodingFire .
```

macOS 上用 `CGO_ENABLED=0` 能编过，但一启动就 panic：
*"no native backend available"* —— go-gui 只在开启 cgo 时才选 Metal 后端。
所以 macOS 产物是在 macOS runner 上构建的，而不是交叉编译。

版本号只有一处出处：`internal/core/version.go`。Go 二进制没有版本资源，所以
`release.ps1` 和发版工作流都是从源码里读出这个常量，与 tag 不一致就拒绝发布。

### 测试

```powershell
go test ./...    # 全部包
go vet ./...     # build.ps1 编译前跑的就是这个
```

每次 push 和 PR 都会在 Windows、Linux、macOS 三端跑这两条
（[`.github/workflows/ci.yml`](.github/workflows/ci.yml)）。Linux 那个 job 还会在虚拟
X 服务器下**真的把程序启动起来**，确认它能正常起来
（[`scripts/linux-smoke.sh`](scripts/linux-smoke.sh)）—— X11 窗口层没有任何编译期信号，
做错了是完全静默的。

有一个测试是默认跳过的：悬浮窗内存探针，它会真的创建窗口、耗时约 16 秒（仅 Windows）。要跑得显式打开：

```powershell
$env:CODINGFIRE_MEMORY_PROBE = '1'
go test ./internal/ui -run TestOverlayMemoryProbe -v -count=1
```

### 第三方依赖

`third_party/go-gui` 与 `third_party/go-glyph` 是打过补丁的 fork，通过 `go.mod` 的
`replace` 指向本地目录。**它们是故意提交进仓库的**，这样 clone 下来不用联网取依赖
就能构建：

- **go-gui** —— 共三处补丁，**上游升级后每一处都要重新检查**：
  - Windows：托盘菜单没有跳过 root 哨兵节点，导致所有菜单动作整体错一位；
    子菜单也不会把索引返回给后面的兄弟项。这个 fork 两处都修了。
  - X11：创建窗口时没有写 `_NET_WM_PID`。X 协议本身没有「窗口归属」查询，缺了这个属性
    就无法把自己进程的窗口和别的客户端区分开 —— 而悬浮窗恰恰必须找到自己的窗口，
    因为 go-gui 把 XID 藏起来了。
- **go-glyph** —— 上游的全局字体缓存上限 384 MiB，且空闲五分钟后才淘汰，对一个小型
  常驻桌面工具来说完全不合适。这个 fork 加了释放接口，关闭控制台时把内存还回去。

### 发版

发版由 CI 完成。推一个 tag，
[`.github/workflows/release.yml`](.github/workflows/release.yml) 会自动构建、打包并发布：

```bash
git tag -a v1.1.0 -m "CodingFire v1.1.0"
git push origin v1.1.0
```

如果 `internal/core/version.go` 声明的版本与 tag 不一致，工作流会直接中止。也可以在
Actions 页面手动触发，自己填版本号。

工作流先统一校验一次，再并行构建全部目标，最后才发布 —— 所以某个平台编不过时，整条
流水线会**在往 release 挂任何东西之前**失败。目标：`windows/amd64`、`windows/arm64`、
`linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`，每个都是一个单文件 zip。

每个目标都在**与自己匹配的 runner** 上构建，所以校验是真的校验而不是只编一遍：

- **windows** 与 **linux** 用 `CGO_ENABLED=0` 交叉编译。Windows `amd64` 那个会被真正
  执行、读回 `--dump` 头；Linux `amd64` 那个先跑一次无界面报告，再在 Xvfb 下启动。
- **macos** 在 macOS runner 上以 `CGO_ENABLED=1` 构建，并执行一次 `--dump`。
- 所有产物还会用 `go version -m` 确认二进制里记录的 `GOOS`/`GOARCH` 与它将挂上去的名字
  一致，再扫一遍版本号字面量。

不想等 runner 的话，`release.ps1` 在本地做同样的事（只构建 Windows 目标）。

## 数据放在哪

全部落在一个目录里：

| 系统 | 目录 |
|---|---|
| Windows | `%APPDATA%\CodingFireGo\` |
| Linux | `$XDG_CONFIG_HOME/CodingFireGo/`（一般是 `~/.config/CodingFireGo/`） |
| macOS | `~/Library/Application Support/CodingFireGo/` |

| 文件 | 用途 |
|---|---|
| `usage.ndjson` | 事件库，保留 45 天 |
| `cursors.json` | 每个文件的读取游标 |
| `settings.json` | 尺寸、位置、语言、配色、开机自启 |
| `codingfire.log` | 仅出错时写 |

设 `CODINGFIRE_DATA_DIR` 可切便携模式。

唯一写在这个目录之外的东西是「开机自启」项，且只在开关打开时才写。它不需要管理员权限，
关掉自启时会立刻被删掉：

| 系统 | 写在哪 |
|---|---|
| Windows | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`，值名 `CodingFireGo` |
| Linux | `$XDG_CONFIG_HOME/autostart/CodingFireGo.desktop` |
| macOS | `~/Library/LaunchAgents/com.codingfire.go.plist` |

每个名字都带 `Go` 标记，所以它不会和另一份副本撞名、把对方的自启项悄悄关掉。

无界面自检：

```powershell
CodingFire.exe --dump 报告.txt     # 输出统计报告
CodingFire.exe --render 目录       # 把各档火势渲染成 PNG
```

`--dump` 输出纯文本报告：总量、按来源拆分、以及已存历史。报告**固定用英文**，
与界面语言设置无关。

## CPU、显卡与内存占用

日志兜底扫描每 **4 秒**执行一次，文件变化通知仍可提前触发扫描。已有有效游标、
内容未变化的 JSONL 文件直接跳过，不再分配读取缓冲区；启动时逐行加载历史记录，
避免同时保留整份原始文本和解析结果。

燃烧时保持 **10 帧/秒**，余烬为 **4 帧/秒**；隐藏火焰后停止定时重绘，完全熄灭后
只在状态切换时绘制一次。状态计算和鼠标检测仍保持 20 Hz。Windows 默认使用可复用的
原生 DIB 位图显示火焰，悬浮卡片使用 Windows 字体，控制台使用软件绘制和 GDI 显示，
正常运行不创建 OpenGL 上下文。悬浮统计每秒刷新两次。关闭控制台时真正销毁窗口，
并释放共享字体缓存。

非 Windows 平台全部由 go-gui 的 OpenGL 后端绘制，因此需要可用的 GL 驱动 —— 真显卡，
或者 Mesa 的软件光栅器（`llvmpipe`），CI 用的就是后者。

同场景内存探针测得：火焰约 33 MiB、悬浮卡片约 34 MiB、关闭控制台后约 37 MiB。
控制台打开期间仍有较大的临时开销（本次约 182 MiB）。这是进程工作集，不是固定内存承诺；
详见[对比记录](perf-artifacts/memory-optimization-report.md)。

需要诊断渲染问题时，可设置 `CODINGFIRE_OVERLAY_BACKEND=gl` 使用旧 OpenGL 路径；
不设置时默认使用低内存的 Windows 位图路径。

需要进一步降低渲染开销时，可选择小尺寸火焰，或从托盘隐藏火焰；隐藏不影响
token 统计。重新编译后，请退出正在运行的旧副本，再启动 `dist\CodingFire.exe`。

## 致谢

本项目是 macOS 版 **[TinyFire](https://github.com/wdkwdkwdk/tinyfire)**（作者
[@wdkwdkwdk](https://github.com/wdkwdkwdk)，MIT 协议）的重写版，先做 Windows，
之后移植到 Linux 与 macOS，感谢原作者给出的创意与核心算法。数据源口径对齐
[juejin-cn/juejin-usage](https://github.com/juejin-cn/juejin-usage)。桌面窗口、托盘
与控制台基于 [go-gui](https://github.com/go-gui-org/go-gui) 构建。

## 协议

MIT —— 详见 [LICENSE](./LICENSE)。
