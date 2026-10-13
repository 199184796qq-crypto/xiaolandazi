package roomaudio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var ErrPCMNormalizationRequired = errors.New("audio source requires pcm normalization")

// leadingSilenceSampleThreshold matches the management-service voice trim: a
// 16-bit sample whose amplitude is within ±64 (~-54 dBFS) counts as silence.
const leadingSilenceSampleThreshold = 64

// LeadingSilenceMS reports the duration in milliseconds of leading
// near-silence in a 24kHz mono 16-bit PCM WAV payload. TTS providers (notably
// CosyVoice clones) prepend a few hundred milliseconds of low-level silence
// before the first phoneme; skipping it keeps captions aligned with audio.
// Non-WAV or mismatched audio returns 0 so the caller falls back to normal
// playback.
func LeadingSilenceMS(data []byte) int {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0
	}
	var (
		channels      uint16
		sampleRate    uint32
		bitsPerSample uint16
	)
	dataStart := -1
	dataSize := 0
	for off := 12; off+8 <= len(data); {
		chunkID := string(data[off : off+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		if chunkSize < 0 || off+8+chunkSize > len(data) {
			return 0
		}
		switch chunkID {
		case "fmt ":
			if chunkSize >= 16 {
				channels = binary.LittleEndian.Uint16(data[off+10 : off+12])
				sampleRate = binary.LittleEndian.Uint32(data[off+12 : off+16])
				bitsPerSample = binary.LittleEndian.Uint16(data[off+22 : off+24])
			}
		case "data":
			dataStart = off + 8
			dataSize = chunkSize
			goto scan
		}
		off += 8 + chunkSize
		if chunkSize%2 == 1 {
			off++
		}
	}
scan:
	if dataStart < 0 || channels != Channels || sampleRate != SampleRate || bitsPerSample != BytesPerSample*8 {
		return 0
	}
	pcm := data[dataStart:]
	if dataSize < len(pcm) {
		pcm = pcm[:dataSize]
	}
	bytesPerSample := int(bitsPerSample / 8)
	totalSamples := len(pcm) / bytesPerSample
	leadingSamples := 0
	for i := 0; i+bytesPerSample <= len(pcm); i += bytesPerSample {
		value := int16(binary.LittleEndian.Uint16(pcm[i : i+2]))
		if value < -leadingSilenceSampleThreshold || value > leadingSilenceSampleThreshold {
			break
		}
		leadingSamples++
	}
	// 整段都是静音（无语音）时不要整体跳过，保持原样播放，返回 0。
	if leadingSamples >= totalSamples {
		return 0
	}
	return leadingSamples * 1000 / int(sampleRate)
}

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
		return fmt.Errorf("%w: audio source is not a RIFF/WAVE file", ErrPCMNormalizationRequired)
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
				return fmt.Errorf("%w: wav must be pcm s16le mono 24khz", ErrPCMNormalizationRequired)
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
		n, err := io.ReadFull(reader, frame)
		if err != nil {
			if emittedFrames > 0 && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
				if n > 0 {
					clear(frame[n:])
					if publishErr := onFrame(append([]byte(nil), frame...), cursorMS); publishErr != nil {
						return publishErr
					}
				}
				// Some generated/TTS WAV files advertise a data chunk that is a few
				// bytes longer than the HTTP body. Emit and silence-pad any physical
				// final sub-frame before treating the boundary as normal completion.
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
	// Do not discard the final sub-frame of a generated WAV. Pad it to a full
	// output frame so the last few milliseconds of speech are always emitted.
	if remaining > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		clear(frame)
		if _, err := io.ReadFull(reader, frame[:remaining]); err != nil {
			if emittedFrames > 0 && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
				return nil
			}
			return err
		}
		if err := onFrame(append([]byte(nil), frame...), cursorMS); err != nil {
			return err
		}
	}
	return nil
}
