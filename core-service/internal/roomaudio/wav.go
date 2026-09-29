package roomaudio

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
)

func StreamWAVPCM(ctx context.Context, reader io.Reader, startMS int, onFrame func([]byte, int) error) error {
	if reader == nil {
		return errors.New("wav reader is required")
	}
	if onFrame == nil {
		return errors.New("frame callback is required")
	}
	if startMS < 0 {
		startMS = 0
	}

	header := make([]byte, 12)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	if string(header[:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return errors.New("audio source is not a RIFF/WAVE file")
	}

	var (
		formatSeen bool
		dataSize   int64
	)
	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(reader, chunkHeader); err != nil {
			return err
		}
		chunkID := string(chunkHeader[:4])
		chunkSize := int64(binary.LittleEndian.Uint32(chunkHeader[4:8]))
		if chunkSize < 0 {
			return errors.New("invalid wav chunk size")
		}

		switch chunkID {
		case "fmt ":
			if chunkSize < 16 || chunkSize > 1<<20 {
				return errors.New("invalid wav fmt chunk")
			}
			payload := make([]byte, chunkSize)
			if _, err := io.ReadFull(reader, payload); err != nil {
				return err
			}
			audioFormat := binary.LittleEndian.Uint16(payload[0:2])
			channels := binary.LittleEndian.Uint16(payload[2:4])
			sampleRate := binary.LittleEndian.Uint32(payload[4:8])
			bitsPerSample := binary.LittleEndian.Uint16(payload[14:16])
			if audioFormat != 1 || channels != Channels || sampleRate != SampleRate || bitsPerSample != BytesPerSample*8 {
				return errors.New("wav must be pcm s16le mono 24khz")
			}
			formatSeen = true
		case "data":
			if !formatSeen {
				return errors.New("wav data chunk appeared before fmt")
			}
			dataSize = chunkSize
			goto streamData
		default:
			if _, err := io.CopyN(io.Discard, reader, chunkSize); err != nil {
				return err
			}
		}

		if chunkSize%2 == 1 {
			if _, err := io.CopyN(io.Discard, reader, 1); err != nil {
				return err
			}
		}
	}

streamData:
	bytesPerSecond := int64(SampleRate * Channels * BytesPerSample)
	startBytes := int64(startMS) * bytesPerSecond / 1000
	startBytes -= startBytes % int64(Channels*BytesPerSample)
	if startBytes > dataSize {
		startBytes = dataSize
	}
	if startBytes > 0 {
		if _, err := io.CopyN(io.Discard, reader, startBytes); err != nil {
			return err
		}
	}

	remaining := dataSize - startBytes
	frame := make([]byte, PCMBytesPerFrame)
	cursorMS := startMS
	emittedFrames := 0
	for remaining >= int64(len(frame)) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if _, err := io.ReadFull(reader, frame); err != nil {
			if emittedFrames > 0 && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
				// Some generated/TTS WAV files advertise a data chunk that is a few
				// bytes longer than the HTTP body. Once real PCM frames were already
				// emitted, reaching the physical end is the audible end of the clip,
				// not a playback failure. Treat it as normal completion so the room
				// engine can resume the mainline immediately instead of waiting for
				// the interaction fallback timer.
				return nil
			}
			return err
		}
		copyFrame := append([]byte(nil), frame...)
		if err := onFrame(copyFrame, cursorMS); err != nil {
			return err
		}
		cursorMS += FrameDurationMS
		remaining -= int64(len(frame))
		emittedFrames++
	}
	return nil
}
