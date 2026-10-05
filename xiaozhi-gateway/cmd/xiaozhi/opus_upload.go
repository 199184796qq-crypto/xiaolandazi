package main

import (
	"bytes"
	"encoding/binary"
	"errors"
)

const maxVoiceDurationSamples = 8 * 48000

// Opus always uses a 48 kHz Ogg granule clock, even if the input microphone is
// 16 kHz. Derive each packet duration from its TOC, not from a guessed frame rate.
func opusPacketSamples(packet []byte) (int, error) {
	if len(packet) < 2 || len(packet) > 8192 {
		return 0, errors.New("invalid Opus packet size")
	}
	configuration := packet[0] >> 3
	var samples int
	switch {
	case configuration < 12:
		samples = []int{480, 960, 1920, 2880}[configuration%4]
	case configuration < 16:
		samples = []int{480, 960}[configuration%2]
	default:
		samples = []int{120, 240, 480, 960}[configuration%4]
	}
	frames := 1
	switch packet[0] & 3 {
	case 1, 2:
		frames = 2
	case 3:
		frames = int(packet[1] & 63)
		if frames == 0 {
			return 0, errors.New("empty Opus frame count")
		}
	}
	if samples*frames > 5760 {
		return 0, errors.New("Opus packet exceeds 120 ms")
	}
	return samples * frames, nil
}

func oggCRC(data []byte) uint32 {
	var crc uint32
	for _, value := range data {
		crc ^= uint32(value) << 24
		for bit := 0; bit < 8; bit++ {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func appendOggPage(out *bytes.Buffer, serial, sequence uint32, flags byte, granule uint64, packet []byte) {
	segments := len(packet)/255 + 1
	page := make([]byte, 27+segments+len(packet))
	copy(page, "OggS")
	page[5] = flags
	binary.LittleEndian.PutUint64(page[6:14], granule)
	binary.LittleEndian.PutUint32(page[14:18], serial)
	binary.LittleEndian.PutUint32(page[18:22], sequence)
	page[26] = byte(segments)
	for index := 0; index < segments-1; index++ {
		page[27+index] = 255
	}
	page[27+segments-1] = byte(len(packet) % 255)
	copy(page[27+segments:], packet)
	binary.LittleEndian.PutUint32(page[22:26], oggCRC(page))
	out.Write(page)
}

func microphoneOgg(packets [][]byte, sampleRate int) ([]byte, error) {
	if len(packets) == 0 {
		return nil, errors.New("没有收到语音，请靠近设备再说一次")
	}
	if sampleRate != 8000 && sampleRate != 12000 && sampleRate != 16000 && sampleRate != 24000 && sampleRate != 48000 {
		return nil, errors.New("不支持的麦克风采样率")
	}
	var out bytes.Buffer
	head := make([]byte, 19)
	copy(head, "OpusHead")
	head[8], head[9] = 1, 1 // version 1, mono, pre-skip 0, mapping family 0
	binary.LittleEndian.PutUint32(head[12:16], uint32(sampleRate))
	const serial = uint32(0x584C414E)
	appendOggPage(&out, serial, 0, 2, 0, head)
	var tags bytes.Buffer
	tags.WriteString("OpusTags")
	_ = binary.Write(&tags, binary.LittleEndian, uint32(len("xiaolan-device-control")))
	tags.WriteString("xiaolan-device-control")
	_ = binary.Write(&tags, binary.LittleEndian, uint32(0))
	appendOggPage(&out, serial, 1, 0, 0, tags.Bytes())
	var granule uint64
	for index, packet := range packets {
		samples, err := opusPacketSamples(packet)
		if err != nil {
			return nil, err
		}
		granule += uint64(samples)
		if granule > maxVoiceDurationSamples {
			return nil, errors.New("单条语音不能超过8秒")
		}
		flags := byte(0)
		if index == len(packets)-1 {
			flags = 4 // End-of-stream: a complete upload, not a streaming fragment.
		}
		appendOggPage(&out, serial, uint32(index+2), flags, granule, packet)
	}
	return out.Bytes(), nil
}
