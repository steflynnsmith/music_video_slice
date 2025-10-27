package midiparse

import (
	"fmt"
	"os"

	"gitlab.com/gomidi/midi/v2/smf"

	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

type NoteEvent struct {
	Note     int
	Start    float64 // in seconds
	Duration float64 // in seconds
}

func ParseMIDI(filename string) ([]NoteEvent, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	fmt.Printf("Opened file: %s\n", filename)

	var events []NoteEvent
	noteStart := map[int]float64{}

	reader := smf.ReadTracksFrom(f)
	fmt.Printf("Created reader: %+v\n", reader)

	callbackCount := 0
	reader.Do(func(ev smf.TrackEvent) {
		callbackCount++
		fmt.Printf("Event: %v\n", ev.Message)

		var ch, key, vel uint8
		gotNoteStart := ev.Message.GetNoteStart(&ch, &key, &vel)
		fmt.Printf("GetNoteStart returned: %v\n", gotNoteStart)

		if gotNoteStart {
			fmt.Printf("Note Start - Channel: %d, Key: %d, Velocity: %d, Time: %f\n",
				ch, key, vel, float64(ev.AbsMicroSeconds)/1_000_000)
			noteStart[int(key)] = float64(ev.AbsMicroSeconds) / 1_000_000
		}

		var ch2, key2 uint8
		gotNoteEnd := ev.Message.GetNoteEnd(&ch2, &key2)
		fmt.Printf("GetNoteEnd returned: %v\n", gotNoteEnd)

		if gotNoteEnd {
			fmt.Printf("Note End - Channel: %d, Key: %d, Time: %f\n",
				ch2, key2, float64(ev.AbsMicroSeconds)/1_000_000)
			if start, ok := noteStart[int(key2)]; ok {
				end := float64(ev.AbsMicroSeconds) / 1_000_000
				events = append(events, NoteEvent{
					Note:     int(key2),
					Start:    start,
					Duration: end - start,
				})
				delete(noteStart, int(key2))
			}
		}
	})

	fmt.Printf("Callback executed %d times\n", callbackCount)
	fmt.Printf("Total events collected: %d\n", len(events))

	return events, nil
}
