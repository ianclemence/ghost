package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

// VideoFramesTool extracts frames or short clips from videos using ffmpeg.
// Adapted from OpenClaw's video-frames skill.
type VideoFramesTool struct {
	workspace string
	restrict  bool
}

func NewVideoFramesTool(workspace string, restrict bool) *VideoFramesTool {
	return &VideoFramesTool{
		workspace: workspace,
		restrict:  restrict,
	}
}

func (t *VideoFramesTool) Name() string {
	return "video_frames"
}

func (t *VideoFramesTool) Description() string {
	return "Extract a single frame or a series of frames from a video at a specific timestamp using ffmpeg."
}

func (t *VideoFramesTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"video_path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the video file",
			},
			"timestamp": map[string]interface{}{
				"type":        "string",
				"description": "Timestamp to extract the frame from (format: HH:MM:SS or SS.mmm). Default is 00:00:01.",
			},
			"output_path": map[string]interface{}{
				"type":        "string",
				"description": "Optional custom output path for the extracted frame (JPG or PNG). If not provided, a temporary file will be created.",
			},
		},
		"required": []string{"video_path"},
	}
}

func (t *VideoFramesTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	videoPath, _ := args["video_path"].(string)
	timestamp, ok := args["timestamp"].(string)
	if !ok || timestamp == "" {
		timestamp = "00:00:01"
	}

	resolvedVideoPath, err := validatePath(videoPath, t.workspace, t.restrict)
	if err != nil {
		return ErrorResult(err.Error())
	}

	outputPath, ok := args["output_path"].(string)
	if !ok || outputPath == "" {
		// Create a temporary file in the workspace or system temp
		tempDir := filepath.Join(t.workspace, "tmp", "frames")
		os.MkdirAll(tempDir, 0755)
		outputPath = filepath.Join(tempDir, fmt.Sprintf("frame_%d.jpg", time.Now().UnixNano()))
	} else {
		outputPath, err = validatePath(outputPath, t.workspace, t.restrict)
		if err != nil {
			return ErrorResult(err.Error())
		}
	}

	// Ensure ffmpeg is installed
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return ErrorResult("ffmpeg is not installed on this system. Please install it to use this tool.")
	}

	// ffmpeg command to extract a single frame
	// -ss [timestamp] : seek to timestamp
	// -i [input] : input file
	// -vframes 1 : extract only one frame
	// -q:v 2 : quality (2-5 is good)
	// -y : overwrite output
	cmd := exec.CommandContext(ctx, "ffmpeg", "-ss", timestamp, "-i", resolvedVideoPath, "-vframes", "1", "-q:v", "2", "-y", outputPath)

	logger.InfoCF("video_frames", "Executing ffmpeg", map[string]interface{}{
		"video": resolvedVideoPath,
		"out":   outputPath,
		"time":  timestamp,
	})

	output, err := cmd.CombinedOutput()
	if err != nil {
		return ErrorResult(fmt.Sprintf("ffmpeg failed: %v\nOutput: %s", err, string(output)))
	}

	relPath, _ := filepath.Rel(t.workspace, outputPath)
	return NewToolResult(fmt.Sprintf("Frame successfully extracted from %s at %s and saved to %s", videoPath, timestamp, relPath))
}
