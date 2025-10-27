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
		fmt.Printf("🔥 Processing segment: Start=%.3f, End=%.3f, Note=%d\n", seg.Start, seg.End, seg.Note)
		outFile := filepath.Join(outputDir, fmt.Sprintf("%03d.mp4", seg.Note))

		// Path to the pitch-corrected audio file
		audioFile := fmt.Sprintf("audio_files/%03d.wav", seg.Note)

		// Check if the audio file exists
		if _, err := os.Stat(audioFile); os.IsNotExist(err) {
			fmt.Printf("😭 %s", audioFile)

			return nil, fmt.Errorf("audio file not found: %s", audioFile)
		}

		cmd := exec.Command(
			"ffmpeg",
			"-y",
			"-ss", fmt.Sprintf("%.3f", seg.Start),
			"-to", fmt.Sprintf("%.3f", seg.End),
			"-i", videoPath,
			"-i", audioFile,
			"-map", "0:v:0", // Video from first input (original video)
			"-map", "1:a:0", // Audio from second input (pitch-corrected audio)
			"-c:v", "libx264",
			"-c:a", "aac",
			"-shortest", // End when shortest stream ends
			outFile,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		fmt.Printf("Creating video clip %s (%.3f - %.3f) with audio from %s\n",
			outFile, seg.Start, seg.End, audioFile)
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

	cleanUpTempDirs()

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

	// Step 1.5 Prepare the audio pitch-corrected
	filteredSegments := audiopack.FilterAudioSegments(segments, videoPath, 1.3)

	finalSegments := audiopack.PrepareAudio(filteredSegments, audioPath)

	// Step 2: Split video into clips in temp_vids
	tempVidDir := "temp_vids"

	splitVideoSegments(videoPath, finalSegments, tempVidDir)

	if err != nil {
		log.Fatalf("Error splitting video: %v", err)
	}

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

}

func cleanUpTempDirs() {
	fmt.Println("Cleaning up previous run directories...")
	dirsToRemove := []string{
		"./audio_files",
		"./temp_vids",
		"./temp_pitch_corrected_audio",
	}

	for _, dir := range dirsToRemove {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("Warning: Failed to remove %s: %v", dir, err)
		} else {
			fmt.Printf("✓ Removed %s\n", dir)
		}
	}
}
