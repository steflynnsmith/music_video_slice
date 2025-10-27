package audiopack

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"hello/pitching"
)

type NoteSegment struct {
	Start float64
	End   float64
	Note  int
}

const MinClipDuration = 0.25 // seconds

func RunAubioNotes(audioPath string) ([]string, error) {
	cmd := exec.Command("aubionotes", audioPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var lines []string
	buf := make([]byte, 1024)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			for _, line := range strings.Split(string(buf[:n]), "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					lines = append(lines, line)
				}
			}
		}
		if err != nil {
			break
		}
	}

	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	return lines, nil
}

func ParseAubioOutput(lines []string) []NoteSegment {
	var segments []NoteSegment

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 3 {
			start, err1 := strconv.ParseFloat(fields[1], 64)
			end, err2 := strconv.ParseFloat(fields[2], 64)
			noteFloat, err3 := strconv.ParseFloat(fields[0], 64)
			note := int(noteFloat)

			if err1 == nil && err2 == nil && err3 == nil && (end-start) >= MinClipDuration {
				segments = append(segments, NoteSegment{Start: start, End: end, Note: note})
			}
		}
	}
	fmt.Println("Segments parsed:", len(segments))

	return segments
}

func FilterAudioSegments(segments []NoteSegment, videoPath string, thresholdDB float64) []NoteSegment {
	// Get the mean volume level of the entire audio
	meanVolume, err := getMeanVolume(videoPath)
	if err != nil {
		log.Printf("Warning: Could not get mean volume, skipping filtering: %v", err)
		return segments
	}

	fmt.Printf("Mean volume: %.2f dB\n", meanVolume)

	var filtered []NoteSegment

	// Loop through all segments and filter out any below threshold
	for _, seg := range segments {
		// Get volume for this specific segment
		segVolume, err := getSegmentVolume(videoPath, seg.Start, seg.End)
		if err != nil {
			log.Printf("Warning: Could not get volume for segment at %.2f-%.2f, keeping it: %v", seg.Start, seg.End, err)
			filtered = append(filtered, seg)
			continue
		}

		// Keep segment if its volume is above (mean - threshold)
		if segVolume >= (meanVolume - thresholdDB) {
			filtered = append(filtered, seg)
		} else {
			fmt.Printf("Filtered out segment at %.2f-%.2f (volume: %.2f dB, threshold: %.2f dB)\n",
				seg.Start, seg.End, segVolume, meanVolume-thresholdDB)
		}
	}

	fmt.Printf("Filtered segments: %d -> %d (removed %d)\n", len(segments), len(filtered), len(segments)-len(filtered))
	return filtered
}

// getMeanVolume returns the mean volume in dB for the entire audio
func getMeanVolume(videoPath string) (float64, error) {
	cmd := exec.Command(
		"ffmpeg",
		"-i", videoPath,
		"-af", "volumedetect",
		"-vn",
		"-f", "null",
		"-",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ffmpeg volumedetect failed: %w", err)
	}

	// Parse output for mean_volume
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "mean_volume:") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field == "mean_volume:" && i+1 < len(fields) {
					volumeStr := strings.TrimSpace(fields[i+1])
					volume, err := strconv.ParseFloat(volumeStr, 64)
					if err == nil {
						return volume, nil
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("could not parse mean_volume from ffmpeg output")
}

// getSegmentVolume returns the mean volume in dB for a specific time segment
func getSegmentVolume(videoPath string, start, end float64) (float64, error) {
	duration := end - start
	cmd := exec.Command(
		"ffmpeg",
		"-ss", fmt.Sprintf("%.3f", start),
		"-t", fmt.Sprintf("%.3f", duration),
		"-i", videoPath,
		"-af", "volumedetect",
		"-vn",
		"-f", "null",
		"-",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ffmpeg volumedetect failed: %w", err)
	}

	// Parse output for mean_volume
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "mean_volume:") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field == "mean_volume:" && i+1 < len(fields) {
					volumeStr := strings.TrimSpace(fields[i+1])
					volume, err := strconv.ParseFloat(volumeStr, 64)
					if err == nil {
						return volume, nil
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("could not parse mean_volume from ffmpeg output")
}

func PitchVideoClips(clipPaths []string) {
	// Step 3: Loop through clips, pitch-correct audio, replace audio
	tempPitchDir := "temp_pitch_corrected_vids"
	if err := ensureDir(tempPitchDir); err != nil {
		log.Fatalf("Error creating pitch-corrected dir: %v", err)
	}

	// Make sure the audio_files directory exists
	audioDir := "./audio_files"
	if err := os.MkdirAll(audioDir, 0755); err != nil {
		log.Fatalf("Error creating audio directory: %v", err)
	}

	for _, clip := range clipPaths {
		clipBase := filepath.Base(clip)
		audioTmp := filepath.Join(audioDir, clipBase+".wav")
		audioCorrected := filepath.Join(audioDir, clipBase+".corrected.wav")
		outClip := filepath.Join(tempPitchDir, clipBase)

		// Extract audio
		if err := ExtractAudio(clip, audioTmp); err != nil {
			// Delete the video clip if audio extraction fails
			continue
		}

		// Pitch correct
		if err := pitching.PitchCorrectAudio(audioTmp, audioCorrected); err != nil {
			continue
		}

		// Replace audio in video
		if err := ReplaceAudioInVideo(clip, audioCorrected, outClip); err != nil {
			log.Fatalf("Error replacing audio in clip %s: %v", clip, err)
		}
	}
}

func ExtractAudio(videoPath, audioPath string) error {
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-i", videoPath,
		"-vn",
		"-acodec", "pcm_s16le",
		"-ar", "44100",
		"-ac", "2",
		audioPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ReplaceAudioInVideo(videoPath, audioPath, outputPath string) error {
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-i", videoPath,
		"-i", audioPath,
		"-c:v", "copy",
		"-c:a", "aac",
		"-map", "0:v:0",
		"-map", "1:a:0",
		outputPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ensureDir(dir string) error {
	return os.MkdirAll(dir, os.ModePerm)
}
