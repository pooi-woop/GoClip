package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 配置结构体
type Config struct {
	APIKey       string `mapstructure:"api_key"`
	YTDLPPath    string `mapstructure:"yt_dlp_path"`
	WhisperPath  string `mapstructure:"whisper_path"`
	WhisperModel string `mapstructure:"whisper_model"`
	LLMURL       string `mapstructure:"llm_url"`
	OutputDir    string `mapstructure:"output_dir"`
	MinSlices    int    `mapstructure:"min_slices"`
	MaxSlices    int    `mapstructure:"max_slices"`
}

// 全局配置和日志
var (
	config *Config
	logger *zap.Logger
)

// 初始化配置
func initConfig() error {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME/.goclip")
	viper.AddConfigPath("/etc/goclip")

	// 设置默认值
	viper.SetDefault("yt_dlp_path", "yt-dlp")
	viper.SetDefault("whisper_path", "whisper")
	viper.SetDefault("whisper_model", "medium")
	viper.SetDefault("llm_url", "https://dashscope.aliyuncs.com/compatible-mode/v1")
	viper.SetDefault("output_dir", "./output")
	viper.SetDefault("min_slices", 3)
	viper.SetDefault("max_slices", 5)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("读取配置文件错误: %w", err)
		}
		// 配置文件不存在，使用默认值
	}

	config = &Config{}
	if err := viper.Unmarshal(config); err != nil {
		return fmt.Errorf("解析配置错误: %w", err)
	}

	// 检查必要的配置
	if config.APIKey == "" {
		return fmt.Errorf("API Key 未配置")
	}

	return nil
}

// 初始化日志
func initLogger() error {
	// 创建输出目录
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	// 日志文件路径
	logFile := filepath.Join(config.OutputDir, "goclip.log")

	// 配置 zap
	zapConfig := zap.NewProductionConfig()
	zapConfig.OutputPaths = []string{"stdout", logFile}
	zapConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var err error
	logger, err = zapConfig.Build()
	if err != nil {
		return fmt.Errorf("初始化日志失败: %w", err)
	}

	return nil
}

// 下载视频
func downloadVideo(url string) (string, error) {
	logger.Info("开始下载视频", zap.String("url", url))

	// 创建临时目录
	tempDir := filepath.Join(config.OutputDir, "temp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("创建临时目录失败: %w", err)
	}

	// 构建 yt-dlp 命令
	cmd := exec.Command(config.YTDLPPath,
		"--output", filepath.Join(tempDir, "video.%(ext)s"),
		"--format", "bestvideo+bestaudio/best",
		url)

	// 执行命令
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("下载视频失败: %w, 输出: %s", err, string(output))
	}

	// 查找下载的视频文件
	files, err := os.ReadDir(tempDir)
	if err != nil {
		return "", fmt.Errorf("读取临时目录失败: %w", err)
	}

	var videoPath string
	for _, file := range files {
		if !file.IsDir() && strings.Contains(file.Name(), "video.") {
			videoPath = filepath.Join(tempDir, file.Name())
			break
		}
	}

	if videoPath == "" {
		return "", fmt.Errorf("未找到下载的视频文件")
	}

	logger.Info("视频下载成功", zap.String("path", videoPath))
	return videoPath, nil
}

// 确保 ffmpeg 可用
func ensureFFmpeg() (string, error) {
	// 工具目录
	toolsDir := filepath.Join(config.OutputDir, "tools")
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		return "", fmt.Errorf("创建工具目录失败: %w", err)
	}

	// ffmpeg 路径
	ffmpegPath := filepath.Join(toolsDir, "ffmpeg.exe")

	// 检查 ffmpeg 是否存在
	if _, err := os.Stat(ffmpegPath); os.IsNotExist(err) {
		// 下载 ffmpeg
		logger.Info("下载 ffmpeg", zap.String("path", ffmpegPath))
		ffmpegZipPath := filepath.Join(toolsDir, "ffmpeg.zip")

		// 下载 ffmpeg zip 文件
		cmd := exec.Command("powershell", "-Command", fmt.Sprintf(
			"Invoke-WebRequest -Uri 'https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip' -OutFile '%s'",
			ffmpegZipPath))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("下载 ffmpeg 失败: %w, 输出: %s", err, string(output))
		}

		// 解压 ffmpeg
		logger.Info("解压 ffmpeg", zap.String("zip_path", ffmpegZipPath))
		cmd = exec.Command("powershell", "-Command", fmt.Sprintf(
			"Expand-Archive -Path '%s' -DestinationPath '%s' -Force",
			ffmpegZipPath, toolsDir))
		output, err = cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("解压 ffmpeg 失败: %w, 输出: %s", err, string(output))
		}

		// 删除 zip 文件
		os.Remove(ffmpegZipPath)

		// 查找 ffmpeg.exe
		files, err := os.ReadDir(toolsDir)
		if err != nil {
			return "", fmt.Errorf("读取工具目录失败: %w", err)
		}

		for _, file := range files {
			if strings.Contains(file.Name(), "ffmpeg.exe") {
				ffmpegPath = filepath.Join(toolsDir, file.Name())
				break
			}
		}
	}

	return ffmpegPath, nil
}

// 生成字幕
func generateSubtitles(videoPath string) (string, error) {
	logger.Info("开始生成字幕", zap.String("video_path", videoPath))

	// 字幕文件路径
	subtitlePath := strings.Replace(videoPath, filepath.Ext(videoPath), ".srt", 1)

	// 模型目录
	modelDir := filepath.Join(config.OutputDir, "models")
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		return "", fmt.Errorf("创建模型目录失败: %w", err)
	}

	// 模型路径
	modelPath := filepath.Join(modelDir, config.WhisperModel+".bin")

	// 检查模型是否存在
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		// 下载模型
		logger.Info("下载 Whisper 模型", zap.String("model", config.WhisperModel))
		cmd := exec.Command("powershell", "-Command", fmt.Sprintf(
			"Invoke-WebRequest -Uri 'https://huggingface.co/ggerganov/whisper.cpp/resolve/main/%s.bin' -OutFile '%s'",
			config.WhisperModel, modelPath))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("下载模型失败: %w, 输出: %s", err, string(output))
		}
	}

	// 构建 whisper 命令
	cmd := exec.Command(config.WhisperPath,
		videoPath,
		"--model", modelPath,
		"--output", "srt",
		"--output-dir", filepath.Dir(videoPath))

	// 执行命令
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("生成字幕失败: %w, 输出: %s", err, string(output))
	}

	logger.Info("字幕生成成功", zap.String("path", subtitlePath))
	return subtitlePath, nil
}

// OpenAI 兼容的 API 请求结构体
type ChatRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Temperature float64 `json:"temperature"`
}

// OpenAI 兼容的 API 响应结构体
type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// 高光时刻结构体
type Highlight struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Title     string `json:"title"`
	Content   string `json:"content"`
}

// 生成高光
func generateHighlights(subtitlePath string) (string, error) {
	logger.Info("开始生成高光", zap.String("subtitle_path", subtitlePath))

	// 读取字幕文件
	subtitleContent, err := os.ReadFile(subtitlePath)
	if err != nil {
		return "", fmt.Errorf("读取字幕文件失败: %w", err)
	}

	// 构建提示词
	prompt := fmt.Sprintf(`请从以下字幕中提取%d-%d个最重要的高光时刻。

要求：
1. 每个高光时刻必须包含：开始时间、结束时间、标题（简短描述）、内容（详细说明）
2. 时间格式：HH:MM:SS
3. 标题应该简洁明了，能够概括该片段的核心内容
4. 输出格式必须是严格的JSON数组，格式如下：
[
  {
    "start_time": "00:01:23",
    "end_time": "00:01:45",
    "title": "精彩开场",
    "content": "这里是该片段的详细内容描述"
  }
]

字幕内容：
%s`, config.MinSlices, config.MaxSlices, string(subtitleContent))

	// 构建 OpenAI 兼容的 API 请求
	requestBody := ChatRequest{
		Model: "qwen-turbo", // 使用 Qwen 模型
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0.7,
	}

	// 序列化请求体
	requestJSON, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求体失败: %w", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", config.LLMURL+"/chat/completions", bytes.NewBuffer(requestJSON))
	if err != nil {
		return "", fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.APIKey)

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送 API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API 请求失败，状态码: %d", resp.StatusCode)
	}

	// 解析响应
	var response ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	// 生成高光文件
	highlightsPath := strings.Replace(subtitlePath, ".srt", "_highlights.json", 1)

	// 构建高光内容
	var highlightsContent string
	if len(response.Choices) > 0 {
		highlightsContent = response.Choices[0].Message.Content
		// 清理可能的 markdown 代码块标记
		highlightsContent = strings.TrimPrefix(highlightsContent, "```json")
		highlightsContent = strings.TrimPrefix(highlightsContent, "```")
		highlightsContent = strings.TrimSuffix(highlightsContent, "```")
		highlightsContent = strings.TrimSpace(highlightsContent)
	}

	// 写入高光文件
	if err := os.WriteFile(highlightsPath, []byte(highlightsContent), 0644); err != nil {
		return "", fmt.Errorf("写入高光文件失败: %w", err)
	}

	logger.Info("高光生成成功", zap.String("path", highlightsPath))
	return highlightsPath, nil
}

// 解析高光时间
func parseHighlightTimes(highlightsPath string) ([]Highlight, error) {
	// 读取高光文件
	highlightsContent, err := os.ReadFile(highlightsPath)
	if err != nil {
		return nil, fmt.Errorf("读取高光文件失败: %w", err)
	}

	// 解析JSON格式的高光数据
	var highlights []Highlight
	if err := json.Unmarshal(highlightsContent, &highlights); err != nil {
		return nil, fmt.Errorf("解析高光JSON失败: %w", err)
	}

	return highlights, nil
}

// 生成安全的文件名
func sanitizeFilename(name string) string {
	// 替换非法字符
	invalidChars := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	result := name
	for _, char := range invalidChars {
		result = strings.ReplaceAll(result, char, "_")
	}
	// 限制长度
	if len(result) > 50 {
		result = result[:50]
	}
	return result
}

// 生成视频切片（带字幕压制）
func generateSlices(videoPath string, subtitlePath string, highlights []Highlight) error {
	logger.Info("开始生成视频切片")

	// 确保 ffmpeg 可用
	ffmpegPath, err := ensureFFmpeg()
	if err != nil {
		return fmt.Errorf("确保 ffmpeg 可用失败: %w", err)
	}

	// 创建切片目录
	slicesDir := filepath.Join(filepath.Dir(videoPath), "slices")
	if err := os.MkdirAll(slicesDir, 0755); err != nil {
		return fmt.Errorf("创建切片目录失败: %w", err)
	}

	// 为每个高光生成切片
	for i, highlight := range highlights {
		// 使用标题作为文件名，如果没有标题则使用序号
		filename := sanitizeFilename(highlight.Title)
		if filename == "" {
			filename = fmt.Sprintf("highlight_%d", i+1)
		}
		slicePath := filepath.Join(slicesDir, fmt.Sprintf("%s.mp4", filename))

		// 构建 ffmpeg 命令（带字幕压制）
		cmd := exec.Command(ffmpegPath,
			"-i", videoPath,
			"-ss", highlight.StartTime,
			"-to", highlight.EndTime,
			"-vf", fmt.Sprintf("subtitles='%s'", subtitlePath),
			"-c:v", "libx264",
			"-crf", "23",
			"-c:a", "aac",
			"-b:a", "192k",
			"-y",
			slicePath)

		// 执行命令
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("生成切片失败: %w, 输出: %s", err, string(output))
		}

		logger.Info("切片生成成功",
			zap.String("path", slicePath),
			zap.String("title", highlight.Title),
			zap.String("start_time", highlight.StartTime),
			zap.String("end_time", highlight.EndTime))
	}

	return nil
}

// 主函数
func main() {
	// 初始化配置
	if err := initConfig(); err != nil {
		fmt.Printf("初始化配置失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志
	if err := initLogger(); err != nil {
		fmt.Printf("初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// 检查命令行参数
	if len(os.Args) < 2 {
		fmt.Println("使用方法: goclip <视频URL或本地视频路径>")
		os.Exit(1)
	}

	inputPath := os.Args[1]
	var videoPath string
	var err error

	// 检查输入是 URL 还是本地文件
	if strings.HasPrefix(inputPath, "http://") || strings.HasPrefix(inputPath, "https://") {
		// 下载视频
		videoPath, err = downloadVideo(inputPath)
		if err != nil {
			logger.Error("下载视频失败", zap.Error(err))
			os.Exit(1)
		}
	} else {
		// 检查本地文件是否存在
		if _, err := os.Stat(inputPath); os.IsNotExist(err) {
			fmt.Printf("本地文件不存在: %s\n", inputPath)
			os.Exit(1)
		}
		videoPath = inputPath
		logger.Info("使用本地视频", zap.String("path", videoPath))
	}

	// 生成字幕
	subtitlePath, err := generateSubtitles(videoPath)
	if err != nil {
		logger.Error("生成字幕失败", zap.Error(err))
		os.Exit(1)
	}

	// 生成高光
	highlightsPath, err := generateHighlights(subtitlePath)
	if err != nil {
		logger.Error("生成高光失败", zap.Error(err))
		os.Exit(1)
	}

	// 解析高光时间
	highlights, err := parseHighlightTimes(highlightsPath)
	if err != nil {
		logger.Error("解析高光时间失败", zap.Error(err))
		os.Exit(1)
	}

	// 生成视频切片
	if err := generateSlices(videoPath, subtitlePath, highlights); err != nil {
		logger.Error("生成视频切片失败", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("所有任务完成",
		zap.String("video_path", videoPath),
		zap.String("subtitle_path", subtitlePath),
		zap.String("highlights_path", highlightsPath))

	fmt.Printf("视频处理完成！\n")
	fmt.Printf("视频路径: %s\n", videoPath)
	fmt.Printf("字幕路径: %s\n", subtitlePath)
	fmt.Printf("高光路径: %s\n", highlightsPath)
	fmt.Printf("切片目录: %s\n", filepath.Join(filepath.Dir(videoPath), "slices"))
}
