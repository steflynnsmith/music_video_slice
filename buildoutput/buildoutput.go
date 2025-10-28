package buildoutput

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"hello/midiparse"
)

// BuildFFmpegCommandWithAudio coordinates the build process, either in a single pass or in batches.
func BuildFFmpegCommandWithAudio(events []midiparse.NoteEvent, outputFile string) error {
	fmt.Println("\n\n\n\nBuilding FFmpeg command...\n\n\n\n\n", events)
	if len(events) == 0 {
		return fmt.Errorf("no events to process")
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Start < events[j].Start
	})

	// Calculate total duration (absolute end time of the last event)
	maxEnd := 0.0
	for _, e := range events {
		end := e.Start + e.Duration
		if end > maxEnd {
			maxEnd = end
		}
	}

	// If we have too many events, process in segments
	const maxEventsPerBatch = 50
	if len(events) > maxEventsPerBatch {
		return buildFFmpegInBatches(events, outputFile, maxEnd, maxEventsPerBatch)
	}

	// Original implementation for smaller sets
	return buildFFmpegSinglePass(events, outputFile, maxEnd)
}

// buildFFmpegInBatches processes events in smaller groups to create temporary segment files.
func buildFFmpegInBatches(events []midiparse.NoteEvent, outputFile string, maxEnd float64, batchSize int) error {
	tempSegments := []string{}

	// Process in batches
	for i := 0; i < len(events); i += batchSize {
		end := i + batchSize
		if end > len(events) {
			end = len(events)
		}

		batchEvents := events[i:end]

		// 1. Determine the absolute start time of the segment
		batchStart := batchEvents[0].Start

		// 2. Calculate the segment's full duration
		batchMaxEnd := 0.0
		for _, e := range batchEvents {
			eventEnd := e.Start + e.Duration
			if eventEnd > batchMaxEnd {
				batchMaxEnd = eventEnd
			}
		}
		// The duration of the segment is the time from the first event's start to the last event's end.
		segmentDuration := batchMaxEnd - batchStart

		// 3. Create a time-shifted slice of events (Fix for black screen issue)
		shiftedEvents := make([]midiparse.NoteEvent, len(batchEvents))
		for j, e := range batchEvents {
			// Shift the start time to be relative to the segment's start (0)
			shiftedEvents[j] = midiparse.NoteEvent{
				Note:     e.Note,
				Start:    e.Start - batchStart, // Corrected time-shift
				Duration: e.Duration,
				// Removed non-existent fields: Velocity and Channel
			}
		}

		segmentFile := fmt.Sprintf("temp_segment_%d.mp4", i/batchSize)
		tempSegments = append(tempSegments, segmentFile)

		fmt.Printf("Processing batch %d/%d (start: %.3f, duration: %.3f)...\n",
			i/batchSize+1, (len(events)+batchSize-1)/batchSize, batchStart, segmentDuration)

		// 4. Pass the time-shifted events and the relative segment duration
		err := buildFFmpegSinglePass(shiftedEvents, segmentFile, segmentDuration)
		if err != nil {
			// Clean up temp files
			// for _, seg := range tempSegments {
			// 	os.Remove(seg)
			// }
			return fmt.Errorf("failed to build segment %d: %w", i/batchSize, err)
		}
	}

	// Combine all segments sequentially.
	return combineSegments(tempSegments, outputFile, maxEnd)
}

// buildFFmpegSinglePass creates a video/audio file for a set of events.
func buildFFmpegSinglePass(events []midiparse.NoteEvent, outputFile string, maxEnd float64) error {
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
		file := fmt.Sprintf("temp_vids/%s.mp4", noteFile)

		// Check if file exists, if not try same note in other octaves
		if _, err := os.Stat(file); os.IsNotExist(err) {
			file = findNoteInOtherOctave(e.Note)
			if file == "" {
				return fmt.Errorf("could not find video for note %s in any octave", noteFile)
			}
			fmt.Printf("Note %s not found, using %s instead\n", noteFile, file)
		}

		inputs = append(inputs, "-i", file)

		// Video: trim to duration, reset timestamps to start at 0, scale, setpts to delay
		filterComplex += fmt.Sprintf("[%d:v]trim=duration=%.3f,setpts=PTS-STARTPTS,scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2,setpts=PTS+%.3f/TB,format=yuv420p[v%d];",
			i, e.Duration, e.Start, i)

		// Audio: trim, delay, volume enable. Times (e.Start) are relative to the segment start.
		delayMS := int(e.Start * 1000)
		filterComplex += fmt.Sprintf("[%d:a]atrim=duration=%.3f,asetpts=PTS-STARTPTS,adelay=%d|%d,volume=enable='between(t,%.3f,%.3f)':volume=1[a%d];",
			i, e.Duration, delayMS, delayMS, e.Start, e.Start+e.Duration, i)
		audioLabels = append(audioLabels, fmt.Sprintf("[a%d]", i))
	}

	// Create a black background video for the segment duration
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

		// Use shortest=0 to ensure the black background stream dictates the length
		filterComplex += fmt.Sprintf("%s[v%d]overlay=shortest=0:eof_action=pass%s;",
			currentLabel, i, outputLabel)
		currentLabel = outputLabel
	}

	// --- CRITICAL AUDIO FIX: Ensure an audio stream is always created ---
	// Add a silent audio source with the same duration as the segment (maxEnd)
	filterComplex += fmt.Sprintf("anullsrc=channel_layout=stereo:sample_rate=44100:d=%.3f[silence];", maxEnd)

	// Start the mixer inputs with the silence stream
	mixerInputs := []string{"[silence]"}

	// Add all generated note audio streams
	mixerInputs = append(mixerInputs, audioLabels...)

	// Join the labels for the amix filter
	allAudioInputs := strings.Join(mixerInputs, "")

	// Mix all audio sources (note audio + silence)
	audioFilter := fmt.Sprintf("%samix=inputs=%d:duration=longest[aout]",
		allAudioInputs, len(mixerInputs))
	filterComplex += audioFilter
	// --- END CRITICAL AUDIO FIX ---

	cmdArgs := append(inputs,
		"-filter_complex", filterComplex,
		"-map", "[vout]", "-map", "[aout]",
		"-c:v", "libx264",
		"-c:a", "aac",
		"-pix_fmt", "yuv420p",
		"-t", fmt.Sprintf("%.3f", maxEnd), // Limit output duration to segment length
		"-y", // Overwrite output file
		outputFile,
	)

	fmt.Printf("Running FFmpeg command with %d inputs\n", len(events))
	fmt.Printf("Total duration: %.3f seconds\n", maxEnd)

	cmd := exec.Command("ffmpeg", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// combineSegments is a function that joins video segments using the ffmpeg 'concat' demuxer.
// This method is generally more robust than the 'concat' filter for segment joining.
func combineSegments(segments []string, outputFile string, duration float64) error {
	if len(segments) == 0 {
		return fmt.Errorf("no segments provided for concatenation")
	}

	fmt.Printf("Combining %d segments into final output using concat demuxer...\n", len(segments))

	// 1. Create a temporary file list for the ffmpeg demuxer
	listContent := ""
	for _, seg := range segments {
		// The demuxer requires paths relative to the ffmpeg execution or absolute paths.
		// Use file paths that ffmpeg can easily read.
		listContent += fmt.Sprintf("file '%s'\n", seg)
	}

	tempListFile := "segment_list_temp.txt"
	if err := os.WriteFile(tempListFile, []byte(listContent), 0644); err != nil {
		return fmt.Errorf("failed to create temporary list file: %w", err)
	}

	// Ensure the list file is cleaned up after the function finishes
	defer os.Remove(tempListFile)

	// 2. Build the ffmpeg command using the concat demuxer
	// -f concat: Specifies the concat demuxer
	// -i: Uses the temporary list file as input
	// -c copy: Instructs ffmpeg to simply copy the streams without re-encoding, which is fast and lossless.
	cmdArgs := []string{
		"-f", "concat",
		"-safe", "0", // Required for external files/absolute paths
		"-i", tempListFile,
		"-c", "copy",
		"-t", fmt.Sprintf("%.3f", duration), // Use the final maxEnd duration
		"-y",
		outputFile,
	}

	cmd := exec.Command("ffmpeg", cmdArgs...)

	// Print the command for debugging purposes (helpful to see what ffmpeg executes)
	fmt.Printf("Executing command: ffmpeg %s\n", strings.Join(cmdArgs, " "))

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()

	if err != nil {
		return fmt.Errorf("ffmpeg concatenation failed: %w", err)
	}

	// 3. Clean up the temp segment files (optional, but good practice)
	// for _, seg := range segments {
	// Uncomment this if you want to remove the segments automatically:
	// os.Remove(seg)
	// }

	fmt.Printf("Successfully created output file: %s\n", outputFile)

	return nil
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
			file := fmt.Sprintf("temp_vids/%s.mp4", noteFile)
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
