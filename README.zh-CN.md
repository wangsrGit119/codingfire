# CodingFire for Windows（Go 版）

把 AI 编程烧掉的 token 变成桌面上的一把像素篝火 —— 火势就是当前的 token 消耗速率。

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-lightgrey)](#系统要求)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8)](#从源码构建)
[![Release](https://img.shields.io/github/v/release/wangsrGit119/codingfire)](../../releases)
[![CI](https://github.com/wangsrGit119/codingfire/actions/workflows/ci.yml/badge.svg)](../../actions/workflows/ci.yml)
[![Release workflow](https://github.com/wangsrGit119/codingfire/actions/workflows/release.yml/badge.svg)](../../actions/workflows/release.yml)

[English](README.md) · **简体中文**

<p align="center">
  <img src="assets/example_01.gif" width="344" alt="CodingFire —— 偏绿的火苗">
  <img src="assets/example_02.gif" width="344" alt="CodingFire —— 经典橙色火苗">
</p>

一个常驻桌面顶层的小篝火，读取 AI 编程工具本来就写在磁盘上的 token 用量日志。
烧得越快，火越旺。

- **火势 = 实时 token 消耗速率**；多客户端同时跑也只有一把火（火势加总）
- 鼠标悬停弹出当日用量卡片与实时 tok/s
- **全程只读本地日志**，不联网、不上传、不读 prompt 或代码内容
- 单个静态 exe，**零运行时依赖**，不需要 .NET，不需要安装

> 这是用 Go + [go-gui](https://github.com/go-gui-org/go-gui) 重写的版本，功能与
> [C#/.NET 版](https://github.com/wangsrGit119/codingfire-win)完全对等。Windows 7 / 8
> 请继续用 C# 版。**两版可以同时装** —— 数据目录、开机自启项、单实例锁各自独立。

## 系统要求

| 系统 | 需要什么 |
|---|---|
| Windows 11 / 10（64 位） | 无，单个自包含 exe |
| Windows 8 / 8.1、Windows 7 SP1 | 请改用 [C# 版](https://github.com/wangsrGit119/codingfire-win) |
| Linux / macOS | **实验性**：会构建发布，但尚不可用 |

Go 1.21 起不再支持 Windows 7 / 8，所以本版**只支持 Win10 及以上**。以
`CGO_ENABLED=0` 编译、不链接 C 运行库，所以是完全静态的单个文件 ——
代价是体积约 18 MB（C# 版是 191 KB）。磁盘很便宜，缺 DLL 不便宜。

每次发布也会附上 Linux（`x64` / `arm64`）与 macOS（`x64` / `arm64`）的压缩包，
**给后续移植留个落点，但目前还不能用**：`internal/ui/win32_other.go`、
`internal/core/autostart_other.go` 等平台桩是有意留空的 no-op，所以在非 Windows 上
既没有鼠标穿透，也没有窗口定位和开机自启。**正式支持的只有 Windows**；
`windows/arm64` 会构建发布，但从未实机跑过。

## 下载与运行

到 [Releases](../../releases) 下载最新 zip，解压到任意目录，双击 `CodingFire.exe`。
压缩包里只有一个文件，没有安装过程。

- **鼠标移到火上** —— 当日用量卡片、实时 tok/s、当前档位
- **右键 / 双击托盘图标** —— 菜单与统计控制台
- **默认开机自启** —— 不想要的话在托盘菜单里取消勾选即可

首次启动没有数据时，火保持「余烬」状态，属正常。

### 鼠标穿透

Windows 版火焰和悬浮卡片使用原生分层位图窗口，与 C# 版一样，全透明像素天然不拦截点击。
本版还提供了一个**鼠标穿透**开关（托盘菜单，以及控制台「设置」页），**默认开启**，
免得篝火把本该点给桌面图标的点击吃掉。把鼠标移到篝火上，按住鼠标左键即可拖动；
由于穿透窗口收不到普通鼠标消息，程序会轮询全局鼠标按键状态来实现拖动。关闭鼠标穿透
后，篝火窗口本身也会接收点击。

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

需要 **Go 1.26 或更高**。不需要 C 编译器，不需要 Visual Studio。

```powershell
powershell -ExecutionPolicy Bypass -File build.ps1            # vet + 构建 -> dist\
powershell -ExecutionPolicy Bypass -File build.ps1 -Run       # 构建后直接启动
powershell -ExecutionPolicy Bypass -File build.ps1 -Dump      # 构建后输出用量报告
powershell -ExecutionPolicy Bypass -File build.ps1 -Render    # 构建后渲染各档火势
powershell -ExecutionPolicy Bypass -File release.ps1 -Version 1.0.0   # 打包 + 打 tag + 发布
```

构建会先跑 `go vet ./...`，有告警就直接中止。产物是 `dist\CodingFire.exe`，带
`-trimpath` 与 `-ldflags "-s -w -H=windowsgui"`，所以双击不会弹控制台窗口。

版本号只有一处出处：`internal/core/version.go`。Go 二进制没有版本资源，所以
`release.ps1` 和发版工作流都是从源码里读出这个常量，与 tag 不一致就拒绝发布。

### 测试

```powershell
go test ./...    # 全部包
go vet ./...     # build.ps1 编译前跑的就是这个
```

每次 push 和 PR 都会在 CI 里跑这两条
（[`.github/workflows/ci.yml`](.github/workflows/ci.yml)）。

有一个测试是默认跳过的：悬浮窗内存探针，它会真的创建窗口、耗时约 16 秒。要跑得显式打开：

```powershell
$env:CODINGFIRE_MEMORY_PROBE = '1'
go test ./internal/ui -run TestOverlayMemoryProbe -v -count=1
```

### 第三方依赖

`third_party/go-gui` 与 `third_party/go-glyph` 是打过补丁的 fork，通过 `go.mod` 的
`replace` 指向本地目录。**它们是故意提交进仓库的**，这样 clone 下来不用联网取依赖
就能构建：

- **go-gui** —— Windows 托盘菜单没有跳过 root 哨兵节点，导致所有菜单动作整体错一位；
  子菜单也不会把索引返回给后面的兄弟项。这个 fork 两处都修了。**上游升级后要重新检查这个补丁。**
- **go-glyph** —— 上游的全局字体缓存上限 384 MiB，且空闲五分钟后才淘汰，对一个小型
  常驻桌面工具来说完全不合适。这个 fork 加了释放接口，关闭控制台时把内存还回去。

### 发版

发版由 CI 完成。推一个 tag，
[`.github/workflows/release.yml`](.github/workflows/release.yml) 会自动构建、打包并发布：

```bash
git tag -a v1.0.0 -m "CodingFire for Windows (Go) v1.0.0"
git push origin v1.0.0
```

如果 `internal/core/version.go` 声明的版本与 tag 不一致，工作流会直接中止。也可以在
Actions 页面手动触发，自己填版本号。

工作流先统一校验一次，再并行构建全部目标，最后才发布 —— 所以某个平台编不过时，整条
流水线会**在往 release 挂任何东西之前**失败。目标：`windows/amd64`、`windows/arm64`、
`linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`，每个都是一个单文件 zip。

每个产物都会按自己的标签校验：Windows `amd64` 那个会被真正执行、读回 `--dump` 头；
交叉编译的用 `go version -m` 确认二进制里记录的 `GOOS`/`GOARCH` 与它将挂上去的名字
一致，再扫一遍版本号字面量。`.github/workflows/ci.yml` 在每次 push 时编译同一套矩阵，
所以某个平台被改坏会在**造成破坏的那次提交上**暴露，而不是等到打 tag。

不想等 runner 的话，`release.ps1` 在本地做同样的事（只构建 Windows 目标）。

## 数据放在哪

全部落在 `%APPDATA%\CodingFireGo\`：

| 文件 | 用途 |
|---|---|
| `usage.ndjson` | 事件库，保留 45 天 |
| `cursors.json` | 每个文件的读取游标 |
| `settings.json` | 尺寸、位置、语言、配色、开机自启 |
| `codingfire.log` | 仅出错时写 |

设 `CODINGFIRE_DATA_DIR` 可切便携模式。

唯一写在这个目录之外的东西，是「开机自启」在
`HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 下的一条值，值名是
`CodingFireGo`。它不需要管理员权限，关掉自启时会立刻被删掉。C# 版用的值名是
`CodingFire`，两版不会互相覆盖。

无界面自检：

```powershell
CodingFire.exe --dump 报告.txt     # 输出统计报告
CodingFire.exe --render 目录       # 把各档火势渲染成 PNG
```

`--dump` 的输出格式与 C# 版逐字节兼容，两版的报告可以直接对比。报告**固定用英文**，
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

同场景内存探针测得：火焰约 33 MiB、悬浮卡片约 34 MiB、关闭控制台后约 37 MiB。
控制台打开期间仍有较大的临时开销（本次约 182 MiB）。这是进程工作集，不是固定内存承诺；
详见[对比记录](perf-artifacts/memory-optimization-report.md)。目前未达到 C# 版反馈的 14 MB。

需要诊断渲染问题时，可设置 `CODINGFIRE_OVERLAY_BACKEND=gl` 使用旧 OpenGL 路径；
不设置时默认使用低内存的 Windows 位图路径。

需要进一步降低渲染开销时，可选择小尺寸火焰，或从托盘隐藏火焰；隐藏不影响
token 统计。重新编译后，请退出正在运行的旧副本，再启动 `dist\CodingFire.exe`。

## 致谢

本项目是 macOS 版 **[TinyFire](https://github.com/wdkwdkwdk/tinyfire)**（作者
[@wdkwdkwdk](https://github.com/wdkwdkwdk)，MIT 协议）的 Windows 版本，
感谢原作者给出的创意与核心算法。数据源口径对齐
[juejin-cn/juejin-usage](https://github.com/juejin-cn/juejin-usage)。桌面窗口、托盘
与控制台基于 [go-gui](https://github.com/go-gui-org/go-gui) 构建。

## 协议

MIT —— 详见 [LICENSE](./LICENSE)。
