package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	// "hello/buildoutput"
	"hello/audiopack"
	"hello/buildoutput"
	"hello/midiparse"
)

func ensureDir(dir string) error {
	return os.MkdirAll(dir, os.ModePerm)
}

func splitVideoSegments(videoPath string, segments []audiopack.NoteSegment, outputDir string) ([]string, error) {
	if err := ensureDir(outputDir); err != nil {
		return nil, err
	}

	var clipPaths []string
	for _, seg := range segments {
		outFile := filepath.Join(outputDir, fmt.Sprintf("%03d.mp4", seg.Note))
		cmd := exec.Command(
			"ffmpeg",
			"-y",
			"-i", videoPath,
			"-ss", fmt.Sprintf("%.3f", seg.Start),
			"-to", fmt.Sprintf("%.3f", seg.End),
			"-c:v", "libx264",
			"-c:a", "aac",
			outFile,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		fmt.Printf("Creating video clip %s (%.3f - %.3f)\n", outFile, seg.Start, seg.End)
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("ffmpeg split failed: %w", err)
		}
		clipPaths = append(clipPaths, outFile)
	}
	return clipPaths, nil
}

func main() {
	if len(os.Args) < 3 {
		log.Fatalf("Usage: go run main.go <video-file> <midi-file>")
	}
	videoPath := os.Args[1]

	// Step 1: Extract audio and run aubionotes
	audioPath := "audio.wav"
	if err := audiopack.ExtractAudio(videoPath, audioPath); err != nil {
		log.Fatalf("Error extracting audio: %v", err)
	}

	lines, err := audiopack.RunAubioNotes(audioPath)
	if err != nil {
		log.Fatalf("Error running aubionotes: %v", err)
	}

	segments := audiopack.ParseAubioOutput(lines)

	if len(segments) == 0 {
		log.Printf("No segments >= %.3f detected.", audiopack.MinClipDuration)
		return
	}

	// Step 2: Split video into clips in temp_vids
	tempVidDir := "temp_vids"

	clips, err := splitVideoSegments(videoPath, segments, tempVidDir)

	if err != nil {
		log.Fatalf("Error splitting video: %v", err)
	}

	audiopack.PitchVideoClips(clips)

	midiFilePath := os.Args[2]

	outputFile := "final_output_with_audio.mp4"

	events, err := midiparse.ParseMIDI(midiFilePath)
	if err != nil {
		panic(err)
	}

	fmt.Println("Parsed MIDI events:", len(events))

	err = buildoutput.BuildFFmpegCommandWithAudio(events, outputFile)
	if err != nil {
		panic(err)
	}

	fmt.Println("Video with audio generated:", outputFile)

	// fmt.Println("All pitch-corrected video clips created in", tempPitchDir)
}
