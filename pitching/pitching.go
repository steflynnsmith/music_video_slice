package pitching

import (
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// pitchCorrectAudio detects the pitch of the input audio and shifts it to the nearest MIDI note
func PitchCorrectAudio(inputAudio, outputAudio string, targetMIDI float64) error {
	// Step 1: Detect the current pitch using aubiopitch
	detectedPitch, err := detectPitch(inputAudio)
	if err != nil {
		return fmt.Errorf("failed to detect pitch: %w", err)
	}

	if detectedPitch <= 0 {
		return fmt.Errorf("no valid pitch detected")
	}

	fmt.Printf("Detected pitch: %.2f Hz\n", detectedPitch)

	// Step 2: Convert detected frequency to MIDI note number
	detectedMIDI := frequencyToMIDI(detectedPitch)
	fmt.Printf("Detected MIDI note (float): %.2f\n", detectedMIDI)

	// Step 3: Round to nearest MIDI note
	fmt.Printf("Target MIDI note: %.0f\n", targetMIDI)

	// Step 4: Calculate cents difference
	// Cents = 1200 * log2(f2/f1) = 100 * (targetMIDI - detectedMIDI)
	centsShift := 100 * (targetMIDI - detectedMIDI)
	fmt.Printf("Pitch shift needed: %.2f cents\n", centsShift)

	// Step 5: Apply pitch shift using SoX
	if math.Abs(centsShift) < 1 {
		fmt.Println("Pitch is already close to target, no correction needed")
		// Just copy the file
		return exec.Command("cp", inputAudio, outputAudio).Run()
	}

	cmd := exec.Command(
		"sox", inputAudio, outputAudio,
		"pitch", fmt.Sprintf("%.2f", centsShift),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sox pitch shift failed: %w, output: %s", err, string(output))
	}

	fmt.Printf("Successfully pitch corrected by %.2f cents\n", centsShift)
	return nil
}

// detectPitch uses aubiopitch to detect the dominant frequency in the audio file
func detectPitch(audioPath string) (float64, error) {
	cmd := exec.Command("aubiopitch", "-i", audioPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("aubiopitch failed: %w", err)
	}

	// Parse aubiopitch output - format is typically "timestamp frequency"
	lines := strings.Split(string(output), "\n")
	var frequencies []float64

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 2 {
			freq, err := strconv.ParseFloat(fields[1], 64)
			if err == nil && freq > 0 {
				frequencies = append(frequencies, freq)
			}
		}
	}

	if len(frequencies) == 0 {
		return 0, fmt.Errorf("no valid frequencies detected")
	}

	// Return the median frequency to avoid outliers
	return median(frequencies), nil
}

// frequencyToMIDI converts a frequency in Hz to a MIDI note number (float)
// MIDI note = 69 + 12 * log2(f / 440)
func frequencyToMIDI(frequency float64) float64 {
	return 69.0 + 12.0*math.Log2(frequency/440.0)
}

// midiToFrequency converts a MIDI note number to frequency in Hz
// f = 440 * 2^((midi - 69) / 12)
func midiToFrequency(midi float64) float64 {
	return 440.0 * math.Pow(2.0, (midi-69.0)/12.0)
}

// median calculates the median of a slice of float64
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	// Make a copy to avoid modifying the original
	sorted := make([]float64, len(values))
	copy(sorted, values)

	// Simple bubble sort (fine for small arrays)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}
