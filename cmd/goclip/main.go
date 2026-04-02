// GoClip - 视频切片工具
// 作者：皖月清风
// 开源协议：MIT
// 本项目开源免费，请勿从二道贩子处购买
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
		"--no-check-certificate", // 跳过证书检查
		"--verbose",              // 显示详细输出
		url)

	// 执行命令并显示实时输出
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		// 检查是否是 Bilibili 提取器错误
		if strings.Contains(url, "bilibili.com") {
			fmt.Println("\n🔧 提示：Bilibili 视频解析失败")
			fmt.Println("可能的原因：")
			fmt.Println("1. yt-dlp 版本过旧，请更新到最新版本")
			fmt.Println("2. Bilibili 网站结构变化导致提取器失效")
			fmt.Println("3. 视频可能需要登录或权限")
			fmt.Println("\n建议：")
			fmt.Println("- 尝试使用其他视频平台的链接")
			fmt.Println("- 手动下载视频后使用本地文件模式")
		}
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

// 分割长音频
// 根据Whisper官方建议，将长音频分割为30分钟左右的片段以获得最佳转录质量
func splitLongAudio(audioPath string) ([]string, error) {
	// 获取音频时长
	duration, err := getMediaDuration(audioPath)
	if err != nil {
		return nil, fmt.Errorf("获取音频时长失败: %w", err)
	}

	// 如果音频时长小于30分钟，不需要分割
	if duration < 30*60 {
		return []string{audioPath}, nil
	}

	logger.Info("音频时长较长，开始分割", zap.Int("duration", duration))
	fmt.Println("检测到长音频，正在按Whisper建议的30分钟时长进行分段处理...")

	// 确保 ffmpeg 可用
	ffmpegPath, err := ensureFFmpeg()
	if err != nil {
		return nil, fmt.Errorf("确保 ffmpeg 可用失败: %w", err)
	}

	// 创建分割目录
	splitDir := filepath.Join(filepath.Dir(audioPath), "split")
	if err := os.MkdirAll(splitDir, 0755); err != nil {
		return nil, fmt.Errorf("创建分割目录失败: %w", err)
	}

	// 分割时长（30分钟）- 符合Whisper官方建议
	splitDuration := 30 * 60
	var segments []string

	// 计算需要分割的段数
	segmentsCount := (duration + splitDuration - 1) / splitDuration

	// 分割音频
	for i := 0; i < segmentsCount; i++ {
		startTime := i * splitDuration
		endTime := (i + 1) * splitDuration
		if endTime > duration {
			endTime = duration
		}

		// 生成输出文件名
		baseName := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		segmentPath := filepath.Join(splitDir, fmt.Sprintf("%s_part_%d.wav", baseName, i+1))

		// 构建 ffmpeg 命令
		cmd := exec.Command(ffmpegPath,
			"-i", audioPath,
			"-ss", fmt.Sprintf("%d", startTime),
			"-to", fmt.Sprintf("%d", endTime),
			"-c:a", "pcm_s16le",
			"-ar", "16000",
			"-ac", "1",
			"-y",
			segmentPath)

		// 执行命令并显示实时输出
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			return nil, fmt.Errorf("分割音频失败: %w", err)
		}

		logger.Info("音频分割成功",
			zap.String("path", segmentPath),
			zap.Int("start_time", startTime),
			zap.Int("end_time", endTime))

		segments = append(segments, segmentPath)
	}

	return segments, nil
}

// 获取媒体文件时长（秒）
func getMediaDuration(filePath string) (int, error) {
	// 确保 ffmpeg 可用
	ffmpegPath, err := ensureFFmpeg()
	if err != nil {
		return 0, fmt.Errorf("确保 ffmpeg 可用失败: %w", err)
	}

	// 构建更简单的 ffmpeg 命令来获取时长
	// 移除可能导致问题的参数
	cmd := exec.Command(ffmpegPath, "-i", filePath, "-f", "null", "-")

	// 执行命令并获取输出
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 打印错误信息以便调试
		logger.Error("ffmpeg 命令执行失败", zap.String("file_path", filePath), zap.String("output", string(output)), zap.Error(err))

		// 尝试使用另一种方法获取时长
		cmd = exec.Command(ffmpegPath, "-i", filePath)
		output, err = cmd.CombinedOutput()
		if err != nil {
			logger.Error("ffmpeg 命令再次执行失败", zap.String("file_path", filePath), zap.String("output", string(output)), zap.Error(err))
			return 0, fmt.Errorf("获取媒体时长失败: %w", err)
		}
	}

	// 从输出中提取时长信息
	outputStr := string(output)
	// 查找类似 "Duration: 00:05:23.45, start: 0.000000, bitrate: 128 kb/s" 的行
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Duration:") {
			// 提取时长部分
			parts := strings.Split(line, " ")
			for _, part := range parts {
				if strings.Contains(part, ":") && strings.Contains(part, ".") {
					// 解析时长字符串
					timeParts := strings.Split(part, ":")
					if len(timeParts) == 3 {
						hours, _ := strconv.Atoi(timeParts[0])
						minutes, _ := strconv.Atoi(timeParts[1])
						secondsStr := strings.Split(timeParts[2], ",")[0]
						seconds, _ := strconv.ParseFloat(secondsStr, 64)
						totalSeconds := hours*3600 + minutes*60 + int(seconds)
						return totalSeconds, nil
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("未从ffmpeg输出中找到时长信息")
}

// 分割长视频
// 根据Whisper官方建议，将长视频分割为30分钟左右的片段以获得最佳转录质量
func splitLongVideo(videoPath string) ([]string, error) {
	// 获取视频时长
	duration, err := getMediaDuration(videoPath)
	if err != nil {
		return nil, fmt.Errorf("获取视频时长失败: %w", err)
	}

	// 如果视频时长小于30分钟，不需要分割
	if duration < 30*60 {
		return []string{videoPath}, nil
	}

	logger.Info("视频时长较长，开始分割", zap.Int("duration", duration))
	fmt.Println("检测到长视频，正在按Whisper建议的30分钟时长进行分段处理...")

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

	// 分割时长（30分钟）- 符合Whisper官方建议
	splitDuration := 30 * 60
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

	// 对于非切片文件，先检查是否有单独的音频文件
	if !strings.Contains(videoDir, "slices") && !strings.Contains(videoDir, "split") {
		// 分割长音频或视频
		var segments []string
		var err error

		if audioPath != videoPath {
			// 如果有单独的音频文件，分割音频
			logger.Info("使用单独的音频文件，开始分割", zap.String("audio_path", audioPath))
			segments, err = splitLongAudio(audioPath)
		} else {
			// 分割长视频 - 使用原始视频路径，避免使用音频文件路径
			segments, err = splitLongVideo(videoPath)
		}

		if err != nil {
			// 如果分割失败，尝试直接处理原始文件（不分割）
			logger.Warn("分割失败，尝试直接处理原始文件", zap.Error(err))
			segments = []string{audioPath}
		}

		// 为每个片段生成字幕然后合并
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

			logger.Info("长文件字幕生成成功", zap.String("path", expectedSubtitlePath))
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

	// 保存原始文件路径，用于生成字幕文件名
	originalAudioPath := audioPath

	// 检查是否为视频文件，如果是则提取音频
	isVideo := false
	videoExtensions := []string{".mp4", ".avi", ".mov", ".mkv", ".wmv"}
	fileExt := strings.ToLower(filepath.Ext(audioPath))
	for _, ext := range videoExtensions {
		if fileExt == ext {
			isVideo = true
			break
		}
	}

	// 如果是视频文件，提取音频
	if isVideo {
		tempAudioPath := filepath.Join(filepath.Dir(audioPath), "temp_audio.wav")
		logger.Info("提取视频中的音频", zap.String("video_path", audioPath), zap.String("audio_path", tempAudioPath))

		// 使用 ffmpeg 提取音频
		extractCmd := exec.Command(ffmpegPath,
			"-i", audioPath,
			"-vn",                  // 禁用视频
			"-acodec", "pcm_s16le", // 无损音频
			"-ar", "16000", // 16kHz 采样率
			"-ac", "1", // 单声道
			"-y",
			tempAudioPath)

		extractCmd.Stdout = os.Stdout
		extractCmd.Stderr = os.Stderr
		err = extractCmd.Run()
		if err != nil {
			return "", fmt.Errorf("提取音频失败: %w", err)
		}

		// 使用提取的音频文件
		audioPath = tempAudioPath
		defer os.Remove(tempAudioPath) // 清理临时文件
	}

	// 构建 whisper 命令
	// 指定模型目录，让 Whisper 在 output/models 目录中查找和下载模型
	modelDir := filepath.Join(config.OutputDir, "models")

	// 构建 whisper 命令，使用原始文件的目录作为输出目录
	cmd := exec.Command(whisperPath,
		audioPath,
		"--model", config.WhisperModel,
		"--model_dir", modelDir,
		"--output_format", "srt",
		"--output_dir", filepath.Dir(originalAudioPath),
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

	// 查找生成的字幕文件 - 使用原始文件路径生成文件名
	videoName = strings.TrimSuffix(filepath.Base(originalAudioPath), filepath.Ext(originalAudioPath))
	expectedSubtitlePath = filepath.Join(videoDir, videoName+".srt")

	// 打印目录内容进行调试
	files, err = os.ReadDir(videoDir)
	if err != nil {
		return "", fmt.Errorf("读取目录失败: %w", err)
	}

	logger.Info("目录内容", zap.Int("file_count", len(files)))
	// 只查找与原始音频文件同名的字幕文件
	targetSubtitleName := strings.TrimSuffix(filepath.Base(originalAudioPath), filepath.Ext(originalAudioPath)) + ".srt"
	for _, file := range files {
		logger.Info("文件", zap.String("name", file.Name()), zap.Bool("is_dir", file.IsDir()))
		if strings.HasSuffix(file.Name(), ".srt") {
			// 只选择与原始音频文件同名的字幕文件
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
		promptTemplate = []byte(`请从以下字幕中提取%d-%d个有趣的完整片段。

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
    "end_time": "00:02:23",
    "title": "精彩开场",
    "content": "这里是该片段的详细内容描述"
  }
]
7. "start_time"与"end_time"之间不能少于60秒，确保片段有足够的长度讲述完整故事
8. 你相中的片段需要向前向后各自多取15秒，确保故事的完整性
9. 避免提取只是几句话的片段，确保每个片段都有完整的情节发展
字幕内容：
%s`)
	}

	// 构建提示词模板
	promptTemplateStr := string(promptTemplate)
	// 计算提示词模板的基础长度（不包含字幕内容）
	basePromptLength := len(fmt.Sprintf(promptTemplateStr, config.MinSlices, config.MaxSlices, ""))
	// API限制的最大输入长度
	maxAPILength := 30720
	// 计算字幕内容的最大允许长度
	maxSubtitleLength := maxAPILength - basePromptLength

	// 处理字幕内容
	subtitleContentStr := string(subtitleContent)
	var allHighlights []Highlight

	if len(subtitleContentStr) <= maxSubtitleLength {
		// 字幕内容长度在限制范围内，直接处理
		highlights, err := generateHighlightsForSegment(subtitleContentStr, promptTemplateStr, config.MinSlices, config.MaxSlices)
		if err != nil {
			return "", err
		}
		allHighlights = highlights
	} else {
		// 字幕内容过长，需要分段处理
		logger.Info("字幕内容过长，开始分段处理", zap.Int("total_length", len(subtitleContentStr)))

		// 分段处理
		segments := splitSubtitleContent(subtitleContentStr, maxSubtitleLength)
		logger.Info("字幕分段完成", zap.Int("segment_count", len(segments)))

		// 对每个分段生成高光
		for i, segment := range segments {
			logger.Info("处理字幕分段", zap.Int("segment_index", i+1), zap.Int("segment_length", len(segment)))
			highlights, err := generateHighlightsForSegment(segment, promptTemplateStr, config.MinSlices, config.MaxSlices)
			if err != nil {
				logger.Error("处理字幕分段失败", zap.Int("segment_index", i+1), zap.Error(err))
				// 继续处理下一个分段，不中断
				continue
			}
			allHighlights = append(allHighlights, highlights...)
		}
	}

	// 去重处理，避免重复的高光片段
	allHighlights = deduplicateHighlights(allHighlights)

	// 限制高光数量
	if len(allHighlights) > config.MaxSlices {
		allHighlights = allHighlights[:config.MaxSlices]
	}

	// 生成高光文件
	highlightsPath = strings.Replace(subtitlePath, ".srt", "_highlights.json", 1)

	// 序列化高光数据
	highlightsJSON, err := json.MarshalIndent(allHighlights, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化高光数据失败: %w", err)
	}

	// 写入高光文件
	if err := os.WriteFile(highlightsPath, highlightsJSON, 0644); err != nil {
		return "", fmt.Errorf("写入高光文件失败: %w", err)
	}

	logger.Info("高光生成成功", zap.String("path", highlightsPath), zap.Int("highlight_count", len(allHighlights)))
	return highlightsPath, nil
}

// 为字幕分段生成高光
func generateHighlightsForSegment(subtitleSegment string, promptTemplate string, minSlices int, maxSlices int) ([]Highlight, error) {
	// 构建提示词
	prompt := fmt.Sprintf(promptTemplate, minSlices, maxSlices, subtitleSegment)

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
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", config.LLMURL+"/chat/completions", bytes.NewBuffer(requestJSON))
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.APIKey)

	// 发送请求
	client := &http.Client{}
	logger.Info("发送 API 请求",
		zap.String("url", config.LLMURL+"/chat/completions"),
		zap.Int("request_body_length", len(requestJSON)),
		zap.String("model", config.LLMModel))

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("发送 API 请求失败", zap.Error(err))
		return nil, fmt.Errorf("发送 API 请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("读取响应体失败", zap.Error(err))
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		logger.Error("API 请求失败",
			zap.Int("status_code", resp.StatusCode),
			zap.String("response_body", string(respBody)))
		return nil, fmt.Errorf("API 请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(respBody))
	}

	// 重置响应体，因为已经被读取过了
	resp.Body = io.NopCloser(strings.NewReader(string(respBody)))

	// 解析响应
	var response ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 提取高光内容
	var highlightsContent string
	if len(response.Choices) > 0 {
		highlightsContent = response.Choices[0].Message.Content
		// 清理可能的 markdown 代码块标记
		highlightsContent = strings.TrimPrefix(highlightsContent, "```json")
		highlightsContent = strings.TrimPrefix(highlightsContent, "```")
		highlightsContent = strings.TrimSuffix(highlightsContent, "```")
		highlightsContent = strings.TrimSpace(highlightsContent)
	}

	// 解析高光数据
	var highlights []Highlight
	if highlightsContent != "" {
		if err := json.Unmarshal([]byte(highlightsContent), &highlights); err != nil {
			logger.Error("解析高光JSON失败", zap.Error(err))
			return nil, fmt.Errorf("解析高光JSON失败: %w", err)
		}
	}

	return highlights, nil
}

// 分割字幕内容
func splitSubtitleContent(content string, maxLength int) []string {
	var segments []string
	currentPos := 0

	for currentPos < len(content) {
		// 计算当前分段的结束位置
		endPos := currentPos + maxLength
		if endPos > len(content) {
			endPos = len(content)
		}

		// 确保分割点在合适的位置，避免破坏字幕格式
		// 找到最后一个完整的字幕块结束
		lastEmptyLine := strings.LastIndex(content[currentPos:endPos], "\n\n")
		if lastEmptyLine != -1 {
			endPos = currentPos + lastEmptyLine + 2
		}

		// 添加分段
		segments = append(segments, content[currentPos:endPos])
		currentPos = endPos
	}

	return segments
}

// 去重高光片段
func deduplicateHighlights(highlights []Highlight) []Highlight {
	// 使用map去重
	highlightMap := make(map[string]Highlight)

	for _, highlight := range highlights {
		// 使用开始时间和结束时间作为唯一标识
		key := highlight.StartTime + "-" + highlight.EndTime
		highlightMap[key] = highlight
	}

	// 转换回切片
	var result []Highlight
	for _, highlight := range highlightMap {
		result = append(result, highlight)
	}

	return result
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

// 根据时间戳从原始字幕文件中截取字幕
// offset: 累计偏移量（秒），字幕时间将以此为基础进行偏移
func extractSubtitlesByTimestamp(originalSubtitlePath string, startTime string, endTime string, outputSubtitlePath string, offset int) error {
	logger.Info("开始根据时间戳截取字幕",
		zap.String("original_subtitle", originalSubtitlePath),
		zap.String("start_time", startTime),
		zap.String("end_time", endTime),
		zap.String("output_subtitle", outputSubtitlePath),
		zap.Int("offset", offset))

	// 解析开始和结束时间
	startSeconds, err := parseTimeToSeconds(startTime)
	if err != nil {
		return fmt.Errorf("解析开始时间失败: %w", err)
	}

	endSeconds, err := parseTimeToSeconds(endTime)
	if err != nil {
		return fmt.Errorf("解析结束时间失败: %w", err)
	}

	// 读取原始字幕文件
	subtitleContent, err := os.ReadFile(originalSubtitlePath)
	if err != nil {
		return fmt.Errorf("读取原始字幕文件失败: %w", err)
	}

	// 解析字幕文件
	lines := strings.Split(string(subtitleContent), "\n")
	var extractedSubtitles []string
	var currentSubtitle []string
	var currentStart, currentEnd int
	var inSubtitle bool

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			// 字幕块结束
			if inSubtitle {
				// 检查当前字幕是否与时间范围有重叠
				if currentStart < endSeconds && currentEnd > startSeconds {
					// 调整时间戳：
					// 1. 先计算相对于切片开始时间的偏移
					// 2. 再加上累计偏移量
					adjustedStart := offset + (currentStart - startSeconds)
					adjustedEnd := offset + (currentEnd - startSeconds)

					// 确保调整后的时间不为负数
					if adjustedStart < 0 {
						adjustedStart = 0
					}
					if adjustedEnd < 0 {
						adjustedEnd = 0
					}

					// 格式化调整后的时间
					adjustedStartTime := formatSecondsToTime(adjustedStart)
					adjustedEndTime := formatSecondsToTime(adjustedEnd)

					// 替换时间行
					for j, subLine := range currentSubtitle {
						if strings.Contains(subLine, "-->") {
							currentSubtitle[j] = fmt.Sprintf("%s --> %s", adjustedStartTime, adjustedEndTime)
							break
						}
					}

					// 添加到提取的字幕中
					extractedSubtitles = append(extractedSubtitles, currentSubtitle...)
					extractedSubtitles = append(extractedSubtitles, "")
				}
				inSubtitle = false
				currentSubtitle = []string{}
			}
			continue
		}

		if !inSubtitle {
			// 检查是否是序号行
			if _, err := strconv.Atoi(line); err == nil {
				currentSubtitle = append(currentSubtitle, line)
				inSubtitle = true
			}
		} else {
			// 检查是否是时间行
			if strings.Contains(line, "-->") {
				// 解析时间
				timeParts := strings.Split(line, " --> ")
				if len(timeParts) == 2 {
					startTimeStr := strings.Split(timeParts[0], ",")[0]
					endTimeStr := strings.Split(timeParts[1], ",")[0]

					// 转换为秒数
					currentStart, _ = parseSRTTimeToSeconds(startTimeStr)
					currentEnd, _ = parseSRTTimeToSeconds(endTimeStr)
				}
				currentSubtitle = append(currentSubtitle, line)
			} else {
				// 字幕内容
				currentSubtitle = append(currentSubtitle, line)
			}
		}
	}

	// 处理最后一个字幕块
	if inSubtitle {
		if currentStart < endSeconds && currentEnd > startSeconds {
			// 调整时间戳
			adjustedStart := offset + (currentStart - startSeconds)
			adjustedEnd := offset + (currentEnd - startSeconds)

			// 确保调整后的时间不为负数
			if adjustedStart < 0 {
				adjustedStart = 0
			}
			if adjustedEnd < 0 {
				adjustedEnd = 0
			}

			// 格式化调整后的时间
			adjustedStartTime := formatSecondsToTime(adjustedStart)
			adjustedEndTime := formatSecondsToTime(adjustedEnd)

			// 替换时间行
			for j, subLine := range currentSubtitle {
				if strings.Contains(subLine, "-->") {
					currentSubtitle[j] = fmt.Sprintf("%s --> %s", adjustedStartTime, adjustedEndTime)
					break
				}
			}

			// 添加到提取的字幕中
			extractedSubtitles = append(extractedSubtitles, currentSubtitle...)
			extractedSubtitles = append(extractedSubtitles, "")
		}
	}

	// 重新编号字幕
	var numberedSubtitles []string
	var subtitleIndex int = 1
	var currentBlock []string

	for _, line := range extractedSubtitles {
		if line == "" {
			if len(currentBlock) > 0 {
				// 替换序号
				if len(currentBlock) > 0 {
					currentBlock[0] = fmt.Sprintf("%d", subtitleIndex)
					subtitleIndex++
				}
				numberedSubtitles = append(numberedSubtitles, currentBlock...)
				numberedSubtitles = append(numberedSubtitles, "")
				currentBlock = []string{}
			}
		} else {
			currentBlock = append(currentBlock, line)
		}
	}

	// 写入提取的字幕文件
	extractedContent := strings.Join(numberedSubtitles, "\n")
	if err := os.WriteFile(outputSubtitlePath, []byte(extractedContent), 0644); err != nil {
		return fmt.Errorf("写入提取的字幕文件失败: %w", err)
	}

	logger.Info("字幕截取成功", zap.String("output_path", outputSubtitlePath))
	return nil
}

// 解析SRT格式的时间字符串为秒数
func parseSRTTimeToSeconds(timeStr string) (int, error) {
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

	secondsStr := parts[2]
	seconds, err := strconv.ParseFloat(secondsStr, 64)
	if err != nil {
		return 0, fmt.Errorf("解析秒失败: %w", err)
	}

	return hours*3600 + minutes*60 + int(seconds), nil
}

// 将秒数格式化为SRT格式的时间字符串
func formatSecondsToTime(seconds int) string {
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	return fmt.Sprintf("%02d:%02d:%02d,000", hours, minutes, secs)
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

	// 验证并过滤片段，确保每个片段至少60秒
	var validHighlights []Highlight
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
		if duration < 60 {
			logger.Warn("片段时长不足60秒，跳过",
				zap.String("title", highlight.Title),
				zap.String("start_time", highlight.StartTime),
				zap.String("end_time", highlight.EndTime),
				zap.Int("duration", duration))
			continue
		}

		logger.Info("添加片段",
			zap.String("title", highlight.Title),
			zap.String("start_time", highlight.StartTime),
			zap.String("end_time", highlight.EndTime),
			zap.Int("duration", duration))
		validHighlights = append(validHighlights, highlight)
	}

	if len(validHighlights) == 0 {
		return nil, fmt.Errorf("没有找到符合时长要求的高光片段（至少60秒）")
	}

	return validHighlights, nil
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
	}

	return nil
}

// 对原始视频进行字幕压制
func addSubtitlesToOriginalVideo(videoPath string, subtitlePath string) (string, error) {
	logger.Info("开始对原始视频进行字幕压制", zap.String("video_path", videoPath), zap.String("subtitle_path", subtitlePath))

	// 确保 ffmpeg 可用
	ffmpegPath, err := ensureFFmpeg()
	if err != nil {
		return "", fmt.Errorf("确保 ffmpeg 可用失败: %w", err)
	}

	// 生成输出路径
	outputPath := strings.Replace(videoPath, ".mp4", "_subtitled.mp4", 1)
	if outputPath == videoPath {
		// 如果视频不是 mp4 格式，添加 _subtitled 后缀
		ext := filepath.Ext(videoPath)
		outputPath = strings.Replace(videoPath, ext, "_subtitled"+ext, 1)
	}

	// 构建 ffmpeg 命令
	subtitlePathForFFmpeg := strings.ReplaceAll(subtitlePath, "\\", "/")
	subtitleFilter := fmt.Sprintf("subtitles='%s'", subtitlePathForFFmpeg)

	cmd := exec.Command(ffmpegPath,
		"-i", videoPath,
		"-vf", subtitleFilter,
		"-c:v", "libx264",
		"-crf", "23",
		"-c:a", "aac",
		"-b:a", "192k",
		"-y",
		outputPath)

	// 执行命令并显示实时输出
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("压制字幕失败: %w", err)
	}

	logger.Info("原始视频字幕压制成功", zap.String("path", outputPath))
	return outputPath, nil
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
func addSubtitlesToSlices(slicesDir string, originalSubtitlePath string, highlightsPath string) error {
	logger.Info("开始为切片添加字幕")

	// 确保 ffmpeg 可用
	ffmpegPath, err := ensureFFmpeg()
	if err != nil {
		return fmt.Errorf("确保 ffmpeg 可用失败: %w", err)
	}

	// 读取高光文件，获取每个切片的时间戳
	highlights, err := parseHighlightTimes(highlightsPath)
	if err != nil {
		return fmt.Errorf("解析高光文件失败: %w", err)
	}

	// 获取片头时长（如果配置了片头文件）
	introDuration := 0
	if config.IntroPath != "" {
		if _, err := os.Stat(config.IntroPath); err == nil {
			duration, err := getMediaDuration(config.IntroPath)
			if err != nil {
				logger.Warn("获取片头时长失败，使用默认值0", zap.Error(err))
			} else {
				introDuration = duration
				logger.Info("获取片头时长成功", zap.Int("duration", introDuration))
			}
		}
	}

	// 为每个高光生成的切片添加字幕（按照高光顺序处理）
	currentOffset := introDuration
	for i, highlight := range highlights {
		// 生成高光对应的安全文件名（与generateSlices函数保持一致）
		highlightFilename := sanitizeFilename(highlight.Title)
		if highlightFilename == "" {
			highlightFilename = fmt.Sprintf("highlight_%d", i+1)
		}

		// 构建切片文件路径
		sliceFileName := fmt.Sprintf("%s.mp4", highlightFilename)
		slicePath := filepath.Join(slicesDir, sliceFileName)

		// 检查切片文件是否存在
		if _, err := os.Stat(slicePath); os.IsNotExist(err) {
			logger.Error("切片文件不存在", zap.String("path", slicePath))
			continue
		}

		// 构建临时文件和字幕文件路径
		tempSlicePath := filepath.Join(slicesDir, fmt.Sprintf("%s_temp.mp4", highlightFilename))
		subtitlePath := filepath.Join(slicesDir, fmt.Sprintf("%s.srt", highlightFilename))

		// 从原始字幕文件中截取字幕，传入累计偏移
		err := extractSubtitlesByTimestamp(originalSubtitlePath, highlight.StartTime, highlight.EndTime, subtitlePath, currentOffset)
		if err != nil {
			logger.Error("截取字幕失败", zap.String("path", slicePath), zap.Error(err))
			continue
		}

		// 计算当前高光片段的时长并更新累计偏移
		highlightStart, _ := parseTimeToSeconds(highlight.StartTime)
		highlightEnd, _ := parseTimeToSeconds(highlight.EndTime)
		highlightDuration := highlightEnd - highlightStart
		if highlightDuration > 0 {
			currentOffset += highlightDuration
		}

		// 压制字幕到切片
		subtitlePathForFFmpeg := strings.ReplaceAll(subtitlePath, "\\", "/")
		// 对包含空格的路径用引号括起来
		subtitleFilter := fmt.Sprintf("subtitles='%s'", subtitlePathForFFmpeg)

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
		if config.IntroPath != "" {
			if _, err := os.Stat(config.IntroPath); err == nil {
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

		logger.Info("为切片添加字幕成功", zap.String("path", slicePath), zap.String("title", highlight.Title))
	}

	return nil
}

// 打印项目信息（开始和结束时调用）
func printProjectInfo(isEnd bool) {
	if isEnd {
		fmt.Println("\n========================================")
		fmt.Println("处理完成！感谢使用 GoClip")
		fmt.Println("========================================")
	} else {
		fmt.Println("========================================")
		fmt.Println("GoClip - 视频切片工具")
		fmt.Println("========================================")
	}
	fmt.Println("📢 重要声明：")
	fmt.Println("   本项目完全开源免费，基于 MIT 协议发布")
	fmt.Println("   作者：皖月清风")
	fmt.Println("   如果您从任何渠道付费购买了本软件，请立即申请退款！")
	fmt.Println("   您可能遭遇了诈骗，请勿相信任何收费版本！")
	fmt.Println("")
	fmt.Println("💬 交流QQ群：1092257118")
	fmt.Println("   欢迎加入交流群，获取最新版本和技术支持")
	fmt.Println("========================================")
}

// 主函数
func main() {
	// 输出版权信息
	printProjectInfo(false)

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

	// 启动goroutine对原始视频进行字幕压制
	var subtitledVideoPath string
	var subtitleError error
	subtitleDone := make(chan struct{})

	go func() {
		defer close(subtitleDone)
		subtitledVideoPath, subtitleError = addSubtitlesToOriginalVideo(videoPath, subtitlePath)
		if subtitleError != nil {
			logger.Error("压制原始视频字幕失败", zap.Error(subtitleError))
		}
	}()

	// 同时生成高光
	highlightsPath, err := generateHighlights(subtitlePath)
	if err != nil {
		logger.Error("生成高光失败", zap.Error(err))
		os.Exit(1)
	}

	// 等待字幕压制完成
	<-subtitleDone
	if subtitleError != nil {
		// 如果字幕压制失败，使用原始视频
		logger.Warn("使用原始视频进行切片，因为字幕压制失败", zap.Error(subtitleError))
		subtitledVideoPath = videoPath
	}

	// 解析高光时间
	highlights, err := parseHighlightTimes(highlightsPath)
	if err != nil {
		logger.Error("解析高光时间失败", zap.Error(err))
		os.Exit(1)
	}

	// 生成视频切片
	slicesDir := filepath.Join(filepath.Dir(videoPath), "slices")
	if err := generateSlices(subtitledVideoPath, highlights); err != nil {
		logger.Error("生成视频切片失败", zap.Error(err))
		os.Exit(1)
	}

	// 不需要为切片生成字幕并压制，因为已经在原始视频上压制了字幕

	// 为每个切片添加片头文件
	if config.IntroPath != "" {
		if _, err := os.Stat(config.IntroPath); err == nil {
			logger.Info("开始为切片添加片头文件", zap.String("intro_path", config.IntroPath))

			// 遍历所有切片文件
			files, err := os.ReadDir(slicesDir)
			if err != nil {
				logger.Error("读取切片目录失败", zap.Error(err))
			} else {
				for _, file := range files {
					if !file.IsDir() && strings.HasSuffix(file.Name(), ".mp4") {
						slicePath := filepath.Join(slicesDir, file.Name())
						logger.Info("为切片添加片头", zap.String("slice_path", slicePath))

						// 调用 addIntroToSlice 函数添加片头
						introSlicePath, err := addIntroToSlice(slicePath, config.IntroPath)
						if err != nil {
							logger.Error("为切片添加片头失败", zap.String("slice_path", slicePath), zap.Error(err))
						} else {
							// 替换原文件
							if err := os.Remove(slicePath); err != nil {
								logger.Error("删除原切片文件失败", zap.String("path", slicePath), zap.Error(err))
							} else {
								if err := os.Rename(introSlicePath, slicePath); err != nil {
									logger.Error("重命名临时文件失败", zap.String("path", introSlicePath), zap.Error(err))
								}
							}
						}
					}
				}
			}
		} else {
			logger.Warn("片头文件不存在，跳过添加片头步骤", zap.String("intro_path", config.IntroPath))
		}
	}

	logger.Info("所有任务完成",
		zap.String("video_path", videoPath),
		zap.String("subtitle_path", subtitlePath),
		zap.String("highlights_path", highlightsPath),
		zap.String("slices_dir", slicesDir))

	fmt.Printf("\n视频处理完成！\n")
	fmt.Printf("视频路径: %s\n", videoPath)
	fmt.Printf("字幕路径: %s\n", subtitlePath)
	fmt.Printf("高光路径: %s\n", highlightsPath)
	fmt.Printf("切片目录: %s\n", filepath.Join(filepath.Dir(videoPath), "slices"))

	// 结束时再次输出项目信息
	printProjectInfo(true)
}
