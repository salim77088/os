package transcode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/salim77088/os/api/internal/config"
)

// generateHLS generates HLS segments and playlist from a transcoded video
func (e *Engine) generateHLS(ctx context.Context, inputPath, outputDir string,
	profile config.ResProfile, codec string) error {

	// Create resolution-specific HLS directory
	resDir := filepath.Join(outputDir, profile.Name)
	if err := os.MkdirAll(resDir, 0755); err != nil {
		return fmt.Errorf("failed to create HLS directory: %w", err)
	}

	playlistPath := filepath.Join(resDir, "playlist.m3u8")
	segmentPattern := filepath.Join(resDir, "segment_%05d.ts")

	args := []string{}

	// Hardware acceleration input
	if e.gpuAvailable && e.cfg.ZeroCopy {
		args = append(args, e.buildHWAccelInputArgs(&ProbeData{})...)
	}

	args = append(args,
		"-i", inputPath,
		"-c:v", "copy", // Video already transcoded
		"-c:a", "aac",
		"-b:a", e.cfg.AudioBitrate,
		"-ac", "2",
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", e.cfg.HLSSegmentDuration),
		"-hls_list_size", "0",
		"-hls_segment_filename", segmentPattern,
		"-hls_flags", "independent_segments+append_list",
		"-hls_segment_type", "mpegts",
		"-y", playlistPath,
	)

	cmd := exec.CommandContext(ctx, e.ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("HLS generation failed: %w (output: %s)", err, truncateString(string(output), 500))
	}

	return nil
}

// generateMasterHLSPlaylist creates a master HLS playlist that references all resolution variants
func (e *Engine) generateMasterHLSPlaylist(hlsDir string, outputs []TranscodedOutput) error {
	masterPath := filepath.Join(hlsDir, "master.m3u8")

	// Build master playlist content
	var builder strings.Builder
	builder.WriteString("#EXTM3U\n")
	builder.WriteString("#EXT-X-VERSION:6\n")
	builder.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n\n")

	for _, output := range outputs {
		// Calculate bandwidth from bitrate string
		bandwidth := parseBitrateToBytes(output.Profile.Bitrate) * 8 // Convert to bits per second
		resolution := fmt.Sprintf("%dx%d", output.Profile.Width, output.Profile.Height)

		builder.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d,RESOLUTION=%s,CODECS=\"%s\"\n",
			bandwidth, bandwidth, resolution, getCodecsString()))
		builder.WriteString(fmt.Sprintf("%s/playlist.m3u8\n\n", output.Profile.Name))
	}

	// Write master playlist
	if err := os.WriteFile(masterPath, []byte(builder.String()), 0644); err != nil {
		return fmt.Errorf("failed to write master HLS playlist: %w", err)
	}

	return nil
}

// generateDASH generates DASH segments and manifest from a transcoded video
func (e *Engine) generateDASH(ctx context.Context, sourcePath, outputDir string,
	outputs []TranscodedOutput, codec string, probe *ProbeData) error {

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create DASH directory: %w", err)
	}

	// Build FFmpeg DASH command using multiple inputs
	args := []string{}

	// Add source input for each resolution
	// We'll use the filter_complex approach for multi-resolution DASH
	args = append(args,
		"-i", sourcePath,
	)

	// For multi-resolution DASH, we create a single command with multiple outputs
	// This is more efficient than separate commands
	args = append(args,
		"-map", "0:v:0",
		"-map", "0:v:0",
		"-map", "0:v:0",
		"-map", "0:a:0",
	)

	// Video codec for each representation
	videoCodec := "libx264" // Use H.264 for DASH compatibility
	if e.gpuAvailable {
		videoCodec = e.determineCodec(probe)
		if isHardwareCodec(videoCodec) {
			// For DASH, use software encoding for better compatibility
			videoCodec = "libx264"
		}
	}

	// Add encoding parameters for each video stream
	for i, output := range outputs {
		args = append(args,
			fmt.Sprintf("-c:v:%d", i), videoCodec,
			fmt.Sprintf("-b:v:%d", i), output.Profile.Bitrate,
			fmt.Sprintf("-maxrate:v:%d", i), output.Profile.MaxBitrate,
			fmt.Sprintf("-bufsize:v:%d", i), multiplyBitrate(output.Profile.MaxBitrate, 2),
			fmt.Sprintf("-filter:v:%d", i), fmt.Sprintf("scale=%d:%d", output.Profile.Width, output.Profile.Height),
		)

		if videoCodec == "libx264" {
			args = append(args,
				fmt.Sprintf("-preset:v:%d", i), "fast",
				fmt.Sprintf("-crf:v:%d", i), "23",
			)
		}
	}

	// Audio codec
	args = append(args,
		"-c:a:0", "aac",
		"-b:a:0", e.cfg.AudioBitrate,
	)

	// DASH output options
	args = append(args,
		"-f", "dash",
		"-seg_duration", fmt.Sprintf("%d", e.cfg.DASHSegmentDuration),
		"-adaptation_sets", "id=0,streams=v id=1,streams=a",
		"-dash_segment_type", "mp4",
		"-use_template", "1",
		"-use_timeline", "1",
		"-y", filepath.Join(outputDir, "manifest.mpd"),
	)

	cmd := exec.CommandContext(ctx, e.ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("DASH generation failed: %w (output: %s)", err, truncateString(string(output), 500))
	}

	return nil
}

// GenerateHLSFromSource generates HLS directly from source (for quick streaming without full transcoding)
func (e *Engine) GenerateHLSFromSource(ctx context.Context, sourcePath, outputDir string,
	profile config.ResProfile, codec string) error {

	resDir := filepath.Join(outputDir, profile.Name)
	if err := os.MkdirAll(resDir, 0755); err != nil {
		return fmt.Errorf("failed to create HLS directory: %w", err)
	}

	playlistPath := filepath.Join(resDir, "playlist.m3u8")
	segmentPattern := filepath.Join(resDir, "segment_%05d.ts")

	args := []string{}

	// Hardware acceleration
	if e.gpuAvailable && e.cfg.ZeroCopy {
		args = append(args, e.buildHWAccelInputArgs(&ProbeData{})...)
	}

	args = append(args, "-i", sourcePath)

	// Video codec
	args = append(args, e.buildVideoCodecArgs(codec, profile)...)

	// Scale filter
	args = append(args, e.buildScaleFilter(profile, codec)...)

	// Audio
	args = append(args,
		"-c:a", "aac",
		"-b:a", e.cfg.AudioBitrate,
		"-ac", "2",
	)

	// HLS output
	args = append(args,
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", e.cfg.HLSSegmentDuration),
		"-hls_list_size", "0",
		"-hls_segment_filename", segmentPattern,
		"-hls_flags", "independent_segments+append_list",
		"-hls_segment_type", "mpegts",
		"-y", playlistPath,
	)

	cmd := exec.CommandContext(ctx, e.ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("HLS generation from source failed: %w", err)
	}

	return nil
}

// parseBitrateToBytes converts a bitrate string (e.g., "5M", "2500k") to bytes per second
func parseBitrateToBytes(bitrate string) int {
	bitrate = strings.TrimSpace(bitrate)
	if len(bitrate) < 2 {
		return 0
	}

	var multiplier float64
	suffix := bitrate[len(bitrate)-1]
	valueStr := bitrate[:len(bitrate)-1]

	switch suffix {
	case 'k', 'K':
		multiplier = 1000
	case 'm', 'M':
		multiplier = 1000000
	case 'g', 'G':
		multiplier = 1000000000
	default:
		multiplier = 1
		valueStr = bitrate
	}

	var value float64
	fmt.Sscanf(valueStr, "%f", &value)
	return int(value * multiplier)
}

// getCodecsString returns the CODECS attribute for HLS/DASH playlists
func getCodecsString() string {
	return "avc1.640028,mp4a.40.2" // H.264 Main Profile Level 4.0, AAC-LC
}

// HLSMasterPlaylistTemplate is a Go template for the master HLS playlist
var HLSMasterPlaylistTemplate = `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-INDEPENDENT-SEGMENTS
{{range .Streams}}#EXT-X-STREAM-INF:BANDWIDTH={{.Bandwidth}},AVERAGE-BANDWIDTH={{.AvgBandwidth}},RESOLUTION={{.Resolution}},CODECS="{{.Codecs}}"
{{.PlaylistPath}}
{{end}}`

// HLSMasterStream represents a stream in the master playlist
type HLSMasterStream struct {
	Bandwidth    int
	AvgBandwidth int
	Resolution   string
	Codecs       string
	PlaylistPath string
}

// GenerateHLSMasterPlaylistFromTemplate generates a master playlist using Go templates
func GenerateHLSMasterPlaylistFromTemplate(outputPath string, streams []HLSMasterStream) error {
	tmpl, err := template.New("master").Parse(HLSMasterPlaylistTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	data := struct {
		Streams []HLSMasterStream
	}{
		Streams: streams,
	}

	return tmpl.Execute(file, data)
}
