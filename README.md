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
- 自动下载和内置 ffmpeg 和 Whisper 模型
- 使用 Viper 管理配置
- 使用 Zap 管理日志

## 安装

### 依赖

- Go 1.20 或更高版本
- yt-dlp
- Whisper
- OpenAI 兼容的 API Key（如阿里云百炼 Qwen）

**注意**：ffmpeg 和 Whisper 模型会在首次运行时自动下载到项目中，无需手动安装。

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
- `yt_dlp_path`：yt-dlp 可执行文件路径（默认：yt-dlp）
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
- 工具目录：`output/tools/`（包含自动下载的 ffmpeg）
- 模型目录：`output/models/`（包含自动下载的 Whisper 模型）

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
