// GoClip - 视频切片工具
// 作者：皖月清风
// 开源协议：MIT
// 本项目开源免费，请勿从二道贩子处购买
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 配置结构体
type Config struct {
	APIKey       string `mapstructure:"api_key"`
	YTDLPPath    string `mapstructure:"yt_dlp_path"`
	WhisperPath  string `mapstructure:"whisper_path"`
	WhisperModel string `mapstructure:"whisper_model"`
	LLMURL       string `mapstructure:"llm_url"`
	LLMModel     string `mapstructure:"llm_model"`
	OutputDir    string `mapstructure:"output_dir"`
	MinSlices    int    `mapstructure:"min_slices"`
	MaxSlices    int    `mapstructure:"max_slices"`
	FFmpegPath   string `mapstructure:"ffmpeg_path"`
	Language     string `mapstructure:"language"`
	IntroPath    string `mapstructure:"intro_path"`
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

	viper.SetDefault("yt_dlp_path", "output/tools/yt-dlp.exe")
	viper.SetDefault("whisper_path", "whisper")
	viper.SetDefault("whisper_model", "medium")
	viper.SetDefault("llm_url", "https://dashscope.aliyuncs.com/compatible-mode/v1")
	viper.SetDefault("llm_model", "qwen-max")
	viper.SetDefault("output_dir", "./output")
	viper.SetDefault("min_slices", 3)
	viper.SetDefault("max_slices", 5)
	viper.SetDefault("language", "Chinese")
	viper.SetDefault("intro_path", "")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("读取配置文件错误: %w", err)
		}
		// 配置文件不存在，提示用户创建
		fmt.Println("配置文件不存在！")
		fmt.Println("请按照以下步骤配置：")
		fmt.Println("1. 复制 config.yaml.example 为 config.yaml")
		fmt.Println("2. 编辑 config.yaml，填入您的 API Key")
		fmt.Println("")
		fmt.Println("示例：")
		fmt.Println("  copy config.yaml.example config.yaml")
		fmt.Println("  # 然后编辑 config.yaml 文件，将 api_key 设置为您的实际值")
		return fmt.Errorf("配置文件未找到")
	}

	config = &Config{}
	if err := viper.Unmarshal(config); err != nil {
		return fmt.Errorf("解析配置错误: %w", err)
	}

	if config.APIKey == "" || config.APIKey == "your_api_key_here" {
		fmt.Println("API Key 未配置！")
		fmt.Println("请编辑 config.yaml 文件，将 api_key 设置为您的实际 API Key")
		fmt.Println("")
		fmt.Println("获取 API Key 的方法：")
		fmt.Println("- 阿里云百炼：https://bailian.console.aliyun.com/")
		fmt.Println("- 其他 OpenAI 兼容服务：请参考相应文档")
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

	// 确保 yt-dlp 可用
	ytDlpPath, err := ensureYTDLP()
	if err != nil {
		return "", fmt.Errorf("确保 yt-dlp 可用失败: %w", err)
	}

	// 构建 yt-dlp 命令
	cmd := exec.Command(ytDlpPath,
		"--output", filepath.Join(tempDir, "video.%(ext)s"),
		"--format", "bestvideo+bestaudio/best",
		url)

	// 执行命令并显示实时输出
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("下载视频失败: %w", err)
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

// 确保 yt-dlp 可用
func ensureYTDLP() (string, error) {
	// 如果配置了 yt-dlp 路径，先检查是否存在
	if config.YTDLPPath != "" {
		if _, err := os.Stat(config.YTDLPPath); err == nil {
			return config.YTDLPPath, nil
		}
	}

	// 检查系统 PATH 中是否有 yt-dlp
	cmd := exec.Command("where", "yt-dlp")
	if output, err := cmd.Output(); err == nil {
		path := strings.TrimSpace(string(output))
		if path != "" {
			return path, nil
		}
	}

	return "", fmt.Errorf("yt-dlp 未找到，请安装或配置正确的路径")
}

// 确保 ffmpeg 可用
func ensureFFmpeg() (string, error) {
	// 如果配置了 ffmpeg 路径，先检查是否存在
	if config.FFmpegPath != "" {
		if _, err := os.Stat(config.FFmpegPath); err == nil {
			return config.FFmpegPath, nil
		}
	}

	// 检查项目目录中的 ffmpeg
	ffmpegPath := filepath.Join("output", "tools", "ffmpeg.exe")
	if _, err := os.Stat(ffmpegPath); err == nil {
		return ffmpegPath, nil
	}

	// 检查系统 PATH 中是否有 ffmpeg
	cmd := exec.Command("where", "ffmpeg")
	if output, err := cmd.Output(); err == nil {
		path := strings.TrimSpace(string(output))
		if path != "" {
			return path, nil
		}
	}

	return "", fmt.Errorf("ffmpeg 未找到，请安装或配置正确的路径")
}

// 确保 Whisper 可用
func ensureWhisper() (string, error) {
	// 如果配置了 Whisper 路径，先检查是否存在
	if config.WhisperPath != "" {
		if _, err := os.Stat(config.WhisperPath); err == nil {
			return config.WhisperPath, nil
		}
	}

	// 检查项目目录中的 whisper
	whisperPath := filepath.Join("output", "tools", "whisper.exe")
	if _, err := os.Stat(whisperPath); err == nil {
		return whisperPath, nil
	}

	// 检查系统 PATH 中是否有 whisper
	cmd := exec.Command("where", "whisper")
	if output, err := cmd.Output(); err == nil {
		path := strings.TrimSpace(string(output))
		if path != "" {
			return path, nil
		}
	}

	return "", fmt.Errorf("whisper 未找到，请安装或配置正确的路径")
}

// 获取视频时长（秒）
func getVideoDuration(videoPath string) (int, error) {
	// 确保 ffmpeg 可用
	ffmpegPath, err := ensureFFmpeg()
	if err != nil {
		return 0, fmt.Errorf("确保 ffmpeg 可用失败: %w", err)
	}

	// 构建 ffmpeg 命令来获取视频时长
	cmd := exec.Command(ffmpegPath, "-i", videoPath, "-show_entries", "format=duration", "-v", "quiet", "-of", "csv=p=0")

	// 执行命令并获取输出
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("获取视频时长失败: %w", err)
	}

	// 解析输出
	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("解析视频时长失败: %w", err)
	}

	return int(duration), nil
}

// 分割长视频
func splitLongVideo(videoPath string) ([]string, error) {
	// 获取视频时长
	duration, err := getVideoDuration(videoPath)
	if err != nil {
		return nil, fmt.Errorf("获取视频时长失败: %w", err)
	}

	// 如果视频时长小于30分钟，不需要分割
	if duration < 30*60 {
		return []string{videoPath}, nil
	}

	logger.Info("视频时长较长，开始分割", zap.Int("duration", duration))

	// 确保 ffmpeg 可用
	ffmpegPath, err := ensureFFmpeg()
	if err != nil {
		return nil, fmt.Errorf("确保 ffmpeg 可用失败: %w", err)
	}

	// 创建分割目录
	splitDir := filepath.Join(filepath.Dir(videoPath), "split")
	if err := os.MkdirAll(splitDir, 0755); err != nil {
		return nil, fmt.Errorf("创建分割目录失败: %w", err)
	}

	// 分割时长（25分钟）
	splitDuration := 25 * 60
	var segments []string

	// 计算需要分割的段数
	segmentsCount := (duration + splitDuration - 1) / splitDuration

	// 分割视频
	for i := 0; i < segmentsCount; i++ {
		startTime := i * splitDuration
		endTime := (i + 1) * splitDuration
		if endTime > duration {
			endTime = duration
		}

		// 生成输出文件名
		baseName := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
		segmentPath := filepath.Join(splitDir, fmt.Sprintf("%s_part_%d.mp4", baseName, i+1))

		// 构建 ffmpeg 命令
		cmd := exec.Command(ffmpegPath,
			"-i", videoPath,
			"-ss", fmt.Sprintf("%d", startTime),
			"-to", fmt.Sprintf("%d", endTime),
			"-c:v", "copy",
			"-c:a", "copy",
			"-y",
			segmentPath)

		// 执行命令并显示实时输出
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			return nil, fmt.Errorf("分割视频失败: %w", err)
		}

		logger.Info("视频分割成功",
			zap.String("path", segmentPath),
			zap.Int("start_time", startTime),
			zap.Int("end_time", endTime))

		segments = append(segments, segmentPath)
	}

	return segments, nil
}

// 合并字幕文件
func mergeSubtitles(subtitlePaths []string, outputPath string) error {
	// 读取所有字幕文件
	var mergedContent strings.Builder
	var index int = 1

	for _, path := range subtitlePaths {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取字幕文件失败: %w", err)
		}

		// 解析字幕文件，调整序号
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if line != "" {
				// 检查是否是序号行
				if _, err := strconv.Atoi(line); err == nil {
					// 替换为新序号
					mergedContent.WriteString(fmt.Sprintf("%d\n", index))
					index++
				} else {
					mergedContent.WriteString(line + "\n")
				}
			}
		}
	}

	// 写入合并后的字幕文件
	if err := os.WriteFile(outputPath, []byte(mergedContent.String()), 0644); err != nil {
		return fmt.Errorf("写入合并字幕文件失败: %w", err)
	}

	logger.Info("字幕合并成功", zap.String("path", outputPath))
	return nil
}

// 生成字幕
func generateSubtitles(videoPath string) (string, error) {
	logger.Info("开始生成字幕", zap.String("video_path", videoPath))

	// 检查是否有单独的音频文件
	videoDir := filepath.Dir(videoPath)
	audioPath := videoPath

	// 查找同目录下的音频文件
	files, err := os.ReadDir(videoDir)
	if err == nil {
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".m4a") {
				audioPath = filepath.Join(videoDir, file.Name())
				logger.Info("使用单独的音频文件", zap.String("audio_path", audioPath))
				break
			}
		}
	}

	// 检查字幕文件是否已经存在
	videoName := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	expectedSubtitlePath := filepath.Join(videoDir, videoName+".srt")

	// 对于切片文件，即使字幕文件已存在，也重新生成字幕
	// 这样可以确保每个切片都有自己的字幕
	if strings.Contains(videoDir, "slices") {
		logger.Info("为切片重新生成字幕", zap.String("path", expectedSubtitlePath))
	} else if _, err := os.Stat(expectedSubtitlePath); err == nil {
		logger.Info("字幕文件已存在，跳过生成步骤", zap.String("path", expectedSubtitlePath))
		return expectedSubtitlePath, nil
	}

	// 对于非切片文件，先进行分段处理
	if !strings.Contains(videoDir, "slices") && !strings.Contains(videoDir, "split") {
		// 分割长视频
		segments, err := splitLongVideo(videoPath)
		if err != nil {
			return "", fmt.Errorf("分割视频失败: %w", err)
		}

		// 如果视频被分割了，为每个片段生成字幕然后合并
		if len(segments) > 1 {
			var subtitlePaths []string
			for _, segment := range segments {
				// 为每个片段生成字幕
				subtitlePath, err := generateSubtitles(segment)
				if err != nil {
					return "", fmt.Errorf("为片段生成字幕失败: %w", err)
				}
				subtitlePaths = append(subtitlePaths, subtitlePath)
			}

			// 合并字幕文件
			if err := mergeSubtitles(subtitlePaths, expectedSubtitlePath); err != nil {
				return "", fmt.Errorf("合并字幕失败: %w", err)
			}

			logger.Info("长视频字幕生成成功", zap.String("path", expectedSubtitlePath))
			return expectedSubtitlePath, nil
		}
	}

	// 确保 ffmpeg 可用
	ffmpegPath, err := ensureFFmpeg()
	if err != nil {
		return "", fmt.Errorf("确保 ffmpeg 可用失败: %w", err)
	}

	// 确保 Whisper 可用
	whisperPath, err := ensureWhisper()
	if err != nil {
		return "", fmt.Errorf("确保 Whisper 可用失败: %w", err)
	}

	// 构建 whisper 命令
	// 指定模型目录，让 Whisper 在 output/models 目录中查找和下载模型
	modelDir := filepath.Join(config.OutputDir, "models")
	cmd := exec.Command(whisperPath,
		audioPath,
		"--model", config.WhisperModel,
		"--model_dir", modelDir,
		"--output_format", "srt",
		"--output_dir", filepath.Dir(videoPath),
		"--language", config.Language)

	// 添加 ffmpeg 路径到环境变量
	env := os.Environ()
	ffmpegDir := filepath.Dir(ffmpegPath)
	env = append(env, fmt.Sprintf("PATH=%s;%s", ffmpegDir, os.Getenv("PATH")))
	cmd.Env = env

	// 执行命令并显示实时输出
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("生成字幕失败: %w", err)
	}

	// 等待一段时间让文件写入完成
	time.Sleep(2 * time.Second)

	// 查找生成的字幕文件
	videoName = strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	expectedSubtitlePath = filepath.Join(videoDir, videoName+".srt")

	// 打印目录内容进行调试
	files, err = os.ReadDir(videoDir)
	if err != nil {
		return "", fmt.Errorf("读取目录失败: %w", err)
	}

	logger.Info("目录内容", zap.Int("file_count", len(files)))
	// 只查找与当前音频文件同名的字幕文件
	targetSubtitleName := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath)) + ".srt"
	for _, file := range files {
		logger.Info("文件", zap.String("name", file.Name()), zap.Bool("is_dir", file.IsDir()))
		if strings.HasSuffix(file.Name(), ".srt") {
			// 只选择与当前音频文件同名的字幕文件
			if file.Name() == targetSubtitleName {
				logger.Info("找到字幕文件", zap.String("path", filepath.Join(videoDir, file.Name())))
				expectedSubtitlePath = filepath.Join(videoDir, file.Name())
				break
			}
		}
	}

	// 再次检查文件是否存在
	if _, err := os.Stat(expectedSubtitlePath); os.IsNotExist(err) {
		return "", fmt.Errorf("未找到生成的字幕文件")
	}

	logger.Info("字幕生成成功", zap.String("path", expectedSubtitlePath))
	return expectedSubtitlePath, nil
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
	// 计算高光文件路径
	highlightsPath := strings.Replace(subtitlePath, ".srt", "_highlights.json", 1)

	// 检查高光文件是否已存在
	if _, err := os.Stat(highlightsPath); err == nil {
		// 检查文件内容是否为有效的JSON
		content, err := os.ReadFile(highlightsPath)
		if err == nil {
			// 清理内容
			contentStr := strings.TrimSpace(string(content))
			contentStr = strings.TrimPrefix(contentStr, "\ufeff")
			// 检查是否是有效的JSON
			if contentStr != "" && (strings.HasPrefix(contentStr, "[") || strings.HasPrefix(contentStr, "{")) {
				logger.Info("高光文件已存在且有效，跳过生成步骤", zap.String("path", highlightsPath))
				return highlightsPath, nil
			}
		}
		// 文件存在但不是有效JSON，重新生成
		logger.Info("高光文件存在但不是有效JSON，重新生成", zap.String("path", highlightsPath))
	}

	logger.Info("开始生成高光", zap.String("subtitle_path", subtitlePath))

	// 读取字幕文件
	subtitleContent, err := os.ReadFile(subtitlePath)
	if err != nil {
		return "", fmt.Errorf("读取字幕文件失败: %w", err)
	}

	// 从文件中读取提示词
	promptTemplate, err := os.ReadFile("prompts/highlight_prompt.txt")
	if err != nil {
		// 如果文件不存在，使用默认提示词
		logger.Warn("提示词文件不存在，使用默认提示词", zap.Error(err))
		promptTemplate = []byte(`请从以下字幕中提取%d-%d个有趣的完整故事片段。

要求：
1. 每个片段必须是一个完整的故事，有明确的开头、中间和结尾，能够独立成篇
2. 每个片段必须包含：开始时间、结束时间、标题（简短描述）、内容（详细说明）
3. 时间格式：HH:MM:SS
4. 标题应该简洁明了，能够概括该片段的核心内容和主题
5. 内容应该详细描述片段的情节发展，包括人物、对话、事件等关键要素
6. 输出格式必须是严格的JSON数组，只返回json不返回其他文本,格式如下：
[
  {
    "start_time": "00:01:23",
    "end_time": "00:01:45",
    "title": "精彩开场",
    "content": "这里是该片段的详细内容描述"
  }
]
7. "start_time"与"end_time"之间不能少于45秒，确保片段有足够的长度讲述完整故事
8. 你相中的片段需要向前向后各自多取15秒，确保故事的完整性
9. 避免提取只是几句话的片段，确保每个片段都有完整的情节发展
字幕内容：
%s`)
	}

	// 构建提示词
	prompt := fmt.Sprintf(string(promptTemplate), config.MinSlices, config.MaxSlices, string(subtitleContent))

	// 构建 OpenAI 兼容的 API 请求
	requestBody := ChatRequest{
		Model: config.LLMModel, // 使用配置的模型名称
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
	highlightsPath = strings.Replace(subtitlePath, ".srt", "_highlights.json", 1)

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

// 解析时间字符串为秒数
func parseTimeToSeconds(timeStr string) (int, error) {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("无效的时间格式: %s", timeStr)
	}

	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("解析小时失败: %w", err)
	}

	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("解析分钟失败: %w", err)
	}

	seconds, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, fmt.Errorf("解析秒失败: %w", err)
	}

	return hours*3600 + minutes*60 + seconds, nil
}

// 解析高光时间
func parseHighlightTimes(highlightsPath string) ([]Highlight, error) {
	// 读取高光文件
	highlightsContent, err := os.ReadFile(highlightsPath)
	if err != nil {
		return nil, fmt.Errorf("读取高光文件失败: %w", err)
	}

	// 清理内容，移除可能的BOM和无效字符
	content := strings.TrimSpace(string(highlightsContent))
	content = strings.TrimPrefix(content, "\ufeff") // 移除BOM

	// 检查内容是否为空
	if content == "" {
		return nil, fmt.Errorf("高光文件内容为空")
	}

	// 检查内容的前几个字符，看是否有无效字符
	if len(content) > 0 {
		logger.Info("高光文件内容开头", zap.String("content", content[:min(50, len(content))]))
	}

	// 解析JSON格式的高光数据
	var highlights []Highlight
	if err := json.Unmarshal([]byte(content), &highlights); err != nil {
		return nil, fmt.Errorf("解析高光JSON失败: %w", err)
	}

	// 检查是否有片段
	if len(highlights) == 0 {
		return nil, fmt.Errorf("没有找到高光片段")
	}

	// 记录所有片段（包括短片段）
	for _, highlight := range highlights {
		startSeconds, err := parseTimeToSeconds(highlight.StartTime)
		if err != nil {
			logger.Warn("解析开始时间失败", zap.String("time", highlight.StartTime), zap.Error(err))
			continue
		}

		endSeconds, err := parseTimeToSeconds(highlight.EndTime)
		if err != nil {
			logger.Warn("解析结束时间失败", zap.String("time", highlight.EndTime), zap.Error(err))
			continue
		}

		duration := endSeconds - startSeconds
		logger.Info("添加片段",
			zap.String("title", highlight.Title),
			zap.String("start_time", highlight.StartTime),
			zap.String("end_time", highlight.EndTime),
			zap.Int("duration", duration))
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

// 生成视频切片（不带字幕压制）
func generateSlices(videoPath string, highlights []Highlight) error {
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

	// 检查是否有单独的音频文件
	videoDir := filepath.Dir(videoPath)
	audioPath := ""

	// 查找同目录下的音频文件
	files, err := os.ReadDir(videoDir)
	if err == nil {
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".m4a") {
				audioPath = filepath.Join(videoDir, file.Name())
				logger.Info("使用单独的音频文件", zap.String("audio_path", audioPath))
				break
			}
		}
	}

	// 创建等待组，用于等待所有异步任务完成
	var wg sync.WaitGroup
	// 创建错误通道，用于收集异步任务的错误
	errChan := make(chan error, len(highlights))

	// 为每个高光生成切片
	for i, highlight := range highlights {
		// 使用标题作为文件名，如果没有标题则使用序号
		filename := sanitizeFilename(highlight.Title)
		if filename == "" {
			filename = fmt.Sprintf("highlight_%d", i+1)
		}
		slicePath := filepath.Join(slicesDir, fmt.Sprintf("%s.mp4", filename))

		var cmd *exec.Cmd
		if audioPath != "" {
			// 如果有单独的音频文件，使用双输入
			cmd = exec.Command(ffmpegPath,
				"-i", videoPath,
				"-i", audioPath,
				"-ss", highlight.StartTime,
				"-to", highlight.EndTime,
				"-c:v", "libx264",
				"-crf", "23",
				"-c:a", "aac",
				"-b:a", "192k",
				"-shortest", // 确保音视频长度一致
				"-y",
				slicePath)
		} else {
			// 只有视频文件
			cmd = exec.Command(ffmpegPath,
				"-i", videoPath,
				"-ss", highlight.StartTime,
				"-to", highlight.EndTime,
				"-c:v", "libx264",
				"-crf", "23",
				"-c:a", "aac",
				"-b:a", "192k",
				"-y",
				slicePath)
		}

		// 执行命令并显示实时输出
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("生成切片失败: %w", err)
		}

		logger.Info("切片生成成功",
			zap.String("path", slicePath),
			zap.String("title", highlight.Title),
			zap.String("start_time", highlight.StartTime),
			zap.String("end_time", highlight.EndTime))

		// 异步处理已经生成的切片，使用whisper生成字幕
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			logger.Info("开始异步处理切片字幕", zap.String("path", path))
			// 生成字幕
			_, err := generateSubtitles(path)
			if err != nil {
				logger.Error("异步生成字幕失败", zap.String("path", path), zap.Error(err))
				errChan <- err
				return
			}
			logger.Info("异步处理切片字幕完成", zap.String("path", path))
		}(slicePath)
	}

	// 等待所有异步任务完成
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// 收集错误
	for err := range errChan {
		if err != nil {
			logger.Error("异步处理中发生错误", zap.Error(err))
			// 这里只记录错误，不返回，因为主要的切片生成已经完成
		}
	}

	return nil
}

// 压制片头到切片
func addIntroToSlice(slicePath string, introPath string) (string, error) {
	// 确保 ffmpeg 可用
	ffmpegPath, err := ensureFFmpeg()
	if err != nil {
		return "", fmt.Errorf("确保 ffmpeg 可用失败: %w", err)
	}

	tempPath := filepath.Join(filepath.Dir(slicePath), fmt.Sprintf("%s_intro.mp4", strings.TrimSuffix(filepath.Base(slicePath), filepath.Ext(slicePath))))

	// 压制片头
	cmd := exec.Command(ffmpegPath,
		"-i", introPath,
		"-i", slicePath,
		"-filter_complex", "[0:v][0:a][1:v][1:a]concat=n=2:v=1:a=1",
		"-c:v", "libx264",
		"-crf", "23",
		"-c:a", "aac",
		"-b:a", "192k",
		"-y",
		tempPath)

	// 执行命令并显示实时输出
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("压制片头失败: %w", err)
	}

	return tempPath, nil
}

// 为切片生成字幕并压制
func addSubtitlesToSlices(slicesDir string) error {
	logger.Info("开始为切片添加字幕")

	// 确保 ffmpeg 可用
	ffmpegPath, err := ensureFFmpeg()
	if err != nil {
		return fmt.Errorf("确保 ffmpeg 可用失败: %w", err)
	}

	// 读取切片目录
	files, err := os.ReadDir(slicesDir)
	if err != nil {
		return fmt.Errorf("读取切片目录失败: %w", err)
	}

	// 为每个切片生成字幕并压制
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".mp4") {
			slicePath := filepath.Join(slicesDir, file.Name())
			tempSlicePath := filepath.Join(slicesDir, fmt.Sprintf("%s_temp.mp4", strings.TrimSuffix(file.Name(), ".mp4")))

			// 生成字幕
			subtitlePath, err := generateSubtitles(slicePath)
			if err != nil {
				logger.Error("为切片生成字幕失败", zap.String("path", slicePath), zap.Error(err))
				continue
			}

			// 压制字幕到切片
			subtitlePathForFFmpeg := strings.ReplaceAll(subtitlePath, "\\", "/")
			subtitleFilter := fmt.Sprintf("subtitles=%s", subtitlePathForFFmpeg)

			cmd := exec.Command(ffmpegPath,
				"-i", slicePath,
				"-vf", subtitleFilter,
				"-c:v", "libx264",
				"-crf", "23",
				"-c:a", "aac",
				"-b:a", "192k",
				"-y",
				tempSlicePath)

			// 执行命令并显示实时输出
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err = cmd.Run()
			if err != nil {
				logger.Error("压制字幕失败", zap.String("path", slicePath), zap.Error(err))
				continue
			}

			// 如果配置了片头文件，压制片头
			if config.IntroPath != "" && _, err := os.Stat(config.IntroPath); err == nil {
				logger.Info("开始压制片头", zap.String("slice_path", tempSlicePath), zap.String("intro_path", config.IntroPath))
				introTempPath, err := addIntroToSlice(tempSlicePath, config.IntroPath)
				if err != nil {
					logger.Error("压制片头失败", zap.String("path", tempSlicePath), zap.Error(err))
					// 继续执行，不中断
				} else {
					// 替换临时文件
					if err := os.Remove(tempSlicePath); err != nil {
						logger.Error("删除临时文件失败", zap.String("path", tempSlicePath), zap.Error(err))
					}
					tempSlicePath = introTempPath
				}
			}

			// 替换原文件
			if err := os.Remove(slicePath); err != nil {
				logger.Error("删除原切片文件失败", zap.String("path", slicePath), zap.Error(err))
				continue
			}

			if err := os.Rename(tempSlicePath, slicePath); err != nil {
				logger.Error("重命名临时文件失败", zap.String("path", tempSlicePath), zap.Error(err))
				continue
			}

			logger.Info("为切片添加字幕成功", zap.String("path", slicePath))
		}
	}

	return nil
}

// 主函数
func main() {
	// 输出版权信息
	fmt.Println("========================================")
	fmt.Println("GoClip - 视频切片工具")
	fmt.Println("作者：皖月清风")
	fmt.Println("开源协议：MIT")
	fmt.Println("本项目开源免费，请勿从二道贩子处购买")
	fmt.Println("========================================")

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
	slicesDir := filepath.Join(filepath.Dir(videoPath), "slices")
	if err := generateSlices(videoPath, highlights); err != nil {
		logger.Error("生成视频切片失败", zap.Error(err))
		os.Exit(1)
	}

	// 为切片生成字幕并压制
	if err := addSubtitlesToSlices(slicesDir); err != nil {
		logger.Error("为切片添加字幕失败", zap.Error(err))
		// 继续执行，不退出
	}

	logger.Info("所有任务完成",
		zap.String("video_path", videoPath),
		zap.String("subtitle_path", subtitlePath),
		zap.String("highlights_path", highlightsPath),
		zap.String("slices_dir", slicesDir))

	fmt.Printf("视频处理完成！\n")
	fmt.Printf("视频路径: %s\n", videoPath)
	fmt.Printf("字幕路径: %s\n", subtitlePath)
	fmt.Printf("高光路径: %s\n", highlightsPath)
	fmt.Printf("切片目录: %s\n", filepath.Join(filepath.Dir(videoPath), "slices"))
}
