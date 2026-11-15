# 真果鉴 · 网页版 (ZhenGuoJian-Web)

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8.svg)](https://go.dev/)
[![PWA](https://img.shields.io/badge/Web-Responsive%20SPA%20%2F%20PWA-FF6B35.svg)](#快速开始)

**真果鉴网页版**是一款基于纯 Go 静态编译（`CGO_ENABLED=0`）的跨平台短剧与影视自托管 Web 应用。程序内嵌多站源请求签名引擎、实时音视频流解密重封装服务与响应式 PWA 前端；本仓库在此基础上提供 Windows 开箱即用整合包（预编译 `zhenguojian-web.exe`、一键脚本与 `bin/` 下 FFmpeg 组件），在电脑、手机、平板或智能电视浏览器中即可直接使用。

> **仓库说明**：本仓库基于原项目 [**wzdyf1992/ZhenGuoJian-Web**](https://github.com/wzdyf1992/ZhenGuoJian-Web) 复制并维护，在保留原有特性与 GPL-3.0 开源协议的基础上，针对 Windows 开箱即用、全站源解锁、多站源解析兼容性、HEVC 无画面转码与前端分页体验修复了若干问题。
>
> 移动端原生客户端（Android / Android TV / iOS）见原仓库姐妹项目：[**ZhenGuoJian-Mobile**](https://github.com/wzdyf1992/ZhenGuoJian-Mobile)。两个项目的备份文件（`schema: 1`）完全互通。

---

## 本仓库修复与增强说明

相比原仓库，本仓库主要做了以下修复与适配：

- **Windows 一键启动与开箱即用**：
  - 仓库直接提供预编译的 `zhenguojian-web.exe`、一键启动脚本 `一键启动.bat` 以及源码重编译脚本 `重新编译并启动.bat`，Windows 用户获取完整仓库后无需配置 Go 环境即可直接运行。
  - 仓库内置 `bin/` 目录（包含 `ffmpeg.exe`、`ffprobe.exe` 及配套 DLL），开箱即可支持浏览器不支持编码的视频实时转码播放（若未携带 `bin/`，则自动回退系统 `PATH` 中的 FFmpeg 或原生流播放）。
  - 改进启动体验（[web/main.go](web/main.go)）：数据默认直接存放在项目/程序同级 `data/` 目录；启动默认自动打开浏览器（`-open=true`）；若检测到默认端口已有实例运行则直接唤起页面；未手动指定端口且 `7080` 被占用时自动顺延尝试 `7081`–`7100`。
- **全站源默认解锁与站源解析修复**：
  - 将 [web/store.go](web/store.go) 中原本标记为受限（`Restricted: true`）的 7 个站源（黄豆、剧果、野果、帝果、黄果视频、黄果 AI、黄果旧版）全部改为 `Restricted: false`，内置全部 11 个站源开箱即用。
  - 修复部分站源（如黄果边缘协议 `dx150-edge-v1` / HTTP 410、剧果分类、青空/Sorani 分页、野果、韩小圈、黄豆及离线 AI 分类等）在分类浏览、分页检索或视频地址解析时直接失败的问题。
- **解决“只有声音没有画面”（HEVC / ByteVC1）播放问题**：
  - 针对部分站源（如红果等）下发浏览器不支持原生解码的 HEVC / `bytevc1` 视频流导致“有声无画”的情况，服务端补充视频编码与时长探测，结合 `bin/ffmpeg.exe` 提供可拖动（Seekable）的 H.264 实时转码输出；前端播放器检测 `videoWidth === 0` 时也会自动回退至兼容转码流。
- **优化列表下滑加载滚动体验**：
  - 修复探索页与搜索页下滑触发加载更多时页面跳回顶部的问题，改为在现有列表尾部增量追加新卡片并保留当前滚动条位置。

---

## 核心特性

- **轻量跨平台部署**：Go 服务端采用 `CGO_ENABLED=0` 静态编译，前端静态资源与图标通过 `embed` 内嵌进二进制文件，支持 Windows、Linux（x86_64 / ARM64）、macOS（Apple Silicon / Intel）部署运行（Windows 仓库版本附带 `bin/` FFmpeg 以完整支持 HEVC 实时转码）。
- **全站源聚合与独立开关**：内置 11 个短剧 / 影视 / 番剧站源，默认全部放开，支持管理员在「设置 › 站源」逐个启用或停用，支持输入联想搜索与多季剧集聚合检索。
- **纯 Go 实时解密与无缝连播**：
  - 内置 ISO/IEC 23001-7 `cenc-aes-ctr` MP4 HTTP Range 实时流解密，以及 HLS 播放列表同源重写与伪装 `.ts` 分片头剥离。
  - 双 `<video>` 后台静音预缓冲管线 + 服务端 `moov` 索引与首段明文内存预热，自动连播与切下一集秒开起播。
  - 针对源站下发的 `HEIC` 封面，支持纯 Go 提取 HEVC 主图帧配合浏览器 `WebCodecs` / 单帧 MP4 解码回写 JPEG 缓存（若存在 FFmpeg 则自动后台转码）。
- **多端自适应交互与 PWA 支持**：
  - 自动适配手机底栏竖屏沉浸式播控、平板/电脑可折叠侧栏与左右分栏选集、电视大屏遥控器焦点导航与拼音首字母软键盘。
  - 移动端播放器支持左侧上下滑调节画面亮度、右侧上下滑调节音量、中间双击暂停/播放、两侧双击快退/快进及横屏全屏旋转锁定。
  - 支持红果分段滚动弹幕、倍速与清晰度记忆、6 套主题色与深浅色模式，HTTPS 环境下可一键安装为独立桌面/主屏 PWA 应用。
- **离线下载与无损转 MP4**：后台并发下载分集，完成后纯 Go 自动解密并转封装为标准 `.mp4` 文件，支持浏览器直接取回或按集保存。
- **多用户权限与跨端备份**：支持多用户独立保存追剧、观看记录与搜索历史，管理员可配置密码（3–32 位）、访问站源白名单与下载权限；支持一键导出/导入 `.json` 备份并与移动端客户端无缝迁移。

---

## 内置站源

| 站源 | 内容范围 | 搜索方式 |
| --- | --- | --- |
| 红果 | 短剧、漫剧、AI 剧；推荐混排流、榜单、题材筛选 | 在线搜索（含联想词与多季语义匹配） |
| 韩小圈 | 韩剧、韩国电影、综艺、动漫，多线路切换 | 在线搜索 |
| 鬼片 | 恐怖片、电视剧、动漫，多线路切换 | 匹配站点 RSS 最新条目 |
| 青空 | 番剧、剧场动画、特摄 | 在线搜索 |
| 黄豆 | 精选短剧、榜单 | 本机目录缓存检索 |
| 剧果 | 热门、最新、分类短剧 | 在线搜索 |
| 野果 | 分类短剧 | 在线搜索 |
| 帝果 | 分类视频 | 在线搜索 |
| 黄果视频 / 黄果 AI / 黄果旧版 | 视频剧集、AI 短剧与旧版剧库 | 黄果 AI 在线搜索，其余检索本机缓存 |

> 站源可用性、清晰度与区域限制取决于源站及部署主机网络；应用不解除源站 VIP 或其他授权限制。

---

## 快速开始（使用方式）

### 1. Windows 一键启动（推荐，开箱即用）

本仓库已附带编译好的 `zhenguojian-web.exe` 与 `bin/` 下的 FFmpeg 组件，Windows 用户只需两步即可使用：

1. **获取完整项目**：
   - 使用 Git 克隆：`git clone https://github.com/Nexro920/ZhenGuoJian-Web.git`
   - 或在 GitHub 页面点击 **Code → Download ZIP** 下载压缩包并解压到本地目录。
2. **双击启动**：
   - 直接双击根目录下的 **`一键启动.bat`** 即可运行。
   - 若目录中不存在 `zhenguojian-web.exe` 且本机已安装 Go 1.24.1+，脚本会自动执行静态编译并启动。
   - 修改源码后如需重新打包运行，可双击 **`重新编译并启动.bat`**（需本机安装 Go 1.24.1+）。

启动后程序会自动打开默认浏览器，并在终端打印本机访问地址（默认 `http://127.0.0.1:7080`，若未显式指定端口且 `7080` 被占用，将自动尝试 `7081`–`7100`；若同一地址已有实例运行则直接唤起浏览器）及局域网 IPv4 地址（自动过滤虚拟网卡与代理隧道），同一局域网的电脑、手机或电视浏览器也可直接访问。运行过程中生成的封面缓存、用户记录与下载内容默认保存在同级 `data/` 目录中。

> 如在无桌面环境或后台运行、希望禁用自动打开浏览器，可传入 `-open=false` 或设置环境变量 `ZGJ_NO_OPEN=1`：
> ```powershell
> .\zhenguojian-web.exe -open=false
> ```

### 2. Linux / macOS 编译运行

Linux / macOS 用户可通过 Go 1.24.1+ 工具链直接编译启动（如需 HEVC 转码支持，建议宿主机同时安装 `ffmpeg` / `ffprobe` 并加入 `PATH`）：

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.appVersion=1.0.13+113" -o ./zhenguojian-web ./web
chmod +x ./zhenguojian-web
./zhenguojian-web -open=false
```

### 3. 命令行参数与环境变量

| 参数 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `-listen` | `ZGJ_LISTEN` / `PORT` | `0.0.0.0:7080` | HTTP 监听地址与端口（未显式指定且端口被占用时自动尝试 `7081`–`7100`） |
| `-data-dir` | `ZGJ_DATA_DIR` | 项目/程序同级 `data/`（无法定位时回退系统配置目录 `zhenguojian-web`） | 配置、用户数据与剧库缓存目录 |
| `-downloads-dir` | `ZGJ_DOWNLOADS_DIR` | `<data-dir>/downloads` | 离线视频下载保存目录 |
| `-open` | `ZGJ_OPEN` / `ZGJ_NO_OPEN` | `true` | 启动后自动在默认浏览器打开页面（`ZGJ_NO_OPEN=1` 或 `-open=false` 可关闭） |
| `-disable-images` | `DISABLE_REMOTE_IMAGES` | `false` | 禁用远程站源海报请求并返回本地占位图 |
| `-ui-dir` | `ZGJ_UI_DIR` | 空（使用内嵌静态资源） | 前端热调试时指定外部静态目录覆盖 |

---

## 本地编译构建

### 环境要求

- **Go**：`1.24.1+`
- **Python**：`3.10+`（仅在使用跨平台批量构建脚本 `scripts/build.py` 时需要）

### 编译命令

```bash
# 直接启动开发服务
go run ./web

# 编译当前平台单文件可执行程序（输出至 dist/，仅含 Go 可执行文件，不含 bin/ FFmpeg）
python scripts/build.py

# 一次性交叉编译 Windows / Linux (amd64, arm64) / macOS (arm64, amd64) 全部平台产物（输出至 dist/）
python scripts/build.py --os all
```

### 代码检查与测试

```bash
go vet ./...
DISABLE_REMOTE_IMAGES=true go test ./...
```

> 若在 Linux / macOS 或已配置 C 编译器的环境下需要竞态检测，可运行 `DISABLE_REMOTE_IMAGES=true go test -race ./...`（Windows `CGO_ENABLED=0` 默认不支持 `-race`）。

---

## 项目结构

```text
ZhenGuoJian-Web/
├── core/                  # Go 多站源核心引擎（站源解析、签名、目录缓存、流代理、弹幕、下载调度）
├── web/                   # 独立 Web 服务端（HTTP API、纯 Go CENC 实时解密、HLS 封装、HEIC/FFmpeg 转码、多用户存储）
│   └── ui/                # 内嵌响应式前端（HTML / CSS / 原生 JS 模块 / PWA Manifest & Service Worker）
├── bin/                   # Windows 预置 FFmpeg / FFprobe 运行时及依赖 DLL
├── scripts/
│   └── build.py           # 跨平台静态单文件构建脚本
├── zhenguojian-web.exe    # Windows 预编译可执行文件
├── 一键启动.bat           # Windows 一键启动脚本（无 exe 时自动编译）
├── 重新编译并启动.bat     # Windows 一键静态重编译并启动脚本
├── go.mod
├── VERSION
└── LICENSE
```

---

## 免责声明

- 本项目仅供个人学习与音视频流媒体技术研究。
- 本项目与红果短剧及其他各站源运营方无任何关联，未获得其授权或认可；文中涉及的名称与商标归各自权利人所有。
- 程序本身不存储、不上传、不分发任何影视内容，所有数据均由使用者所在设备实时请求第三方站点获取，版权归原权利人所有；程序不解除源站 VIP 或其他授权限制。
- 使用者应遵守所在地法律法规及各站点服务条款，因部署或使用本项目产生的一切后果由使用者自行承担。

---

## 开源协议与致谢

- 本仓库复制并修改自 [**wzdyf1992/ZhenGuoJian-Web**](https://github.com/wzdyf1992/ZhenGuoJian-Web)，感谢原作者的开源贡献。
- 原项目核心站源框架改编自 [fish2018](https://github.com/fish2018) 的开源代码，整体以 [**GNU General Public License v3.0 (GPL-3.0)**](LICENSE) 协议发布。第三方依赖（包括预置的 FFmpeg 组件）遵循各自的开源许可证。
