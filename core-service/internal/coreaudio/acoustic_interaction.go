package coreaudio

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/roomaudio"
)

// Cuts are derived from the exact normalized PCM, never from text or captions.
// Frame-aligned millisecond cuts match the engine's 20ms output contract.
type acousticTable struct {
	Hash    [32]byte
	Samples int
	Points  []audioout.AcousticPoint
}

const maxAcousticPCMBytes = 32 << 20

func scanAcousticPCM(pcm []byte) *acousticTable {
	t := &acousticTable{Hash: sha256.Sum256(pcm), Samples: len(pcm) / 2}
	n := len(pcm) / roomaudio.PCMBytesPerFrame
	if n < 5 {
		return t
	}
	env := make([]float64, n)
	var total float64
	for i := range env {
		var sum float64
		for j := i * roomaudio.PCMBytesPerFrame; j < (i+1)*roomaudio.PCMBytesPerFrame; j += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(pcm[j:])))
			sum += v * v
		}
		env[i] = math.Sqrt(sum / float64(roomaudio.PCMBytesPerFrame/2))
		total += sum
	}
	mean := math.Sqrt(total / float64(n*roomaudio.PCMBytesPerFrame/2))
	// Three consecutive low-energy frames (60ms), with local and absolute
	// ceilings, reject isolated zero crossings and low-volume voiced samples.
	grades := make([]string, n)
	for i, e := range env {
		var local float64
		lo, hi := max(0, i-25), min(n, i+26)
		for _, v := range env[lo:hi] {
			local += v * v
		}
		local = math.Sqrt(local / float64(hi-lo))
		a := min(100.0, max(1.0, min(mean*.04, local*.10)))
		b := min(300.0, max(1.0, min(mean*.15, local*.25)))
		if e <= a {
			grades[i] = "A"
		} else if e <= b {
			grades[i] = "B"
		}
	}
	for start := 0; start < n; {
		if grades[start] == "" {
			start++
			continue
		}
		end := start + 1
		for end < n && grades[end] != "" {
			end++
		}
		if end-start >= 3 {
			// Use the middle of a real low-energy neighborhood, leaving a full
			// frame on each side. Long silence offers more than one opportunity.
			for lo := start; lo+2 < end; {
				hi := min(end, lo+50)
				mid := (lo + hi) / 2
				if mid >= 1 && mid+1 < n && grades[mid-1] != "" && grades[mid] != "" {
					grade := "B"
					if grades[mid-1] == "A" && grades[mid] == "A" && grades[min(mid+1, end-1)] == "A" {
						grade = "A"
					}
					ms := mid * roomaudio.FrameDurationMS
					if ms >= 100 && ms <= (n-1)*roomaudio.FrameDurationMS {
						t.Points = append(t.Points, audioout.AcousticPoint{CutMS: ms, Grade: grade})
					}
				}
				lo = hi
			}
		}
		start = end
	}
	return t
}

// Analyze the same retained download that will be played, avoiding a second
// network request or a mismatched asset. Temporary files keep per-room memory
// bounded; only four PCM scans run at once and each is capped at 32 MiB.
type acousticSource struct{ *os.File }

func (s *acousticSource) Close() error {
	name := s.Name()
	err := s.File.Close()
	_ = os.Remove(name)
	return err
}

func (c *Client) acousticPlaybackSource(ctx context.Context, program *programState, taskID string, position programTrackPosition, track programTrack, source io.ReadCloser) (io.ReadCloser, error) {
	select {
	case c.scanSlots <- struct{}{}:
		defer func() { <-c.scanSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	file, err := os.CreateTemp("", "core-acoustic-*.wav")
	if err != nil {
		return nil, err
	}
	owned := &acousticSource{file}
	success := false
	defer func() {
		source.Close()
		if !success {
			owned.Close()
		}
	}()
	n, err := io.Copy(file, io.LimitReader(source, maxAcousticPCMBytes+1))
	if err != nil {
		return nil, err
	}
	if n > maxAcousticPCMBytes {
		return nil, errors.New("acoustic source exceeds bound")
	}
	if _, err = file.Seek(0, 0); err != nil {
		return nil, err
	}
	pcm := make([]byte, 0, min(track.DurationMS*48, maxAcousticPCMBytes))
	collect := func(frame []byte, _ int) error {
		if len(pcm)+len(frame) > maxAcousticPCMBytes {
			return errors.New("acoustic PCM exceeds bound")
		}
		pcm = append(pcm, frame...)
		return nil
	}
	err = roomaudio.StreamWAVPCM(ctx, file, 0, collect)
	if errors.Is(err, roomaudio.ErrPCMNormalizationRequired) {
		pcm = pcm[:0]
		err = CollectNormalizedPCM(ctx, file.Name(), collect)
		if err == nil {
			// Playback must consume exactly the normalized samples that were scanned.
			if err = file.Truncate(0); err == nil {
				_, err = file.Seek(0, 0)
			}
			if err == nil {
				_, err = file.Write(acousticPCMAsWAV(pcm))
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if len(pcm)/48 < track.DurationMS {
		return nil, io.ErrUnexpectedEOF
	}
	table := scanAcousticPCM(pcm)
	c.mu.Lock()
	index, e := position.index(program)
	valid := e == nil && ctx.Err() == nil && c.programs[program.RoomID] == program && program.Running && !program.Suspended && !program.ManualPaused && program.CurrentTaskID == taskID
	if valid {
		prior := program.Tracks[index].Acoustic
		if prior != nil && prior.Hash != table.Hash {
			c.mu.Unlock()
			return nil, errors.New("acoustic source identity changed")
		}
		program.Tracks[index].Acoustic = table
	}
	c.mu.Unlock()
	if !valid {
		return nil, context.Canceled
	}
	if _, err = file.Seek(0, 0); err != nil {
		return nil, err
	}
	log.Printf("core acoustic cuts ready room=%d program=%s track=%s algorithm=pcm-neighborhood-v1 samples=%d points=%d hash=%x", program.RoomID, program.ID, track.ID, table.Samples, len(table.Points), table.Hash)
	success = true
	return owned, nil
}

func acousticPCMAsWAV(pcm []byte) []byte {
	out := make([]byte, 44+len(pcm))
	copy(out, "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	copy(out[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], 1)
	binary.LittleEndian.PutUint32(out[24:], 24000)
	binary.LittleEndian.PutUint32(out[28:], 48000)
	binary.LittleEndian.PutUint16(out[32:], 2)
	binary.LittleEndian.PutUint16(out[34:], 16)
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(len(pcm)))
	copy(out[44:], pcm)
	return out
}

func verifiedAcousticCut(track programTrack, target int) bool {
	if track.Acoustic == nil || target <= 0 || target >= track.DurationMS || target%roomaudio.FrameDurationMS != 0 {
		return false
	}
	for _, p := range track.Acoustic.Points {
		if p.CutMS == target {
			return true
		}
	}
	return false
}

// Equal-length ramps operate on copies made for output; paid assets and the
// canceled reservation's unplayed samples remain immutable.
func acousticRamp(pcm []byte, fadeIn bool) {
	n := min(48, len(pcm)/2) // 2ms at 24kHz
	for i := 0; i < n; i++ {
		j := i
		if !fadeIn {
			j = len(pcm)/2 - n + i
		}
		v := int16(binary.LittleEndian.Uint16(pcm[j*2:]))
		gain := i
		if !fadeIn {
			gain = n - 1 - i
		}
		binary.LittleEndian.PutUint16(pcm[j*2:], uint16(int16(int(v)*gain/max(1, n-1))))
	}
}

func acousticSwitchError() error {
	return fmt.Errorf("%w: acoustic cut is not bound to current PCM", audioout.ErrInteractionSwitchStale)
}
