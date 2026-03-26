# GoClip

GoClip 是一个基于Go语言开发的自动切片工具，用于下载视频、生成字幕、提取高光片段并自动生成视频切片。

## 功能

- 使用 yt-dlp 下载视频
- 使用 Whisper 生成字幕
- 使用 大模型 从字幕中提取高光（目前支持 Qwen）
- 根据提取的高光时间自动生成视频切片
- **自动将字幕压制进视频切片**
- **自动根据高光内容为切片命名**
- 支持本地视频处理（从提取字幕这一步开始继续走）
- 内置 ffmpeg、yt-dlp 和 Whisper 模型
- 使用 Viper 管理配置
- 使用 Zap 管理日志

## 快速开始（推荐）

**无需安装 Go 环境，直接使用编译好的工具！**

### 步骤 1：创建文件夹结构

1. **创建主文件夹**：在您的电脑上创建一个名为 `GoClip` 的文件夹（例如：`D:\GoClip`）
2. **创建子文件夹**：在 `GoClip` 文件夹内创建以下子文件夹：
   - `output/tools/` - 用于存放 ffmpeg 和 yt-dlp 工具
   - `output/models/` - 用于存放 Whisper 模型

### 步骤 2：下载所需文件

1. **下载编译好的工具**：
   - 从 [GitHub Releases](https://github.com/pooi-woop/GoClip/releases) 下载 `goclip.exe`，放到 `GoClip` 文件夹
   
2. **下载依赖文件**：
   - 从 Releases 下载以下文件并放到对应目录：
     - `ffmpeg.exe` (约114MB) → 放到 `output/tools/` 目录
     - `medium.bin` (约1.5GB) → 放到 `output/models/` 目录
     - `yt-dlp.exe` (约100MB) → 放到 `output/tools/` 目录
     - `whisper.exe` (约100MB) → 放到 `output/models/` 目录
   
   **提示**：如果 Releases 中没有提供这些文件，您也可以从以下来源获取：
   - `whisper.exe` 和模型：从 [Whisper GitHub 仓库](https://github.com/openai/whisper) 下载
   - `ffmpeg.exe`：从 [FFmpeg 官网](https://ffmpeg.org/download.html) 下载
   - `yt-dlp.exe`：从 [yt-dlp GitHub 仓库](https://github.com/yt-dlp/yt-dlp/releases) 下载

### 步骤 3：配置 API Key

1. **复制配置文件**：将 `config.yaml.example` 复制为 `config.yaml`
2. **编辑配置文件**：用记事本打开 `config.yaml`，填入您的 API Key

### 步骤 4：运行工具

1. **打开命令窗口**：在 `GoClip` 文件夹下打开命令提示符或 PowerShell 窗口
2. **运行命令**：输入以下命令并按回车
   ```bash
   .\goclip.exe <视频URL或本地视频路径>
   ```
   （将 `<视频URL或本地视频路径>` 替换为实际的视频链接或本地视频文件路径）

就这么简单！工具会自动完成所有处理步骤。

**注意**：首次运行时如果缺少必要文件，程序会自动下载，但国内下载速度较慢，建议提前从 Releases 或 GitHub 下载好所有依赖文件。

## 环境准备（仅用于编译）

如果您想自己编译项目，请按照以下步骤配置 Go 环境：

#### Windows 系统

1. **下载 Go**：
   - 访问 Go 官方网站：https://golang.org/dl/
   - 下载适用于 Windows 的安装包（推荐下载 .msi 安装程序）

2. **安装 Go**：
   - 双击下载的 .msi 安装程序
   - 按照安装向导完成安装（默认安装路径为 `C:\Program Files\Go`）
   - 安装程序会自动配置环境变量

3. **验证安装**：
   - 打开新的命令提示符或 PowerShell 窗口
   - 运行以下命令验证安装：
   ```bash
   go version
   ```
   - 如果显示 Go 版本信息，说明安装成功



## 安装（可选，仅用于编译）

### 依赖

- Go 1.20 或更高版本（仅用于编译）
- OpenAI 兼容的 API Key（如阿里云百炼 Qwen）

**注意**：ffmpeg、yt-dlp 和 Whisper 模型已包含在项目中，无需手动安装。

### 步骤

1. 克隆仓库
2. 安装依赖：
   ```bash
   go mod tidy
   ```
3. 复制示例配置文件并填写 API Key：
   ```bash
   copy config.yaml.example config.yaml
   ```
4. 构建项目：
   ```bash
   go build -o goclip.exe
   ```

## 使用

### 处理在线视频

```bash
goclip <视频URL>
```

### 处理本地视频

```bash
goclip <本地视频路径>
```

## 配置

配置文件 `config.yaml` 包含以下选项：

- `api_key`：OpenAI 兼容的 API Key（必填）
- `yt_dlp_path`：yt-dlp 可执行文件路径（默认：output/tools/yt-dlp.exe）
- `whisper_path`：Whisper 可执行文件路径（默认：whisper）
- `whisper_model`：Whisper 模型（默认：medium）
- `llm_url`：OpenAI 兼容的 API URL（默认：https://dashscope.aliyuncs.com/compatible-mode/v1）
- `output_dir`：输出目录（默认：./output）
- `min_slices`：最少生成切片数量（默认：3）
- `max_slices`：最多生成切片数量（默认：5）

## 输出

- 视频文件：`output/temp/video.*`（在线视频）或使用本地视频路径
- 字幕文件：`output/temp/video.srt` 或与本地视频同目录
- 高光文件：`output/temp/video_highlights.json` 或与本地视频同目录（JSON格式，包含标题、时间、内容）
- 视频切片：`output/temp/slices/` 或与本地视频同目录的 slices 文件夹（**已压制字幕**）
- 日志文件：`output/goclip.log`
- 工具目录：`output/tools/`（包含 yt-dlp.exe 和自动下载的 ffmpeg）
- 模型目录：`output/models/`（包含 Whisper 模型 medium.bin）

## 示例配置

对于阿里云百炼 Qwen 模型，配置示例：

```yaml
api_key: "your_aliyun_api_key"
yt_dlp_path: "yt-dlp"
whisper_path: "whisper"
whisper_model: "medium"
llm_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
output_dir: "./output"
min_slices: 3
max_slices: 5
```

## 工作流程

1. **下载视频**（仅在线视频）：使用 yt-dlp 下载视频到临时目录
2. **自动下载工具**：首次运行时自动下载 ffmpeg 和 Whisper 模型到项目目录
3. **生成字幕**：使用 Whisper 生成 SRT 格式的字幕
4. **提取高光**：通过 OpenAI 兼容的 API（如 Qwen）从字幕中提取高光时刻，输出为 JSON 格式，包含标题、时间、内容
5. **生成切片**：根据提取的高光时间范围，使用内置的 ffmpeg 生成视频切片，**自动将字幕压制进切片**，**切片以高光标题命名**

## 贡献指南

我们欢迎任何形式的贡献！如果您想为 GoClip 项目做出贡献，请参考以下指南。


### 交流反馈

- **QQ群**：1092257118
  - 欢迎加入我们的QQ群进行交流、反馈问题和分享使用经验
  - 群内可以讨论功能建议、报告bug、分享使用技巧

### 如何贡献

1. **报告问题**：
   - 在 GitHub Issues 中报告您遇到的 bug
   - 提供详细的错误信息和复现步骤
   - 附上相关的日志文件（`output/goclip.log`）

2. **功能建议**：
   - 欢迎提出新功能建议
   - 描述您的使用场景和需求
   - 讨论可能的实现方案

3. **代码贡献**：
   - Fork 本仓库
   - 创建您的特性分支（`git checkout -b my-new-feature`）
   - 提交您的更改（`git commit -am 'Add some feature'`）
   - 推送到分支（`git push origin my-new-feature`）
   - 创建 Pull Request

4. **文档改进**：
   - 改进 README 文档
   - 添加使用示例
   - 翻译文档到其他语言

### 开发指南

如果您想参与开发，请参考以下指南：

- 好的代码风格
- 为新功能添加注释
- 更新相关文档
- 确保功能可靠

### 赞助支持

如果您觉得 GoClip 对您有帮助，欢迎给我买杯咖啡来支持项目的持续开发。[GitHub Funding](https://github.com/pooi-woop/GoClip/sponsors)

您的赞助将用于：
- 持续改进和维护 GoClip
- 添加更多功能
- 优化性能和用户体验
- 提供更好的技术支持

感谢您的支持！


### 许可证

本项目采用开源许可证，具体请参考 LICENSE 文件。

