package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type SubtitleItem struct {
	Index    int
	Start    float64
	End      float64
	Text     string
}

type SubtitleSegment struct {
	Items    []SubtitleItem
	Offset   float64
}

func (si *SubtitleItem) Duration() float64 {
	return si.End - si.Start
}

func GenerateSubtitlesParallel(videoPath string) (string, error) {
	logger.Info("开始生成字幕(单文件模式)", zap.String("video_path", videoPath))

	videoDir := filepath.Dir(videoPath)
	audioPath := videoPath

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

	videoName := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	expectedSubtitlePath := filepath.Join(videoDir, videoName+".srt")

	if strings.Contains(videoDir, "slices") {
		logger.Info("为切片重新生成字幕", zap.String("path", expectedSubtitlePath))
	} else if _, err := os.Stat(expectedSubtitlePath); err == nil {
		logger.Info("字幕文件已存在，跳过生成步骤", zap.String("path", expectedSubtitlePath))
		return expectedSubtitlePath, nil
	}

	return generateSubtitleForSingleFile(audioPath, videoDir)
}

func generateSubtitlesForSegments(segments []string, startTimes []int, outputPath string) (string, error) {
	logger.Info("开始并行处理片段", zap.Int("segment_count", len(segments)))

	type segmentResult struct {
		Index    int
		Subtitles []SubtitleItem
		Error    error
	}

	results := make(chan segmentResult, len(segments))
	var wg sync.WaitGroup

	maxConcurrency := 2
	semaphore := make(chan struct{}, maxConcurrency)

	for i, segment := range segments {
		wg.Add(1)
		go func(idx int, segPath string) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			subPath, err := generateSubtitleForSingleFile(segPath, filepath.Dir(segPath))
			if err != nil {
				results <- segmentResult{Index: idx, Error: err}
				return
			}

			items, err := parseSubtitleFile(subPath)
			if err != nil {
				results <- segmentResult{Index: idx, Error: fmt.Errorf("解析字幕失败: %w", err)}
				return
			}

			results <- segmentResult{Index: idx, Subtitles: items}
		}(i, segment)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	segmentResults := make([]segmentResult, len(segments))
	for result := range results {
		segmentResults[result.Index] = result
	}

	for _, result := range segmentResults {
		if result.Error != nil {
			return "", fmt.Errorf("片段处理失败: %w", result.Error)
		}
	}

	logger.Info("所有片段处理完成，开始合并字幕")

	var allItems []SubtitleItem
	for i, result := range segmentResults {
		offset := float64(startTimes[i])
		for _, item := range result.Subtitles {
			adjustedItem := SubtitleItem{
				Start: item.Start + offset,
				End:   item.End + offset,
				Text:  item.Text,
			}
			allItems = append(allItems, adjustedItem)
		}
	}

	allItems = deduplicateAndFixTimestamps(allItems)

	if err := writeSubtitleFile(allItems, outputPath); err != nil {
		return "", fmt.Errorf("写入字幕文件失败: %w", err)
	}

	logger.Info("字幕合并成功", zap.String("path", outputPath))
	return outputPath, nil
}

func generateSubtitleForSingleFile(audioPath, outputDir string) (string, error) {
	ffmpegPath, err := ensureFFmpeg()
	if err != nil {
		return "", fmt.Errorf("确保 ffmpeg 可用失败: %w", err)
	}

	whisperPath, err := ensureWhisper()
	if err != nil {
		return "", fmt.Errorf("确保 Whisper 可用失败: %w", err)
	}

	originalAudioPath := audioPath
	isVideo := false
	videoExtensions := []string{".mp4", ".avi", ".mov", ".mkv", ".wmv"}
	fileExt := strings.ToLower(filepath.Ext(audioPath))
	for _, ext := range videoExtensions {
		if fileExt == ext {
			isVideo = true
			break
		}
	}

	var tempAudioPath string
	if isVideo {
		tempAudioPath = filepath.Join(outputDir, "temp_audio_"+strconv.FormatInt(time.Now().UnixNano(), 10)+".wav")
		logger.Info("提取视频中的音频", zap.String("video_path", audioPath), zap.String("audio_path", tempAudioPath))

		extractCmd := exec.Command(ffmpegPath,
			"-hwaccel", "cuda",
			"-i", audioPath,
			"-vn",
			"-acodec", "pcm_s16le",
			"-ar", "16000",
			"-ac", "1",
			"-y",
			tempAudioPath)

		extractCmd.Stdout = os.Stdout
		extractCmd.Stderr = os.Stderr
		err = extractCmd.Run()
		if err != nil {
			return "", fmt.Errorf("提取音频失败: %w", err)
		}

		audioPath = tempAudioPath
		defer func() {
			if tempAudioPath != "" {
				os.Remove(tempAudioPath)
			}
		}()
	}

	modelDir := filepath.Join(config.OutputDir, "models")
	videoName := strings.TrimSuffix(filepath.Base(originalAudioPath), filepath.Ext(originalAudioPath))
	expectedSubtitlePath := filepath.Join(outputDir, videoName+".srt")

	cmd := exec.Command(whisperPath,
		audioPath,
		"--model", config.WhisperModel,
		"--model_dir", modelDir,
		"--output_format", "srt",
		"--output_dir", outputDir,
		"--language", config.Language)

	env := os.Environ()
	ffmpegDir := filepath.Dir(ffmpegPath)
	env = append(env, fmt.Sprintf("PATH=%s;%s", ffmpegDir, os.Getenv("PATH")))
	cmd.Env = env

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("生成字幕失败: %w", err)
	}

	if _, err := os.Stat(expectedSubtitlePath); os.IsNotExist(err) {
		return "", fmt.Errorf("未找到生成的字幕文件")
	}

	logger.Info("字幕生成成功", zap.String("path", expectedSubtitlePath))
	return expectedSubtitlePath, nil
}

func parseSubtitleFile(path string) ([]SubtitleItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var items []SubtitleItem
	scanner := bufio.NewScanner(file)

	var currentItem SubtitleItem
	var inText bool

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if inText && currentItem.Text != "" {
				items = append(items, currentItem)
				currentItem = SubtitleItem{}
				inText = false
			}
			continue
		}

		if _, err := strconv.Atoi(line); err == nil {
			continue
		}

		if strings.Contains(line, " --> ") {
			start, end, err := parseSubtitleTime(line)
			if err == nil {
				currentItem.Start = start
				currentItem.End = end
				inText = true
			}
			continue
		}

		if inText {
			if currentItem.Text == "" {
				currentItem.Text = line
			} else {
				currentItem.Text += "\n" + line
			}
		}
	}

	if inText && currentItem.Text != "" {
		items = append(items, currentItem)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func deduplicateAndFixTimestamps(items []SubtitleItem) []SubtitleItem {
	if len(items) == 0 {
		return items
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Start != items[j].Start {
			return items[i].Start < items[j].Start
		}
		return items[i].End < items[j].End
	})

	result := []SubtitleItem{items[0]}

	for i := 1; i < len(items); i++ {
		last := &result[len(result)-1]
		current := items[i]

		minEnd := last.End
		if current.End < minEnd {
			minEnd = current.End
		}
		maxStart := last.Start
		if current.Start > maxStart {
			maxStart = current.Start
		}
		overlap := minEnd - maxStart
		overlapRatio := overlap / last.Duration()

		if overlapRatio > 0.8 && strings.TrimSpace(last.Text) == strings.TrimSpace(current.Text) {
			continue
		}

		if current.Start < last.End {
			current.Start = last.End
			if current.Start >= current.End {
				current.End = current.Start + 0.1
			}
		}

		result = append(result, current)
	}

	for i := range result {
		result[i].Index = i + 1
	}

	return result
}

func writeSubtitleFile(items []SubtitleItem, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	for _, item := range items {
		fmt.Fprintf(writer, "%d\n", item.Index)
		fmt.Fprintf(writer, "%s --> %s\n", secondsToTimeStr(item.Start), secondsToTimeStr(item.End))
		fmt.Fprintf(writer, "%s\n\n", item.Text)
	}

	return nil
}