package buildoutput

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"

	"hello/midiparse"
)

func BuildFFmpegCommandWithAudio(events []midiparse.NoteEvent, outputFile string) error {
	fmt.Println("\n\n\n\nBuilding FFmpeg command...\n\n\n\n\n", events)
	if len(events) == 0 {
		return fmt.Errorf("no events to process")
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Start < events[j].Start
	})

	inputs := []string{}
	filterComplex := ""
	audioLabels := []string{}

	// Add inputs and build filter chains
	for i, e := range events {
		noteFile := strconv.Itoa(e.Note)
		if e.Note < 100 {
			noteFile = "0" + noteFile
		}
		file := fmt.Sprintf("temp_pitch_corrected_vids/%s.mp4", noteFile)

		// Check if file exists, if not try same note in other octaves
		if _, err := os.Stat(file); os.IsNotExist(err) {
			file = findNoteInOtherOctave(e.Note)
			if file == "" {
				return fmt.Errorf("could not find video for note %s in any octave", noteFile)
			}
			fmt.Printf("Note %s not found, using %s instead\n", noteFile, file)
		}

		inputs = append(inputs, "-i", file)

		// Video: trim to duration, then reset timestamps
		filterComplex += fmt.Sprintf("[%d:v]trim=duration=%.3f,setpts=PTS-STARTPTS[v%d];",
			i, e.Duration, i)

		// Audio: trim to duration, delay to match note start, enable only during note duration
		delayMS := int(e.Start * 1000)
		filterComplex += fmt.Sprintf("[%d:a]atrim=duration=%.3f,asetpts=PTS-STARTPTS,adelay=%d|%d,volume=enable='between(t,%.3f,%.3f)':volume=1[a%d];",
			i, e.Duration, delayMS, delayMS, e.Start, e.Start+e.Duration, i)
		audioLabels = append(audioLabels, fmt.Sprintf("[a%d]", i))
	}

	// Calculate total duration
	maxEnd := 0.0
	for _, e := range events {
		end := e.Start + e.Duration
		if end > maxEnd {
			maxEnd = end
		}
	}

	// Create a black background video for the full duration
	filterComplex += fmt.Sprintf("color=black:s=1920x1080:d=%.3f,format=yuv420p[bg];", maxEnd)

	// Build overlay chain starting from black background
	currentLabel := "[bg]"
	for i := 0; i < len(events); i++ {
		outputLabel := ""
		if i == len(events)-1 {
			outputLabel = "[vout]"
		} else {
			outputLabel = fmt.Sprintf("[tmp%d]", i)
		}

		filterComplex += fmt.Sprintf("%s[v%d]overlay=enable='between(t,%.3f,%.3f)'%s;",
			currentLabel, i, events[i].Start, events[i].Start+events[i].Duration, outputLabel)
		currentLabel = outputLabel
	}

	// Mix audio with silence padding
	audioFilter := fmt.Sprintf("%samix=inputs=%d:duration=longest[aout]",
		concatLabels(audioLabels), len(audioLabels))
	filterComplex += audioFilter

	cmdArgs := append(inputs,
		"-filter_complex", filterComplex,
		"-map", "[vout]", "-map", "[aout]",
		"-c:v", "libx264",
		"-c:a", "aac",
		"-pix_fmt", "yuv420p",
		"-t", fmt.Sprintf("%.3f", maxEnd), // Limit output duration
		"-y", // Overwrite output file
		outputFile,
	)

	fmt.Printf("Running FFmpeg command with %d inputs\n", len(events))
	fmt.Printf("Total duration: %.3f seconds\n", maxEnd)
	fmt.Printf("Filter: %s\n", filterComplex)

	cmd := exec.Command("ffmpeg", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func concatLabels(labels []string) string {
	res := ""
	for _, l := range labels {
		res += l
	}
	return res
}

func findNoteInOtherOctave(note int) string {
	// MIDI notes are 0-127, with 12 notes per octave
	// Note % 12 gives us the note within the octave (C, C#, D, etc.)
	noteInOctave := note % 12

	fmt.Printf("Looking for note %d (note in octave: %d)\n", note, noteInOctave)

	// Try all octaves (0-10 covers MIDI range 0-127)
	for octave := 0; octave <= 10; octave++ {
		candidateNote := octave*12 + noteInOctave
		noteFile := strconv.Itoa(candidateNote)
		if candidateNote < 100 {
			noteFile = "0" + noteFile
		}
		if candidateNote >= 0 && candidateNote <= 127 {
			file := fmt.Sprintf("temp_pitch_corrected_vids/%s.mp4", noteFile)
			fmt.Printf("  Checking: %s\n", file)
			if _, err := os.Stat(file); err == nil {
				fmt.Printf("  Found: %s\n", file)
				return file
			}
		}
	}

	fmt.Printf("  No match found in any octave\n")
	return ""
}
